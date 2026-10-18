// Command gendata regenerates the reference data embedded in the binary:
//
//	data/areas.csv        municipalities with population (総務省 住民基本台帳)
//	web/geo/pref.json     prefecture boundaries
//	web/geo/muni/NN.json  municipality boundaries, one file per prefecture
//
// Boundaries are smartnews-smri/japan-topography's simplified 国土数値情報 N03
// (2021). Run from the repo root: go run ./tools/gendata
//
// shortcut: 2021 boundaries predate Hamamatsu's 2024 ward merge, so its three
// new wards (22138-22140) have no shape; switch to a newer N03 release when it
// matters.
package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/zamonia500/cabbage-coin/internal/xlsx"
)

const (
	// 【総計】令和8年住民基本台帳人口・世帯数（市区町村別）
	populationURL = "https://www.soumu.go.jp/main_content/000892952.xlsx"
	topoBase      = "https://raw.githubusercontent.com/smartnews-smri/japan-topography/main/data/municipality/geojson/"
)

func main() {
	prefs, err := areas()
	if err != nil {
		log.Fatal(err)
	}
	if err := geo(prefs); err != nil {
		log.Fatal(err)
	}
}

var code6 = regexp.MustCompile(`^\d{6}$`)

// areas writes data/areas.csv and returns prefecture name → 2-digit code.
func areas() (map[string]string, error) {
	data, err := get(populationURL)
	if err != nil {
		return nil, err
	}
	rows, err := xlsx.Rows(data)
	if err != nil {
		return nil, err
	}
	f, err := os.Create("data/areas.csv")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Write([]string{"code", "pref", "name", "population"})
	prefs := map[string]string{}
	n := 0
	for _, r := range rows {
		if !code6.MatchString(r["A"]) {
			continue
		}
		code := r["A"][:5] // drop the check digit
		if r["C"] == "-" {
			prefs[r["B"]] = code[:2]
			continue
		}
		if strings.HasSuffix(r["C"], "郡") || r["C"] == "島しょ" { // subtotals, not municipalities
			continue
		}
		w.Write([]string{code, r["B"], r["C"], r["F"]})
		n++
	}
	w.Flush()
	log.Printf("data/areas.csv: %d municipalities, %d prefectures", n, len(prefs))
	return prefs, w.Error()
}

type feature struct {
	Type       string           `json:"type"`
	Properties map[string]any   `json:"properties"`
	Geometry   *json.RawMessage `json:"geometry"`
}

type collection struct {
	Type     string    `json:"type"`
	Features []feature `json:"features"`
}

func geo(prefs map[string]string) error {
	if err := os.MkdirAll("web/geo/muni", 0o755); err != nil {
		return err
	}
	// Prefectures: the 0.1% simplification is plenty at country zoom.
	fc, err := fetchCollection(topoBase + "s0001/prefectures.json")
	if err != nil {
		return err
	}
	for i := range fc.Features {
		name, _ := fc.Features[i].Properties["N03_001"].(string)
		code, ok := prefs[name]
		if !ok {
			return fmt.Errorf("unknown prefecture %q", name)
		}
		fc.Features[i].Properties = map[string]any{"code": code, "name": name}
	}
	if err := write("web/geo/pref.json", fc); err != nil {
		return err
	}
	for p := 1; p <= 47; p++ {
		fc, err := fetchCollection(fmt.Sprintf("%ss0010/N03-21_%02d_210101.json", topoBase, p))
		if err != nil {
			return err
		}
		out := fc.Features[:0]
		for _, f := range fc.Features {
			code, _ := f.Properties["N03_007"].(string)
			if code == "" || f.Geometry == nil { // unsettled territory has no code
				continue
			}
			name, _ := f.Properties["N03_004"].(string)
			if city, _ := f.Properties["N03_003"].(string); strings.HasSuffix(city, "市") && strings.HasSuffix(name, "区") {
				name = city + name // designated-city ward, e.g. 横浜市港北区
			}
			f.Properties = map[string]any{"code": code, "name": name}
			out = append(out, f)
		}
		fc.Features = out
		if err := write(fmt.Sprintf("web/geo/muni/%02d.json", p), fc); err != nil {
			return err
		}
	}
	return nil
}

func fetchCollection(url string) (*collection, error) {
	data, err := get(url)
	if err != nil {
		return nil, err
	}
	var fc collection
	if err := json.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	return &fc, nil
}

// write saves the collection with coordinates rounded to 4 decimals (~10m),
// which roughly halves the size without visible change at our zoom levels.
func write(path string, fc *collection) error {
	for i, f := range fc.Features {
		var g struct {
			Type        string `json:"type"`
			Coordinates any    `json:"coordinates"`
		}
		if err := json.Unmarshal(*f.Geometry, &g); err != nil {
			return err
		}
		g.Coordinates = round(g.Coordinates)
		b, err := json.Marshal(g)
		if err != nil {
			return err
		}
		raw := json.RawMessage(b)
		fc.Features[i].Geometry = &raw
	}
	b, err := json.Marshal(fc)
	if err != nil {
		return err
	}
	log.Printf("%s: %d features, %d KB", path, len(fc.Features), len(b)/1024)
	return os.WriteFile(path, b, 0o644)
}

func round(v any) any {
	switch t := v.(type) {
	case float64:
		return math.Round(t*1e4) / 1e4
	case []any:
		for i := range t {
			t[i] = round(t[i])
		}
	}
	return v
}

func get(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}
