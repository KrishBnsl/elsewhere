package main

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
