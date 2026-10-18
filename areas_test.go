package main

import "testing"

func TestInitialScope(t *testing.T) {
	areas, err := loadAreas()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, a := range areas {
		if initialScope(a) {
			n++
		}
	}
	// 23 Tokyo wards + 171 designated-city wards (after Hamamatsu's 2024 reorganization).
	if n != 194 {
		t.Errorf("initial scope = %d areas, want 194", n)
	}
	for _, a := range areas {
		switch a.Name {
		case "目黒区", "横浜市港北区", "浜松市中央区":
			if !initialScope(a) {
				t.Errorf("%s should be in scope", a.Name)
			}
		case "横浜市", "八王子市", "府中町":
			if initialScope(a) {
				t.Errorf("%s should not be in scope", a.Name)
			}
		}
	}
}
