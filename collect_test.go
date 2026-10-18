package main

import (
	"os"
	"testing"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEstatMonth(t *testing.T) {
	for m, want := range map[int]string{1: "11010301", 4: "12040604", 8: "23070908", 12: "24101212"} {
		if got := estatMonth(m); got != want {
			t.Errorf("estatMonth(%d) = %s, want %s", m, got, want)
		}
	}
}

func TestParseEstatList(t *testing.T) {
	page := readTestdata(t, "estat_list_202608.html")
	id, err := parseEstatList(page, 2026, 8)
	if err != nil || id != "000040506177" {
		t.Fatalf("got %q, %v", id, err)
	}
	if _, err := parseEstatList(page, 2026, 9); err != errNotFound {
		t.Fatalf("unpublished month: got %v, want errNotFound", err)
	}
}

func TestParseEstatCity(t *testing.T) {
	ps, err := parseEstatCity(readTestdata(t, "estat_city_202608.xlsx"), "2026-08")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, p := range ps {
		got[p.CityCode] = p.Yen
	}
	// Sapporo and Hakodate, read off the sheet by hand.
	if got["01100"] != 222 || got["01202"] != 161 {
		t.Errorf("Sapporo/Hakodate = %d/%d, want 222/161", got["01100"], got["01202"])
	}
	if len(ps) < 47 {
		t.Errorf("only %d cities", len(ps))
	}
}

func TestParseTokyoCSV(t *testing.T) {
	ps, err := parseTokyoCSV(readTestdata(t, "tokyo_ota_20261016.csv"), "2026-10-16", marketOta)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 9 {
		t.Fatalf("got %d cabbage rows, want 9", len(ps))
	}
	p := ps[3] // 相対, 群馬: 2916 / 2052 / 864 per 10kg
	if p.Method != "相対" || p.Origin != "群馬" || p.UnitKg != 10 || p.Hi.Int64 != 2916 || p.Mid.Int64 != 2052 || p.Lo.Int64 != 864 {
		t.Errorf("row 3 = %+v", p)
	}
	if ps[2].Hi.Valid { // せり・入札, 各地: no trade
		t.Errorf("row 2 should have no price: %+v", ps[2])
	}
}
