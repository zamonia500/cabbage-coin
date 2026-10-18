package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const userAgent = "cabbage-coin/1.0 (+https://cabbage.zamonia500.com)"

var errNotFound = errors.New("404 not found")

// fetcher spaces out requests so no source sees more than one request per delay.
type fetcher struct {
	client *http.Client
	delay  time.Duration
	mu     sync.Mutex
	last   time.Time
}

func newFetcher(delay time.Duration) *fetcher {
	return &fetcher{client: &http.Client{Timeout: 30 * time.Second}, delay: delay}
}

func (f *fetcher) get(url string) ([]byte, error) {
	f.mu.Lock()
	if wait := f.delay - time.Since(f.last); wait > 0 {
		time.Sleep(wait)
	}
	f.last = time.Now()
	f.mu.Unlock()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}
