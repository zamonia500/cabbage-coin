package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func main() {
	dataDir := flag.String("data", ".", "directory holding cabbage.db")
	once := flag.Bool("once", false, "run every collector once, then exit")
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	noCollect := flag.Bool("no-collect", false, "serve only; do not run collectors")
	lang := flag.String("lang", "ko", "default language for / (ko or ja)")
	flag.Parse()

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	db, err := openDB(filepath.Join(*dataDir, "cabbage.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := seedAreas(db); err != nil {
		log.Fatal(err)
	}

	if *once {
		collectAll(db)
		return
	}
	if !*noCollect {
		go func() {
			for {
				collectAll(db)
				time.Sleep(24 * time.Hour)
			}
		}()
	}
	log.Printf("listening on %s", *addr)
	if !validLang(*lang) {
		log.Fatalf("-lang must be ko or ja, got %q", *lang)
	}
	log.Fatal(http.ListenAndServe(*addr, (&server{db: db, lang: *lang}).routes()))
}

// collectAll runs each collector and records the outcome in fetch_log.
// A first run backfills 3 years of retail and 60 days of wholesale; later
// runs only look at recent gaps.
func collectAll(db *DB) {
	official := newFetcher(2 * time.Second)

	months, days := 3, 7
	if !db.hasAny("retail") {
		months = 36
	}
	if !db.hasAny("wholesale") {
		days = 60
	}
	err := collectRetail(db, official, months)
	db.logFetch("estat", err)
	if err != nil {
		log.Printf("retail: %v", err)
	}
	err = collectWholesale(db, official, days)
	db.logFetch("tokyo-ota", err)
	if err != nil {
		log.Printf("wholesale: %v", err)
	}
	err = collectStores(db, newFetcher(5*time.Second))
	db.logFetch("aeon", err)
	if err != nil {
		log.Printf("stores: %v", err)
	}
}
