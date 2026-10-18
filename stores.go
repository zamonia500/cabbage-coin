package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"html"
	"log"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Aeon Net Super. robots.txt allows product pages (/netsuper/<store>/<item>.html)
// and store pages (/netsuper/<store>/) but not search or category listings, so
// a store's cabbage product cannot be discovered automatically: each store is
// registered from a product URL someone found by browsing (data/stores.txt).
const (
	aeonHost   = "https://shop.aeon.com"
	aeonSource = "aeon"
	foodTax    = 1.08 // reduced consumption tax rate on food
)

//go:embed data/stores.txt
var storesTxt []byte

var aeonProductURL = regexp.MustCompile(`^https://shop\.aeon\.com/netsuper/(\d{14})/[0-9A-Za-z]+\.html$`)

type product struct {
	Name  string
	Unit  string
	ExTax int
}

var (
	metaPrice = regexp.MustCompile(`<meta property="product:price:amount" content="(\d+)"`)
	metaTitle = regexp.MustCompile(`<meta property="og:title" content="([^"]*)"`)
)

func parseAeonProduct(page []byte) (product, error) {
	p := metaPrice.FindSubmatch(page)
	t := metaTitle.FindSubmatch(page)
	if p == nil || t == nil {
		return product{}, errors.New("aeon: no product price/title meta (product gone?)")
	}
	yen, _ := strconv.Atoi(string(p[1]))
	name := html.UnescapeString(string(t[1]))
	return product{Name: name, Unit: cabbageUnit(name), ExTax: yen}, nil
}

// cabbageUnit tells a whole head from cut pieces; only whole heads are compared on the map.
func cabbageUnit(name string) string {
	switch {
	case strings.Contains(name, "1/4") || strings.Contains(name, "1／4") || strings.Contains(name, "4分の1"):
		return "quarter"
	case strings.Contains(name, "1/2") || strings.Contains(name, "1／2") || strings.Contains(name, "半分") || strings.Contains(name, "ハーフ"):
		return "half"
	}
	return "whole"
}

var storeInfo = regexp.MustCompile(`(?s)<span>店舗情報</span>.*?<p>(.*?)</p>`)

// parseAeonStore reads the 店舗情報 widget: company, store name, 〒zip, address, ...
func parseAeonStore(page []byte) (name, address string, err error) {
	m := storeInfo.FindSubmatch(page)
	if m == nil {
		return "", "", errors.New("aeon: no 店舗情報 block")
	}
	lines := strings.Split(string(m[1]), "<br />")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(html.UnescapeString(l))
	}
	for i := 1; i+1 < len(lines); i++ {
		if strings.HasPrefix(lines[i], "〒") {
			return lines[i-1], lines[i+1], nil
		}
	}
	return "", "", errors.New("aeon: no 〒 line in 店舗情報")
}

var county = regexp.MustCompile(`^[^市区町村]+?郡`)

// areaFromAddress finds the municipality an address lies in: the longest
// area name in that prefecture that the rest of the address starts with.
// Town names carry their county (安芸郡府中町); addresses may drop it.
func areaFromAddress(address string, areas []Area) (string, error) {
	best, bestLen := "", 0
	for _, a := range areas {
		rest, ok := strings.CutPrefix(address, prefNames[a.Pref-1])
		if !ok {
			continue
		}
		for _, name := range []string{a.Name, county.ReplaceAllString(a.Name, "")} {
			if strings.HasPrefix(rest, name) && len(name) > bestLen {
				best, bestLen = a.Code, len(name)
			}
		}
	}
	if best == "" {
		return "", fmt.Errorf("no municipality for address %q", address)
	}
	return best, nil
}

// robotsCheck fetches robots.txt once per run and checks a URL against it.
type robotsCheck struct {
	f     *fetcher
	cache map[string][]byte
}

