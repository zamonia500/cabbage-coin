package main

import "testing"

func TestParseAeonProduct(t *testing.T) {
	p, err := parseAeonProduct(readTestdata(t, "aeon_item_head.html"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "群馬県などの国内産 キャベツ 1個" || p.ExTax != 398 || p.Unit != "whole" {
		t.Errorf("got %+v", p)
	}
	if _, err := parseAeonProduct([]byte("<html>page-not-registration</html>")); err == nil {
		t.Error("a page without product meta must fail")
	}
}

func TestCabbageUnit(t *testing.T) {
	for name, want := range map[string]string{
		"キャベツ 1個": "whole", "キャベツ 1/2カット": "half", "キャベツ 1／4カット": "quarter", "春キャベツ ハーフ": "half",
	} {
		if got := cabbageUnit(name); got != want {
			t.Errorf("%s: got %s, want %s", name, got, want)
		}
	}
}

func TestParseAeonStore(t *testing.T) {
	name, addr, err := parseAeonStore(readTestdata(t, "aeon_store_info.html"))
	if err != nil {
		t.Fatal(err)
	}
	if name != "イオンスタイル碑文谷" || addr != "東京都目黒区碑文谷4-1-1" {
		t.Errorf("got %q, %q", name, addr)
	}
}

func TestAreaFromAddress(t *testing.T) {
	areas, err := loadAreas()
	if err != nil {
		t.Fatal(err)
	}
	for addr, want := range map[string]string{
		"東京都目黒区碑文谷4-1-1":        "13110",
		"愛知県名古屋市緑区南大高2丁目450番":   "23114",
		"東京都府中市宮町1-1":           "13206",
		"広島県安芸郡府中町大須2丁目1-1":     "34302", // not Tokyo's 府中市
		"広島県府中町大須2丁目1-1":        "34302", // county omitted
		"千葉県鎌ヶ谷市新鎌ヶ谷 2-7-1":     "12224", // ヶ vs ケ in the MIC table
		"神奈川県横浜市港北区新横浜2-100-45": "14109",
	} {
		got, err := areaFromAddress(addr, areas)
		if err != nil || got != want {
			t.Errorf("%s: got %s, %v; want %s", addr, got, err, want)
		}
	}
	if _, err := areaFromAddress("unknown", areas); err == nil {
		t.Error("unknown address must fail")
	}
}
