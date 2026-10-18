package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func testServer(t *testing.T, lang string) http.Handler {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := seedAreas(db); err != nil {
		t.Fatal(err)
	}
	ps, err := parseEstatCity(readTestdata(t, "estat_city_202608.xlsx"), "2026-08")
	if err != nil {
		t.Fatal(err)
	}
	prev := make([]RetailPrice, len(ps))
	for i, p := range ps {
		prev[i] = RetailPrice{CityCode: p.CityCode, YM: "2026-07", Yen: p.Yen - 10}
	}
	if err := db.upsertRetail(append(ps, prev...)); err != nil {
		t.Fatal(err)
	}
	return (&server{db: db, lang: lang}).routes()
}

func TestPages(t *testing.T) {
	h := testServer(t, "ko")
	for path, want := range map[string]int{
		"/ko/": 200, "/ja/": 200, "/ko/pref/08": 200, "/ja/pref/13": 200, "/ko/muni/13110": 200,
		"/ko/pref/99": 404, "/ko/pref/8": 404, "/ko/muni/99999": 404, "/xx/": 404,
		"/static/app.js": 200, "/geo/muni/13.json": 200, "/api/panel?lang=ko&kind=home": 200,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", path, rec.Code, want)
		}
	}
	for lang, h := range map[string]http.Handler{"ko": h, "ja": testServer(t, "ja")} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		if loc := rec.Header().Get("Location"); loc != "/"+lang+"/" {
			t.Errorf("-lang %s: / redirected to %q", lang, loc)
		}
	}
}

func TestAPIMap(t *testing.T) {
	h := testServer(t, "ko")
	get := func(q string) map[string]mapValue {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/map?"+q, nil))
		var r struct{ Values map[string]mapValue }
		if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
			t.Fatalf("%s: %v (%s)", q, err, rec.Body)
		}
		return r.Values
	}
	pref := get("level=pref&metric=retail")
	if len(pref) != 47 {
		t.Errorf("pref values: %d, want 47", len(pref))
	}
	if v := pref["01"]; v.C < -2 || v.C > 2 || v.V <= 0 {
		t.Errorf("Hokkaido: %+v", v)
	}
	if mom := get("level=pref&metric=mom")["13"]; mom.D <= 0 {
		t.Errorf("Tokyo MoM should be positive (prices rose ¥10): %+v", mom)
	}
	muni := get("level=muni&pref=14&metric=retail")
	if muni["14109"].V == 0 || muni["14109"].V != muni["14100"].V {
		t.Errorf("Yokohama's wards should take Yokohama's price: %+v vs %+v", muni["14109"], muni["14100"])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/map?level=muni&metric=store", nil))
	if rec.Code != 400 {
		t.Errorf("missing pref: %d, want 400", rec.Code)
	}
}

func TestCityPrice(t *testing.T) {
	cities := map[string]float64{"13100": 224, "14100": 207, "14130": 212, "22100": 1, "22130": 2}
	for code, want := range map[string]float64{"13110": 224, "14109": 207, "14135": 212, "22138": 2, "22101": 1} {
		a := Area{Code: code, Pref: 13, Name: "目黒区"}
		if code[:2] != "13" {
			a = Area{Code: code, Pref: 14, Name: "某市某区"}
		}
		got, ok := cityPrice(a, cities)
		if !ok || got != want {
			t.Errorf("%s: %v %v, want %v", code, got, ok, want)
		}
	}
}

func TestClassify(t *testing.T) {
	for d, want := range map[float64]int{-0.3: -2, -0.1: -1, 0: 0, 0.07: 0, 0.1: 1, 0.25: 2} {
		if got := classify(d, 0.07, 0.2); got != want {
			t.Errorf("classify(%v) = %d, want %d", d, got, want)
		}
	}
}
