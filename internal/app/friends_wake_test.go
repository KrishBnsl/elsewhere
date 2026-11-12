package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"elsewhere/internal/friendshare"
)

func TestFriendsWaitsForColdServiceBeforeSending(t *testing.T) {
	var probes, sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" {
				t.Error("readiness probe sent a device credential or used the wrong method")
			}
			switch probes.Add(1) {
			case 1:
				w.WriteHeader(http.StatusServiceUnavailable)
			case 2:
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, "<html>Waking up</html>")
			default:
				w.WriteHeader(http.StatusNoContent)
			}
		case "/v1/invite":
			sends.Add(1)
			if probes.Load() < 3 {
				t.Error("invite sent before the service was ready")
			}
			fmt.Fprint(w, `{"code":"invite"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := friendsClient{base: server.URL, token: "example-device-token"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.waitReady(ctx, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	reply, err := client.call("invite", friendshare.Request{})
	if err != nil || reply.Code != "invite" || sends.Load() != 1 {
		t.Fatalf("invite = %+v, %v, calls = %d", reply, err, sends.Load())
	}
}

func TestFriendsReadinessWorksWithOlderSelfHostedService(t *testing.T) {
	handler, err := friendshare.Open(t.TempDir()+"/friends.json", "local-test-setup-key-123")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Older builds sent every GET through their JSON method guard.
		r.URL.Path = "/v1/home"
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := (friendsClient{base: server.URL}).waitReady(ctx, time.Millisecond); err != nil {
		t.Fatal(err)
	}
}

func TestFriendsDefaultUsesExistingHostedCredential(t *testing.T) {
	config := t.TempDir()
	t.Setenv("ELSEWHERE_FRIENDS_URL", defaultFriendsURL)
	existing, err := newFriendsClient(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := existing.prepare(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ELSEWHERE_FRIENDS_URL", "")
	hosted, err := newFriendsClient(config)
	if err != nil || hosted == nil || hosted.base != defaultFriendsURL || hosted.token != existing.token {
		t.Fatal("the default URL did not reuse the hosted-service credential")
	}
	t.Setenv("ELSEWHERE_FRIENDS_URL", "http://127.0.0.1:8787")
	local, err := newFriendsClient(config)
	if err != nil || local == nil || local.base != "http://127.0.0.1:8787" || local.token != "" {
		t.Fatal("a self-hosted override should use its own credential")
	}
	t.Setenv("ELSEWHERE_FRIENDS_URL", "off")
	disabled, err := newFriendsClient(config)
	if err != nil || disabled != nil {
		t.Fatal("off should disable the friends client")
	}
}
