package main

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"

	"github.com/zamonia500/cabbage-coin/internal/xlsx"
)

// e-Stat 小売物価統計調査（動向編）, table 1 主要品目の都市別小売価格.
// Item 1401 is キャベツ, priced per 1kg, for prefectural capitals and cities of 150k+.
const (
	estatList     = "https://www.e-stat.go.jp/stat-search/files?page=1&layout=datalist&cycle=1&toukei=00200571&tstat=000000680001&tclass1val=0&year=%d&month=%s&result_back=1"
	estatDownload = "https://www.e-stat.go.jp/stat-search/file-download?statInfId=%s&fileKind=0"
	cabbageCode   = "1401"
)

// estatMonth encodes a month the way e-Stat's file search does:
// half(1|2) quarter(1-4) quarterStart quarterEnd month, e.g. Aug → 23070908.
func estatMonth(m int) string {
	q := (m-1)/3 + 1
	h := 1
	if q > 2 {
		h = 2
	}
	return fmt.Sprintf("%d%d%02d%02d%02d", h, q, q*3-2, q*3, m)
}

var estatFileLink = regexp.MustCompile(`stat_infid=(\d{12})"[^>]*>\s*「1001 `)

// parseEstatList finds the file holding items 1001-2183 of table 1 for the month.
func parseEstatList(page []byte, y, m int) (string, error) {
	title := fmt.Sprintf("主要品目の都市別小売価格【%d年%d月】", y, m)
	i := bytes.Index(page, []byte(title))
	if i < 0 {
		return "", errNotFound
	}
	match := estatFileLink.FindSubmatch(page[i:])
	if match == nil {
		return "", fmt.Errorf("e-Stat list %d-%02d: table found but no 1001～ file link", y, m)
	}
	return string(match[1]), nil
}

var cityCode = regexp.MustCompile(`^\d{5}$`)

// parseEstatCity reads the cabbage row of table 1. Cities without a price
// ("-", "...") are skipped.
func parseEstatCity(data []byte, ym string) ([]RetailPrice, error) {
	rows, err := xlsx.Rows(data)
	if err != nil {
		return nil, err
	}
	var codes map[string]string // column → city code
	for _, r := range rows {
		if codes == nil {
			for _, v := range r {
				if v == "地域コード" {
					codes = map[string]string{}
					for col, c := range r {
						if cityCode.MatchString(c) {
							codes[col] = c
						}
					}
				}
			}
			continue
		}
		isCabbage := false
		for _, v := range r {
			if strings.TrimSpace(v) == cabbageCode {
				isCabbage = true
			}
		}
		if !isCabbage {
			continue
		}
		var out []RetailPrice
		for col, code := range codes {
			yen, err := strconv.Atoi(strings.TrimSpace(r[col]))
			if err != nil {
				continue
			}
			out = append(out, RetailPrice{CityCode: code, YM: ym, Yen: yen})
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("e-Stat %s: cabbage row has no prices", ym)
		}
		return out, nil
	}
	if codes == nil {
		return nil, errors.New("e-Stat: no 地域コード header row")
	}
	return nil, errors.New("e-Stat: no cabbage (1401) row")
}

// collectRetail loads every month in the window that is not in the DB yet.
// Months not published yet are skipped quietly.
func collectRetail(db *DB, f *fetcher, months int) error {
	now := time.Now()
	var errs []error
	for i := 1; i <= months; i++ {
		t := time.Date(now.Year(), now.Month()-time.Month(i), 1, 0, 0, 0, 0, time.Local)
		ym := t.Format("2006-01")
		if db.hasRetail(ym) {
			continue
		}
		page, err := f.get(fmt.Sprintf(estatList, t.Year()*10, estatMonth(int(t.Month()))))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		id, err := parseEstatList(page, t.Year(), int(t.Month()))
		if errors.Is(err, errNotFound) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		data, err := f.get(fmt.Sprintf(estatDownload, id))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		ps, err := parseEstatCity(data, ym)
		if err == nil {
			err = db.upsertRetail(ps)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ym, err))
			continue
		}
		log.Printf("retail %s: %d cities", ym, len(ps))
	}
	return errors.Join(errs...)
}

// Tokyo central wholesale market daily report, Ota market (the largest
// vegetable market), produce. Prices are per unit (10kg for cabbage), tax included.
const (
	tokyoOta    = "https://www.shijou-nippo.metro.tokyo.lg.jp/SN/%s/%s/Sei/Sei_K2.csv"
	marketOta   = "大田"
	cabbageName = "キャベツ"
)

func parseTokyoCSV(data []byte, date, market string) ([]WholesalePrice, error) {
	r := csv.NewReader(transform.NewReader(bytes.NewReader(data), japanese.ShiftJIS.NewDecoder()))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	var out []WholesalePrice
	var item, method string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		// 品名,卸売数量計,販売方法,卸売数量,品種,産地,単位,高値,中値,安値
		if len(rec) < 10 || rec[0] == "品名" {
			continue
		}
		if rec[0] != "" {
			item = rec[0]
		}
		if rec[2] != "" {
			method = rec[2]
		}
		if item != cabbageName {
			continue
		}
		unit, err := strconv.Atoi(rec[6])
		if err != nil {
			return nil, fmt.Errorf("tokyo %s: bad unit %q", date, rec[6])
		}
		out = append(out, WholesalePrice{
			Date: date, Market: market, Method: method, Origin: rec[5], UnitKg: unit,
			Hi: yen(rec[7]), Mid: yen(rec[8]), Lo: yen(rec[9]),
		})
	}
	return out, nil
}

// yen parses a price cell; "－" means no trade.
func yen(s string) sql.NullInt64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return sql.NullInt64{Int64: n, Valid: err == nil}
}

// collectWholesale loads the last `days` days that are not in the DB yet.
// Days without trading return 404 and are skipped.
func collectWholesale(db *DB, f *fetcher, days int) error {
	var errs []error
	now := time.Now()
	for i := 0; i < days; i++ {
		t := now.AddDate(0, 0, -i)
		date := t.Format("2006-01-02")
		if db.hasWholesale(date, marketOta) {
			continue
		}
		data, err := f.get(fmt.Sprintf(tokyoOta, t.Format("200601"), t.Format("20060102")))
		if errors.Is(err, errNotFound) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		ps, err := parseTokyoCSV(data, date, marketOta)
		if err == nil {
			err = db.upsertWholesale(ps)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", date, err))
			continue
		}
		log.Printf("wholesale %s %s: %d rows", marketOta, date, len(ps))
	}
	return errors.Join(errs...)
}
