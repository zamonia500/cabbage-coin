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
  - Retail, monthly: e-Stat API v3 小売物価統計調査, 47 prefectural-capital cities. Needs `ESTAT_APP_ID`.
  - Wholesale, daily: 東京都中央卸売市場 statistics CSV, by origin prefecture.
  - Supermarket stores, daily: per-store prices (Aeon Net Super product pages; Tokubai pending ToS check), mapped to municipalities.
  - Never guess `statsDataId`, item codes, CSV URLs or columns. Look them up live, then save real responses in `testdata/` for parser tests.
- **Two map levels**: prefecture (JIS 1–47, official data, whole country) and municipality (5-digit 全国地方公共団体コード, store data). Prefecture code joins retail (consumer city) and wholesale (origin prefecture).
- **Collection scope is data, not code**: `area.enabled` selects municipalities. It starts with 198 (Tokyo's 23 wards + 175 designated-city wards) and widens by population and store presence after reviewing results.
- **Store crawling rules**: 2–3 stores per municipality, once a day, at least 5s between requests, honest UA `cabbage-coin/1.0 (+https://cabbage.zamonia500.com)`, check robots.txt at runtime and skip the source if disallowed, no retry on failure. Never add IP rotation, browser-UA spoofing or private/undocumented endpoints. Compare whole-head (1玉) tax-included prices; a municipality's value is the median of that day's stores.
- **Claude does not access tokubai.co.jp** (its robots.txt blocks Anthropic agents by name). Its parser is built from samples the user saves into `testdata/`.
- **Storage**: SQLite via `modernc.org/sqlite` (pure Go). Do not add cgo; it breaks the arm64 cross-build.
- **Frontend**: `html/template` and static files via `go:embed` from `web/`. No JS framework and no bundler. The map is MapLibre GL JS (vendored, pinned version) over an OpenFreeMap basemap, with embedded GeoJSON colored through `feature-state` from `/api/map`, the only JSON endpoint. Prefecture boundaries load first; municipality boundaries (国土数値情報 N03, simplified) are split into one file per prefecture and loaded on zoom. If the basemap style fails to load, fall back to an empty style so the prefecture fills still render. Charts are server-generated SVG.
- **Attribution is mandatory** in the footer: e-Stat, 東京都中央卸売市場, each store source, 国土数値情報, OpenMapTiles/OpenStreetMap.
- **i18n**: URL prefix `/ko/` and `/ja/`; `/` redirects by `Accept-Language`. Every user-facing string and prefecture name needs both `ko` and `ja`.
- **TLS is not in the binary.** The binary serves plain HTTP on `127.0.0.1:8080`, and Caddy in front handles HTTPS, Let's Encrypt and compression (`deploy/Caddyfile`).
- The only Go dependency is `modernc.org/sqlite`. Adding another needs a strong reason.

## Commands (planned)
```sh
go run .                            # local, http://localhost:8080/ko/
go test ./...                       # parser tests against testdata/
go test -run TestParseEstat ./...   # single test
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o cabbage .   # server build
```

## Deploy gotchas (Oracle Ampere)
- Open 80/443 in the OCI Security List **and** in the instance's iptables: Oracle Ubuntu images block them by default. Persist with `netfilter-persistent save`.
- A systemd unit (`deploy/cabbage.service`) sets `ESTAT_APP_ID` and passes `-addr 127.0.0.1:8080 -data /var/lib/cabbage` (the DB lives there). Caddy runs as its own systemd service with `deploy/Caddyfile`.

## Git workflow
- Never commit directly to the default branch (`main`). Create a branch named for the task's purpose (e.g. `feat/estat-collector`, `docs/plan`), commit there, push it, then merge.
