package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS area(
	code TEXT PRIMARY KEY,          -- 5-digit 全国地方公共団体コード (no check digit)
	pref_code INT NOT NULL,
	name_ja TEXT NOT NULL,
	population INT,
	enabled INT NOT NULL DEFAULT 0  -- 1 = collect store prices here
);
CREATE TABLE IF NOT EXISTS store(
	id TEXT PRIMARY KEY,            -- "<source>:<source store id>"
	source TEXT NOT NULL,
	chain TEXT NOT NULL,
	name TEXT NOT NULL,
	area_code TEXT NOT NULL,
	url TEXT NOT NULL,              -- page holding the cabbage price
	active INT NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS store_price(
	store_id TEXT NOT NULL,
	date TEXT NOT NULL,
	product TEXT NOT NULL,
	unit TEXT NOT NULL,             -- whole | half | quarter
	yen_ex_tax INT NOT NULL,
	yen_in_tax INT NOT NULL,
	on_sale INT NOT NULL DEFAULT 0,
	PRIMARY KEY(store_id, date, product)
);
CREATE TABLE IF NOT EXISTS retail(
	city_code TEXT NOT NULL,        -- 5-digit municipality code of the surveyed city
	ym TEXT NOT NULL,               -- YYYY-MM
	yen_per_kg INT NOT NULL,
	PRIMARY KEY(city_code, ym)
);
CREATE TABLE IF NOT EXISTS wholesale(
	date TEXT NOT NULL,
	market TEXT NOT NULL,
	method TEXT NOT NULL,           -- せり・入札 | 相対 | ...
	origin TEXT NOT NULL,           -- prefecture name as printed, or 各地
	unit_kg INT NOT NULL,
	hi INT, mid INT, lo INT,        -- yen per unit, tax included; NULL when not traded
	PRIMARY KEY(date, market, method, origin)
);
CREATE TABLE IF NOT EXISTS fetch_log(
	source TEXT PRIMARY KEY,
	last_ok TEXT,
	last_err TEXT
);
`

type DB struct{ *sql.DB }

func openDB(path string) (*DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &DB{db}, nil
}

func (db *DB) logFetch(source string, err error) {
	now := time.Now().Format(time.RFC3339)
	if err == nil {
		db.Exec(`INSERT INTO fetch_log(source, last_ok) VALUES(?, ?)
			ON CONFLICT(source) DO UPDATE SET last_ok = excluded.last_ok`, source, now)
		return
	}
	db.Exec(`INSERT INTO fetch_log(source, last_err) VALUES(?, ?)
		ON CONFLICT(source) DO UPDATE SET last_err = excluded.last_err`, source, now+" "+err.Error())
}

// hasAny reports whether a table has rows; table is a fixed name from our code.
func (db *DB) hasAny(table string) bool {
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM (SELECT 1 FROM ` + table + ` LIMIT 1)`).Scan(&n)
	return n > 0
}

type RetailPrice struct {
	CityCode string
	YM       string
	Yen      int
}

func (db *DB) upsertRetail(ps []RetailPrice) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range ps {
		if _, err := tx.Exec(`INSERT INTO retail(city_code, ym, yen_per_kg) VALUES(?, ?, ?)
			ON CONFLICT(city_code, ym) DO UPDATE SET yen_per_kg = excluded.yen_per_kg`, p.CityCode, p.YM, p.Yen); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) hasRetail(ym string) bool {
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM retail WHERE ym = ?`, ym).Scan(&n)
	return n > 0
}

type WholesalePrice struct {
	Date, Market, Method, Origin string
	UnitKg                       int
	Hi, Mid, Lo                  sql.NullInt64
}

func (db *DB) upsertWholesale(ps []WholesalePrice) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range ps {
		if _, err := tx.Exec(`INSERT INTO wholesale(date, market, method, origin, unit_kg, hi, mid, lo) VALUES(?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(date, market, method, origin) DO UPDATE SET unit_kg = excluded.unit_kg, hi = excluded.hi, mid = excluded.mid, lo = excluded.lo`,
			p.Date, p.Market, p.Method, p.Origin, p.UnitKg, p.Hi, p.Mid, p.Lo); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) hasWholesale(date, market string) bool {
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM wholesale WHERE date = ? AND market = ?`, date, market).Scan(&n)
	return n > 0
}

type StorePrice struct {
	StoreID, Date, Product, Unit string
	ExTax, InTax                 int
	OnSale                       bool
}

func (db *DB) upsertStorePrice(p StorePrice) error {
	_, err := db.Exec(`INSERT INTO store_price(store_id, date, product, unit, yen_ex_tax, yen_in_tax, on_sale) VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(store_id, date, product) DO UPDATE SET unit = excluded.unit, yen_ex_tax = excluded.yen_ex_tax,
			yen_in_tax = excluded.yen_in_tax, on_sale = excluded.on_sale`,
		p.StoreID, p.Date, p.Product, p.Unit, p.ExTax, p.InTax, p.OnSale)
	return err
}
