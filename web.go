package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

//go:embed web/static web/geo web/templates
var webFS embed.FS

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"yen": func(v float64) string {
		if math.IsNaN(v) {
			return "–"
		}
		return fmt.Sprintf("¥%s", comma(int(math.Round(v))))
	},
	"pct":   pct,
	"delta": deltaClass,
}).ParseFS(webFS, "web/templates/*.tmpl"))

func comma(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func pct(d float64) string {
	if math.IsNaN(d) || math.IsInf(d, 0) {
		return "–"
	}
	sign := ""
	if d > 0 {
		sign = "+"
	}
	return fmt.Sprintf("%s%.1f%%", sign, d*100)
}

// deltaClass names the arrow for a change: up is more expensive, which is bad news here.
func deltaClass(d float64) string {
	switch {
	case math.IsNaN(d) || math.Abs(d) < 0.005:
		return "flat"
	case d > 0:
		return "up"
	}
	return "down"
}

// Map classes on the diverging scale, -2 (much cheaper) … +2 (much dearer).
func classify(d, step1, step2 float64) int {
	switch {
	case math.IsNaN(d):
		return 0
	case d <= -step2:
		return -2
	case d < -step1:
		return -1
	case d <= step1:
		return 0
	case d < step2:
		return 1
	}
	return 2
}

type server struct {
	db   *DB
	lang string // where / redirects: ko for local testing, ja in production
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web/static")
	geo, _ := fs.Sub(webFS, "web/geo")
	mux.Handle("GET /static/", cacheFor(86400, http.StripPrefix("/static/", http.FileServerFS(static))))
	mux.Handle("GET /geo/", cacheFor(86400, http.StripPrefix("/geo/", http.FileServerFS(geo))))
	mux.HandleFunc("GET /{$}", s.root)
	for _, lang := range []string{"ko", "ja"} {
		page := func(w http.ResponseWriter, r *http.Request) {
			r.SetPathValue("lang", lang)
			s.page(w, r)
		}
		mux.HandleFunc("GET /"+lang+"/{$}", page)
		mux.HandleFunc("GET /"+lang+"/{kind}/{code}", page)
	}
	mux.HandleFunc("GET /api/map", s.apiMap)
	mux.HandleFunc("GET /api/panel", s.apiPanel)
	return mux
}

func cacheFor(seconds int, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", seconds))
		h.ServeHTTP(w, r)
	})
}

func (s *server) root(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/"+s.lang+"/", http.StatusFound)
}

func validLang(l string) bool { return l == "ko" || l == "ja" }

type searchEntry struct {
	Name string `json:"n"`
	Kind string `json:"k"`
	Code string `json:"c"`
}

func (s *server) page(w http.ResponseWriter, r *http.Request) {
	lang, kind, code := r.PathValue("lang"), r.PathValue("kind"), r.PathValue("code")
	if !validLang(lang) {
		http.NotFound(w, r)
		return
	}
	if kind == "" {
		kind = "home"
	}
	panel, err := s.panel(lang, kind, code)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Search index: prefectures (both languages) and municipalities.
	var idx []searchEntry
	for i := range prefNames {
		c := fmt.Sprintf("%02d", i+1)
		idx = append(idx, searchEntry{prefNames[i], "pref", c}, searchEntry{prefNamesKo[i], "pref", c})
	}
	areas, _ := loadAreas()
	for _, a := range areas {
		idx = append(idx, searchEntry{a.Name, "muni", a.Code})
	}
	boot, _ := json.Marshal(map[string]any{
		"lang": lang, "kind": kind, "code": code, "search": idx, "t": jsStrings[lang],
	})
	data := map[string]any{
		"Lang": lang, "T": msgs[lang], "Panel": panel, "Boot": template.JS(boot),
		"Collected": s.db.lastCollected(), "Path": strings.TrimPrefix(r.URL.Path, "/"+lang),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "page.tmpl", data); err != nil {
		log.Print(err)
	}
}

