package lyrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchLyrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/synced":
			if r.Method != http.MethodGet || r.Header.Get("User-Agent") == "" {
				t.Error("expected an identified GET request")
			}
			w.Write([]byte(`{"syncedLyrics":"[00:01.00]A timed line"}`))
		case "/instrumental":
			w.Write([]byte(`{"instrumental":true,"syncedLyrics":null}`))
		case "/plain":
			w.Write([]byte(`{"plainLyrics":"A plain line","syncedLyrics":null}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	for _, tt := range []struct{ path, raw, status string }{
		{"/synced", "[00:01.00]A timed line", ""},
		{"/instrumental", "", "Instrumental track"},
		{"/plain", "", "No synced lyrics found"},
		{"/missing", "", "No synced lyrics found"},
	} {
		raw, status := fetch(context.Background(), server.Client(), server.URL+tt.path)
		if raw != tt.raw || status != tt.status {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", tt.path, raw, status, tt.raw, tt.status)
		}
	}
}
