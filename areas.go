package main

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"regexp"
	"strconv"
)

//go:embed data/areas.csv
var areasCSV []byte

type Area struct {
	Code       string
	Pref       int
	Name       string
	Population int
}

func loadAreas() ([]Area, error) {
	recs, err := csv.NewReader(bytes.NewReader(areasCSV)).ReadAll()
	if err != nil {
		return nil, err
	}
	out := make([]Area, 0, len(recs)-1)
	for _, r := range recs[1:] { // code,pref,name,population
		pref, _ := strconv.Atoi(r[0][:2])
		pop, _ := strconv.Atoi(r[3])
		out = append(out, Area{Code: r[0], Pref: pref, Name: r[2], Population: pop})
	}
	return out, nil
}

var designatedWard = regexp.MustCompile(`^.+市.+区$`)

// initialScope is where store collection starts: Tokyo's 23 special wards
// and every designated-city ward. Widening it later is an UPDATE on area.enabled.
func initialScope(a Area) bool {
	if a.Pref == 13 && a.Code >= "13101" && a.Code <= "13123" {
		return true
	}
	return designatedWard.MatchString(a.Name)
}

// seedAreas fills the area table on first start; later edits to enabled survive restarts.
func seedAreas(db *DB) error {
	if db.hasAny("area") {
		return nil
	}
	areas, err := loadAreas()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, a := range areas {
		if _, err := tx.Exec(`INSERT INTO area(code, pref_code, name_ja, population, enabled) VALUES(?, ?, ?, ?, ?)`,
			a.Code, a.Pref, a.Name, a.Population, initialScope(a)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