func (s *server) apiPanel(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lang := q.Get("lang")
	if !validLang(lang) {
		http.Error(w, "bad lang", http.StatusBadRequest)
		return
	}
	panel, err := s.panel(lang, q.Get("kind"), q.Get("code"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(panel))
}

func (s *server) panel(lang, kind, code string) (template.HTML, error) {
	var data map[string]any
	switch kind {
	case "home":
		data = s.homeData(lang)
	case "pref":
		if p, err := strconv.Atoi(code); err != nil || p < 1 || p > 47 || len(code) != 2 {
			return "", fmt.Errorf("bad pref %q", code)
		}
		data = s.prefData(lang, code)
	case "muni":
		a, ok := s.db.area(code)
		if !ok {
			return "", fmt.Errorf("bad area %q", code)
		}
		data = s.muniData(lang, a)
	default:
		return "", fmt.Errorf("bad kind %q", kind)
	}
	data["T"], data["Lang"] = msgs[lang], lang
	var b strings.Builder
	if err := tmpl.ExecuteTemplate(&b, kind+".tmpl", data); err != nil {
		return "", err
	}
	return template.HTML(b.String()), nil
}

func prefName(lang string, p int) string {
	if lang == "ko" {
		return prefNamesKo[p-1]
	}
	return prefNames[p-1]
}

func prefCode(p int) string { return fmt.Sprintf("%02d", p) }

func (s *server) homeData(lang string) map[string]any {
	ym := s.db.latestYM()
	cur, prev := s.db.retailByCity(ym), s.db.retailByCity(s.db.prevYM(ym))
	byPref := retailByPref(cur)
	nat, natPrev := median(values(cur)), median(values(prev))
	var rank []namedValue
	for p, v := range byPref {
		pi, _ := strconv.Atoi(p)
		rank = append(rank, namedValue{Code: p, Name: prefName(lang, pi), Value: v, Delta: v/nat - 1})
	}
	sort.Slice(rank, func(i, j int) bool { return rank[i].Value > rank[j].Value })

	yms, natSeries := s.db.retailSeries("")
	dates, ws := s.db.wholesaleSeries("")
	wDate, origins := s.db.wholesaleOrigins()
	for i := range origins {
		origins[i].Name = originName(lang, origins[i].Name)
	}
	return map[string]any{
		"YM": ym, "National": nat, "NationalDelta": nat/natPrev - 1, "Rank": rank,
		"RetailChart":    lineChart(lang, yms, []chartSeries{{Name: msgs[lang]["national"], Values: natSeries, Color: colorFocus}}, monthLabel),
		"WholesaleChart": lineChart(lang, dates, []chartSeries{{Name: msgs[lang]["ota_mid"], Values: ws, Color: colorFocus}}, dayLabel),
		"WholesaleDate":  wDate, "Origins": origins,
	}
}

func (s *server) prefData(lang, code string) map[string]any {
	p, _ := strconv.Atoi(code)
	ym := s.db.latestYM()
	cur, prev := s.db.retailByCity(ym), s.db.retailByCity(s.db.prevYM(ym))
	v, pv := retailByPref(cur)[code], retailByPref(prev)[code]
	nat := median(values(cur))
	yms, ps := s.db.retailSeries(code)
	_, ns := s.db.retailSeries("")
	series := []chartSeries{{Name: prefName(lang, p), Values: ps, Color: colorFocus}, {Name: msgs[lang]["national"], Values: ns, Color: colorContext}}

	// Retail cities and store-covered municipalities in this prefecture.
	areas, _ := loadAreas()
	names := map[string]string{}
	for _, a := range areas {
		names[a.Code] = a.Name
	}
	var cities []namedValue
	for c, y := range cur {
		if c[:2] == code {
			n := names[c]
			if n == "" && c == "13100" {
				n = msgs[lang]["tokyo_wards"]
			}
			cities = append(cities, namedValue{Code: c, Name: n, Value: y, Delta: y/nat - 1})
		}
	}
	sort.Slice(cities, func(i, j int) bool { return cities[i].Value > cities[j].Value })
	sDate, sp, counts := s.db.storePrices(code)
	var stores []namedValue
	for a, y := range sp {
		stores = append(stores, namedValue{Code: a, Name: names[a], Value: y, Delta: float64(counts[a])})
	}
	sort.Slice(stores, func(i, j int) bool { return stores[i].Value < stores[j].Value })

	origin := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(prefNames[p-1], "県"), "府"), "都")
	dates, ws := s.db.wholesaleSeries(origin)
	var wchart template.HTML
	if len(ws) > 1 {
		wchart = lineChart(lang, dates, []chartSeries{{Name: originName(lang, origin), Values: ws, Color: colorFocus}}, dayLabel)
	}
	return map[string]any{
		"Code": code, "Name": prefName(lang, p), "YM": ym, "Value": v, "MoM": v/pv - 1, "VsNational": v/nat - 1,
		"HasRetail": v > 0, "RetailChart": lineChart(lang, yms, series, monthLabel),
		"Cities": cities, "Stores": stores, "StoreDate": sDate, "WholesaleChart": wchart,
	}
}

