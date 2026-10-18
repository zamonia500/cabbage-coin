# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Status
No code yet. `docs/plan.md` (Korean) is the design spec: read it before implementing anything. The structure and commands below are the plan; update this file once they exist.

## What this is
A site showing Japanese cabbage (キャベツ) prices by prefecture on a map, served at `cabbage.zamonia500.com` from an Oracle Cloud Ampere A1 (linux/arm64) instance. Hard constraints: **one static Go binary**, as light as possible.

## Architecture (planned)
- Flat `package main`: `main.go` (flags, HTTP, starts collector), `collect.go` (official sources), `stores.go` (supermarket store crawling), `store.go` (SQLite), `web.go` (handlers, i18n, SVG charts).
- **Collector runs in-process** (goroutine + `time.Ticker`); there is no cron. Writes are upserts so re-runs are safe; `fetch_log` records last success/error per source.
- **Data sources**:
  - Retail, monthly: 小売物価統計調査 table 1 (主要品目の都市別小売価格) xlsx from e-Stat's file download, no appId. Item `1401` キャベツ per 1kg, 81 cities (prefectural capitals + 150k+ cities) keyed by 5-digit municipality code. The file is found by parsing the month's e-Stat list page (`parseEstatList`); published about two months late.
  - Wholesale, daily: Tokyo central wholesale market daily report, Ota market CSV (`Sei_K2.csv`, Shift-JIS): price per 10kg by sale method and origin. Non-trading days return 404.
  - Supermarket stores, daily: Aeon Net Super product pages (`product:price:amount` meta, tax excluded; we store ×1.08 too). robots.txt allows product and store pages but not search/category listings, so stores cannot be discovered automatically: each one is registered from a product URL listed in `data/stores.txt` (embedded). Registration reads the store page's 店舗情報 address and maps it to a municipality (`areaFromAddress`). Tokubai: pending the user's ToS check.
  - Never guess `statsDataId`, item codes, CSV URLs or columns. Look them up live, then save real responses in `testdata/` for parser tests.
- **Two map levels**: prefecture (JIS 1–47, official data, whole country) and municipality (5-digit 全国地方公共団体コード, store data). Prefecture code joins retail (consumer city) and wholesale (origin prefecture).
- **Collection scope is data, not code**: `area` is seeded once from the embedded `data/areas.csv` (MIC resident-register table: code, name, population) and `area.enabled` selects municipalities. It starts with 194 (Tokyo's 23 wards + 171 designated-city wards, after Hamamatsu's 2024 reorganization) and widens by population and store presence after reviewing results.
- **Store crawling rules**: 2–3 stores per municipality, once a day, at least 5s between requests, honest UA `cabbage-coin/1.0 (+https://cabbage.zamonia500.com)`, check robots.txt at runtime (`robots.go`, RFC 9309 longest-match) and skip URLs it disallows, no retry on failure. Never add IP rotation, browser-UA spoofing or private/undocumented endpoints. Compare whole-head (1玉) tax-included prices; a municipality's value is the median of that day's stores.
- **Claude does not access tokubai.co.jp** (its robots.txt blocks Anthropic agents by name). Its parser is built from samples the user saves into `testdata/`.
- **Storage**: SQLite via `modernc.org/sqlite` (pure Go). Do not add cgo; it breaks the arm64 cross-build.
- **Frontend**: `html/template` and static files via `go:embed` from `web/`. No JS framework and no bundler. The map is MapLibre GL JS (vendored, pinned version) over an OpenFreeMap basemap, with embedded GeoJSON colored through `feature-state` from `/api/map`, the only JSON endpoint. Prefecture boundaries load first; municipality boundaries (国土数値情報 N03 via smartnews-smri/japan-topography, simplified, 2021) are split into one file per prefecture (`web/geo/muni/NN.json`) and loaded on zoom. Hamamatsu's post-2024 wards have no shape. If the basemap style fails to load, fall back to an empty style so the prefecture fills still render. Charts are server-generated SVG.
- **Attribution is mandatory** in the footer: e-Stat, 東京都中央卸売市場, each store source, 国土数値情報, OpenMapTiles/OpenStreetMap.
- **i18n**: URL prefix `/ko/` and `/ja/`; `/` redirects by `Accept-Language`. Every user-facing string and prefecture name needs both `ko` and `ja`.
- **TLS is not in the binary.** The binary serves plain HTTP on `127.0.0.1:8080`, and Caddy in front handles HTTPS, Let's Encrypt and compression (`deploy/Caddyfile`).
- Go dependencies: `modernc.org/sqlite` and `golang.org/x/text` (Shift-JIS decoding). Adding another needs a strong reason. `internal/xlsx` is our own stdlib-only xlsx reader.
- `go.mod` pins `toolchain go1.23.4`: the locally installed Go 1.22.0 links test binaries without `LC_UUID`, and this macOS refuses to run them. Keep `GOTOOLCHAIN=auto` (the default).

## Commands
```sh
go run . -data ./tmp -once          # run every collector once against live sources, then exit
go run . -data ./tmp                # local, http://localhost:8080/ko/
go test ./...                       # parser tests against testdata/
go test -run TestParseEstat ./...   # single test
go run ./tools/gendata              # regenerate data/areas.csv and web/geo/ (committed; rerun only to refresh)
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o cabbage .   # server build
```

## Deploy gotchas (Oracle Ampere)
- Open 80/443 in the OCI Security List **and** in the instance's iptables: Oracle Ubuntu images block them by default. Persist with `netfilter-persistent save`.
- A systemd unit (`deploy/cabbage.service`) passes `-addr 127.0.0.1:8080 -data /var/lib/cabbage` (the DB lives there). Caddy runs as its own systemd service with `deploy/Caddyfile`.

## Git workflow
- Never commit directly to the default branch (`main`). Create a branch named for the task's purpose (e.g. `feat/estat-collector`, `docs/plan`), commit there, push it, then merge.
