package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"time"
)

func main() {
	dataDir := flag.String("data", ".", "directory holding cabbage.db")
	once := flag.Bool("once", false, "run every collector once, then exit")
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
	go func() {
		for {
			collectAll(db)
			time.Sleep(24 * time.Hour)
		}
	}()
	select {}
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
}