func (s *server) muniData(lang string, a Area) map[string]any {
	_, sp, counts := s.db.storePrices(a.Code[:2])
	dates, series := s.db.storeSeries(a.Code)
	var chart template.HTML
	if len(series) > 1 {
		chart = lineChart(lang, dates, []chartSeries{{Name: a.Name, Values: series, Color: colorFocus}}, dayLabel)
	}
	v, ok := sp[a.Code]
	if !ok {
		v = math.NaN()
	}
	return map[string]any{
		"Code": a.Code, "Name": a.Name, "Pref": prefName(lang, a.Pref), "PrefCode": prefCode(a.Pref),
		"Population": comma(a.Population), "Enabled": s.db.enabled(a.Code), "Value": v, "Count": counts[a.Code],
		"Stores": s.db.storesIn(a.Code), "Chart": chart,
	}
}

// originName shows a wholesale origin (群馬, 各地) in the page language.
func originName(lang, origin string) string {
	if lang != "ko" {
		return origin
	}
	for i, n := range prefNames {
		if strings.TrimRight(n, "都道府県") == origin || n == origin {
			return strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(prefNamesKo[i], "현"), "부"), "도")
		}
	}
	return origin
}

type mapValue struct {
	V float64 `json:"v"`
	C int     `json:"c"`
	D float64 `json:"d"`
	N int     `json:"n,omitempty"`
}

// apiMap returns the values the map paints: code → value, class, delta.
//
//	level=pref  metric=retail|mom
//	level=muni  metric=store|retail  pref=NN
func (s *server) apiMap(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	level, metric, pref := q.Get("level"), q.Get("metric"), q.Get("pref")
	out := map[string]mapValue{}
	var label string
	ym := s.db.latestYM()
	cur := s.db.retailByCity(ym)
	nat := median(values(cur))
	switch {
	case level == "pref" && (metric == "retail" || metric == "mom"):
		label = ym
		prev := retailByPref(s.db.retailByCity(s.db.prevYM(ym)))
		for p, v := range retailByPref(cur) {
			mv := mapValue{V: v, D: v/nat - 1, C: classify(v/nat-1, 0.07, 0.2)}
			if metric == "mom" {
				mom := v/prev[p] - 1
				mv.D, mv.C = mom, classify(mom, 0.03, 0.1)
			}
			out[p] = sanitize(mv)
		}
	case level == "muni" && metric == "store" && len(pref) == 2:
		date, sp, counts := s.db.storePrices("")
		label = date
		ref := median(values(sp))
		for a, v := range sp {
			if a[:2] == pref {
				out[a] = sanitize(mapValue{V: v, D: v/ref - 1, C: classify(v/ref-1, 0.07, 0.2), N: counts[a]})
			}
		}
	case level == "muni" && metric == "retail" && len(pref) == 2:
		label = ym
		areas, _ := loadAreas()
		for _, a := range areas {
			if a.Code[:2] != pref {
				continue
			}
			if v, ok := cityPrice(a, cur); ok {
				out[a.Code] = sanitize(mapValue{V: v, D: v/nat - 1, C: classify(v/nat-1, 0.07, 0.2)})
			}
		}
	default:
		http.Error(w, "bad level/metric", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"label": label, "values": out})
}

// cityPrice finds the surveyed retail price for a municipality: its own, or for
// a ward, its city's (Tokyo's 23 wards share 13100 東京都区部).
func cityPrice(a Area, cities map[string]float64) (float64, bool) {
	if v, ok := cities[a.Code]; ok {
		return v, true
	}
	if !initialScope(a) {
		return 0, false
	}
	best := ""
	for c := range cities {
		// A designated city's code ends in 0 and its wards follow it.
		if c[:3] == a.Code[:3] && c[4] == '0' && c < a.Code && c > best {
			best = c
		}
	}
	v, ok := cities[best]
	return v, ok
}

// sanitize keeps NaN/Inf out of JSON.
func sanitize(m mapValue) mapValue {
	if math.IsNaN(m.D) || math.IsInf(m.D, 0) {
		m.D = 0
	}
	return m
}
