package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"elsewhere/internal/friendshare"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:8787", "HTTP address (use a TLS reverse proxy for remote access)")
	file := flag.String("data", "friends.json", "private friends database path")
	flag.Parse()
	handler, err := friendshare.Open(*file, os.Getenv("ELSEWHERE_FRIENDS_SETUP_KEY"))
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