func (r *robotsCheck) allowed(rawURL string) (bool, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false, err
	}
	origin := u.Scheme + "://" + u.Host
	body, ok := r.cache[origin]
	if !ok {
		body, err = r.f.get(origin + "/robots.txt")
		if errors.Is(err, errNotFound) {
			body, err = nil, nil // no robots.txt: everything allowed
		}
		if err != nil {
			return false, err
		}
		r.cache[origin] = body
	}
	return robotsAllowed(body, u.Path), nil
}

// registerAeonStore adds a store from one of its cabbage product pages.
func registerAeonStore(db *DB, f *fetcher, rc *robotsCheck, productURL string, areas []Area) error {
	m := aeonProductURL.FindStringSubmatch(productURL)
	if m == nil {
		return fmt.Errorf("not an Aeon Net Super product URL: %s", productURL)
	}
	storeCode := m[1]
	storeURL := aeonHost + "/netsuper/" + storeCode + "/"
	for _, u := range []string{productURL, storeURL} {
		ok, err := rc.allowed(u)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("robots.txt disallows %s", u)
		}
	}
	page, err := f.get(productURL)
	if err != nil {
		return err
	}
	if _, err := parseAeonProduct(page); err != nil {
		return fmt.Errorf("%s: %w", productURL, err)
	}
	page, err = f.get(storeURL)
	if err != nil {
		return err
	}
	name, address, err := parseAeonStore(page)
	if err != nil {
		return err
	}
	area, err := areaFromAddress(address, areas)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO store(id, source, chain, name, area_code, url) VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, area_code = excluded.area_code, url = excluded.url, active = 1`,
		aeonSource+":"+storeCode, aeonSource, "イオン", name, area, productURL)
	if err == nil {
		log.Printf("store registered: %s (%s) → %s", name, address, area)
	}
	return err
}

// collectStores registers stores listed in data/stores.txt that are new, then
// records today's price at every active store in an enabled area.
func collectStores(db *DB, f *fetcher) error {
	rc := &robotsCheck{f: f, cache: map[string][]byte{}}
	areas, err := loadAreas()
	if err != nil {
		return err
	}
	var errs []error
	sc := bufio.NewScanner(bytes.NewReader(storesTxt))
	for sc.Scan() {
		u := strings.TrimSpace(sc.Text())
		if u == "" || strings.HasPrefix(u, "#") {
			continue
		}
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM store WHERE url = ?`, u).Scan(&n)
		if n > 0 {
			continue
		}
		if err := registerAeonStore(db, f, rc, u, areas); err != nil {
			errs = append(errs, err)
		}
	}

	rows, err := db.Query(`SELECT s.id, s.url FROM store s JOIN area a ON a.code = s.area_code
		WHERE s.active = 1 AND a.enabled = 1 AND s.source = ?`, aeonSource)
	if err != nil {
		return err
	}
	type target struct{ id, url string }
	var targets []target
	for rows.Next() {
		var t target
		rows.Scan(&t.id, &t.url)
		targets = append(targets, t)
	}
	rows.Close()

	today := time.Now().Format("2006-01-02")
	for _, t := range targets {
		ok, err := rc.allowed(t.url)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !ok {
			errs = append(errs, fmt.Errorf("robots.txt now disallows %s; skipped", t.url))
			continue
		}
		page, err := f.get(t.url) // one attempt; a failure waits for tomorrow
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", t.id, err))
			continue
		}
		p, err := parseAeonProduct(page)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", t.id, err))
			continue
		}
		// shortcut: the meta tag carries the current selling price with no sale flag, so on_sale stays 0.
		err = db.upsertStorePrice(StorePrice{StoreID: t.id, Date: today, Product: p.Name, Unit: p.Unit,
			ExTax: p.ExTax, InTax: int(math.Round(float64(p.ExTax) * foodTax))})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		log.Printf("store %s: %s ¥%d", t.id, p.Name, p.ExTax)
	}
	return errors.Join(errs...)
}
