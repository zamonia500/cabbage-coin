package main

import (
	"database/sql"
	"math"
	"sort"
)

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func (db *DB) latestYM() string {
	var ym sql.NullString
	db.QueryRow(`SELECT MAX(ym) FROM retail`).Scan(&ym)
	return ym.String
}

func (db *DB) prevYM(ym string) string {
	var p sql.NullString
	db.QueryRow(`SELECT MAX(ym) FROM retail WHERE ym < ?`, ym).Scan(&p)
	return p.String
}

// retailByCity returns city code → yen/kg for a month.
func (db *DB) retailByCity(ym string) map[string]float64 {
	out := map[string]float64{}
	rows, err := db.Query(`SELECT city_code, yen_per_kg FROM retail WHERE ym = ?`, ym)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		var y float64
		rows.Scan(&c, &y)
		out[c] = y
	}
	return out
}

// retailByPref aggregates surveyed cities to their prefecture by median.
func retailByPref(cities map[string]float64) map[string]float64 {
	byPref := map[string][]float64{}
	for c, y := range cities {
		byPref[c[:2]] = append(byPref[c[:2]], y)
	}
	out := map[string]float64{}
	for p, ys := range byPref {
		out[p] = median(ys)
	}
	return out
}

func values(m map[string]float64) []float64 {
	out := make([]float64, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// retailSeries returns ym → prefecture median (pref "" = all cities nationwide).
func (db *DB) retailSeries(pref string) ([]string, []float64) {
	rows, err := db.Query(`SELECT ym, city_code, yen_per_kg FROM retail WHERE ? = '' OR substr(city_code, 1, 2) = ? ORDER BY ym`, pref, pref)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()
	var yms []string
	byYM := map[string][]float64{}
	for rows.Next() {
		var ym, c string
		var y float64
		rows.Scan(&ym, &c, &y)
		if len(byYM[ym]) == 0 {
			yms = append(yms, ym)
		}
		byYM[ym] = append(byYM[ym], y)
	}
	out := make([]float64, len(yms))
	for i, ym := range yms {
		out[i] = median(byYM[ym])
	}
	return yms, out
}

// wholesaleSeries returns per-day 相対 (negotiated, the bulk of volume) mid
// price per kg at Ota: the median across origins, or one origin's when given.
func (db *DB) wholesaleSeries(origin string) ([]string, []float64) {
	rows, err := db.Query(`SELECT date, mid * 1.0 / unit_kg FROM wholesale
		WHERE method = '相対' AND mid IS NOT NULL AND origin != '各地' AND (? = '' OR origin = ?) ORDER BY date`, origin, origin)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()
	var dates []string
	byDate := map[string][]float64{}
	for rows.Next() {
		var d string
		var y float64
		rows.Scan(&d, &y)
		if len(byDate[d]) == 0 {
			dates = append(dates, d)
		}
		byDate[d] = append(byDate[d], y)
	}
	out := make([]float64, len(dates))
	for i, d := range dates {
		out[i] = median(byDate[d])
	}
	return dates, out
}

// wholesaleOrigins lists the origins traded on the latest day, with their 相対 mid per kg.
func (db *DB) wholesaleOrigins() (string, []namedValue) {
	var date sql.NullString
	db.QueryRow(`SELECT MAX(date) FROM wholesale`).Scan(&date)
	rows, err := db.Query(`SELECT origin, mid * 1.0 / unit_kg FROM wholesale
		WHERE date = ? AND method = '相対' AND mid IS NOT NULL AND origin != '各地' ORDER BY mid DESC`, date.String)
	if err != nil {
		return date.String, nil
	}
	defer rows.Close()
	var out []namedValue
	for rows.Next() {
		var nv namedValue
		rows.Scan(&nv.Name, &nv.Value)
		out = append(out, nv)
	}
	return date.String, out
}

type namedValue struct {
	Code, Name string
	Value      float64
	Delta      float64 // fraction vs reference; NaN when unknown
}

type storeRow struct {
	Name, Product, Date string
	InTax               int
}

// storePrices returns, per area, the median whole-head tax-included price over
// each store's latest observation within a week of the newest data.
func (db *DB) storePrices(pref string) (string, map[string]float64, map[string]int) {
	var latest sql.NullString
	db.QueryRow(`SELECT MAX(date) FROM store_price`).Scan(&latest)
	rows, err := db.Query(`SELECT s.area_code, p.yen_in_tax FROM store_price p JOIN store s ON s.id = p.store_id
		WHERE p.unit = 'whole' AND (? = '' OR substr(s.area_code, 1, 2) = ?)
		  AND p.date = (SELECT MAX(date) FROM store_price q WHERE q.store_id = p.store_id AND q.unit = 'whole')
		  AND p.date >= date(?, '-7 days')`, pref, pref, latest.String)
	if err != nil {
		return latest.String, nil, nil
	}
	defer rows.Close()
	byArea := map[string][]float64{}
	for rows.Next() {
		var a string
		var y float64
		rows.Scan(&a, &y)
		byArea[a] = append(byArea[a], y)
	}
	out, counts := map[string]float64{}, map[string]int{}
	for a, ys := range byArea {
		out[a], counts[a] = median(ys), len(ys)
	}
	return latest.String, out, counts
}

func (db *DB) storesIn(area string) []storeRow {
	rows, err := db.Query(`SELECT s.name, p.product, p.date, p.yen_in_tax FROM store s
		JOIN store_price p ON p.store_id = s.id
		WHERE s.area_code = ? AND p.date = (SELECT MAX(date) FROM store_price q WHERE q.store_id = s.id)
		ORDER BY p.yen_in_tax`, area)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []storeRow
	for rows.Next() {
		var r storeRow
		rows.Scan(&r.Name, &r.Product, &r.Date, &r.InTax)
		out = append(out, r)
	}
	return out
}

// storeSeries is the area's daily median whole-head price over the last 30 days.
func (db *DB) storeSeries(area string) ([]string, []float64) {
	rows, err := db.Query(`SELECT p.date, p.yen_in_tax FROM store_price p JOIN store s ON s.id = p.store_id
		WHERE s.area_code = ? AND p.unit = 'whole' AND p.date >= date('now', '-30 days') ORDER BY p.date`, area)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()
	var dates []string
	byDate := map[string][]float64{}
	for rows.Next() {
		var d string
		var y float64
		rows.Scan(&d, &y)
		if len(byDate[d]) == 0 {
			dates = append(dates, d)
		}
		byDate[d] = append(byDate[d], y)
	}
	out := make([]float64, len(dates))
	for i, d := range dates {
		out[i] = median(byDate[d])
	}
	return dates, out
}

func (db *DB) area(code string) (Area, bool) {
	var a Area
	err := db.QueryRow(`SELECT code, pref_code, name_ja, COALESCE(population, 0) FROM area WHERE code = ?`, code).
		Scan(&a.Code, &a.Pref, &a.Name, &a.Population)
	return a, err == nil
}

func (db *DB) enabled(code string) bool {
	var n int
	db.QueryRow(`SELECT enabled FROM area WHERE code = ?`, code).Scan(&n)
	return n == 1
}

func (db *DB) lastCollected() string {
	var t sql.NullString
	db.QueryRow(`SELECT MAX(last_ok) FROM fetch_log`).Scan(&t)
	return t.String
}
