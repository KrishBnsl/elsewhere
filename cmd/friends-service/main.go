package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"elsewhere/internal/friendshare"
)

func main() {
	address := "127.0.0.1:8787"
	if port := os.Getenv("PORT"); port != "" {
		address = "0.0.0.0:" + port
	}
	addr := flag.String("listen", address, "HTTP address (use a TLS reverse proxy for remote access)")
	file := flag.String("data", "friends.json", "private friends database path")
	flag.Parse()
	setup := os.Getenv("ELSEWHERE_FRIENDS_SETUP_KEY")
	var handler http.Handler
	var err error
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		var closeDB func()
		handler, closeDB, err = friendshare.OpenPostgres(ctx, dsn, setup)
		cancel()
		if closeDB != nil {
			defer closeDB()
		}
	} else {
		handler, err = friendshare.Open(*file, setup)
	}
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr: *addr, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second,
	}
	log.Printf("Friends service listening on %s", *addr)
	log.Fatal(server.ListenAndServe())
}
