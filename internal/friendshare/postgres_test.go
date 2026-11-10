package friendshare

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestPostgresSharesAndPersistsState(t *testing.T) {
	dsn := os.Getenv("ELSEWHERE_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("set ELSEWHERE_TEST_POSTGRES_URL to test with PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	schema := "friends_test_" + strings.ToLower(rand.Text())
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE")
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	setup := rand.Text()
	open := func() (http.Handler, func()) {
		t.Helper()
		handler, closeDB, err := OpenPostgres(ctx, u.String(), setup)
		if err != nil {
			t.Fatal(err)
		}
		return handler, closeDB
	}
	first, closeFirst := open()
	defer closeFirst()
	second, closeSecond := open()
	defer closeSecond()

	call := func(handler http.Handler, token, op string, req Request) (Reply, error) {
		data, err := json.Marshal(req)
		if err != nil {
			return Reply{}, err
		}
		r := httptest.NewRequest(http.MethodPost, "/v1/"+op, bytes.NewReader(data))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		var reply Reply
		if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
			return Reply{}, err
		}
		if w.Code != http.StatusOK {
			return reply, fmt.Errorf("%s returned %d: %s", op, w.Code, reply.Error)
		}
		return reply, nil
	}
	must := func(handler http.Handler, token, op string, req Request) Reply {
		t.Helper()
		reply, err := call(handler, token, op, req)
		if err != nil {
			t.Fatal(err)
		}
		return reply
	}
	alice, bob := rand.Text(), rand.Text()
	must(first, alice, "connect", Request{Name: "Alice", Code: setup})
	code := must(first, alice, "invite", Request{}).Code
	peer := must(second, bob, "accept", Request{Name: "Bob", Code: code}).Me

	var wg sync.WaitGroup
	for i, handler := range []http.Handler{first, second} {
		wg.Go(func() {
			if _, err := call(handler, alice, "send", Request{
				Friend: peer.ID, Song: Song{Hash: "0123456789abcdef", Title: fmt.Sprintf("Track %d", i)},
			}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()

	afterRestart, closeRestart := open()
	defer closeRestart()
	if cards := must(afterRestart, bob, "home", Request{}).Cards; len(cards) != 2 {
		t.Fatalf("inbox after reopening the service: %+v", cards)
	}
}
