# 양배추 코인 (cabbage-coin) — 기획

## Context
일본 양배추(キャベツ) 가격을 지역별로 추적해 보여주는 웹사이트.
배포 대상: Oracle Cloud Ampere A1 (linux/arm64), 도메인 `cabbage.zamonia500.com`.
요구: 최대한 가볍게, **단일 바이너리**. UI는 한국어/일본어 토글.
코드는 아직 없음 — 이 문서가 구현 기준.

## 지역 단위 — 두 층
1. **현(都道府県, 47)**: 공식 통계로 전국을 빠짐없이 칠하는 기본 지도.
2. **시구정촌(市区町村)**: 마트 점포 가격으로 칠하는 확대 지도. 키는 전국지방공공단체코드(5자리).
   - **초기 수집 범위: 194개** = 도쿄 23구 + 정령지정도시 20곳의 행정구 171 (2024 하마마쓰 구 개편 후, 총무성 주민기본대장 2026 표로 확인).
   - **확장 원칙**: 인구수와 마트 존재 여부를 보고 수집 지역을 넓힌다 (예: 인구 상위 시 → 나머지 시 → 정·촌). 어디로 넓힐지는 초기 수집 결과(점포가 잡히는 비율, 가격 편차)를 보고 정한다.
   - 확장은 코드가 아니라 **데이터**로 한다: `area.enabled` 플래그만 바꾸면 수집 대상이 바뀐다.

## 데이터 소스
| 구분 | 소스 | 주기 | 지역 단위 | 수집 방법 | 상태 |
|---|---|---|---|---|---|
| 소매 (공식) | 小売物価統計調査 第1表 — キャベツ(1401) 1kg | 월간 (약 2개월 뒤) | 81개 시 (현청 소재지 + 인구 15만 이상) | e-Stat 파일 다운로드 xlsx (appId 불필요) | 구현 |
| 도매 (공식) | 東京都中央卸売市場日報 — 大田市場 青果 | 일간 | 산지·판매방법별 10kg 가격 | 일별 CSV (Shift-JIS) | 구현 |
| 마트 점포 | 이온 넷슈퍼 상품 페이지 | 일간 | 점포 → 시구정촌 | 공개 HTML의 `product:price:amount` meta, 점포는 `data/stores.txt`의 상품 URL로 등록 | 구현 |
| 마트 점포 | 토쿠바이 공개 점포 페이지 | 일간 | 점포 → 시구정촌 | 공개 HTML | 이용약관 확인 후 결정 |

- 공식 소스 형식은 실제 응답으로 확정했고 샘플은 `testdata/`에 있음.
- 공식 소스 추가 후보: 농림수산성 食品価格動向調査(주간, 마트 470곳 **전국 평균만** 공개 — 확인함), ALIC 베지탄(14개 도시 19개 시장 도매, 산지별).
- 이온 넷슈퍼 확인 사항: 로그인 없이 가격 노출(확인함, 예: 398엔/세포함 429.84엔). robots.txt는 `/netsuper/*/*/`(슬래시로 끝나는 목록 경로)만 금지 → 상품 `.html` 페이지 허용. 남은 것: 점포 간 상품 ID 규칙, 점포 코드 목록·주소(→ 시구정촌 매핑), 이용약관.
- 토쿠바이: robots.txt는 Anthropic 계열 봇만 전체 차단, `User-agent: *`는 `/2026/random_product`만 금지. 이용약관은 사용자가 직접 확인. Claude는 이 사이트에 직접 접속하지 않으며, 파서는 사용자가 저장한 샘플(`testdata/`)로 만든다. 비공개 JSON 엔드포인트는 쓰지 않는다.

### 점포 수집 규칙 (모든 마트 소스 공통)
- 시구정촌당 점포 **2~3곳**, 하루 1회. 요청 간격 5초 이상 → 194 단위 × 3 ≈ 580회/일, 약 50분.
- 정직한 User-Agent: `cabbage-coin/1.0 (+https://cabbage.zamonia500.com)`. 수집 전 robots.txt를 읽어 이 UA가 허용되는지 확인, 금지면 그 소스는 건너뛰고 `fetch_log`에 기록.
- 실패 시 그날은 재시도하지 않고 다음 날로. IP 순환·브라우저 UA 위장 등 차단 회피 장치는 넣지 않는다.
- 상품 비교 기준: **1개(1玉) 가격, 세포함**. 1/2·1/4 컷은 따로 저장하되 지도 값에는 쓰지 않음. 특가 여부 플래그 저장.
- 시구정촌 값 = 그날 수집된 점포 가격의 중앙값. 점포 0곳이면 비워 둠(회색).

## 기술 스택 (전부 바이너리 안)
- **Go**, `CGO_ENABLED=0` 크로스 컴파일 → `linux/arm64` 바이너리 하나.
- 저장소: SQLite, `modernc.org/sqlite` (순수 Go). DB 파일 하나(`cabbage.db`), WAL 모드.
- 템플릿/정적 파일: `html/template` + `go:embed`. JS 프레임워크 없음.
- 지도: **MapLibre GL JS** (embed, 버전 고정) + **OpenFreeMap** 배경 타일(키 없음, 무제한, SLA 없음). 3D 지형 없음.
  - 현 경계 GeoJSON(embed, 수백 KB 이하) — 첫 화면.
  - 시구정촌 경계: 국토수치정보 N03(행정구역) 단순화본을 **현별 파일로 쪼개 embed**, 현을 확대할 때 그 현 파일만 로드. 원본 라이선스·출처 표기 확인할 것.
  - 배경 스타일 로드 실패 시 빈 배경 스타일로 폴백 → 색칠은 항상 보임.
  - 출처 표기 필수: OpenMapTiles · OpenStreetMap, 국토수치정보.
- 데이터 전달: `/api/map?level=pref|muni&metric=…&pref=NN` JSON(코드→값·단계·차이)을 받아 `feature-state`로 색 적용. 왼쪽 패널은 `/api/panel`(HTML 조각)로 교체.
- 차트: 상세 패널의 추이 차트는 서버에서 SVG `<polyline>` 생성.
- 수집기: 같은 프로세스 안의 goroutine + `time.Ticker` (cron 없음). 공식 소스는 매일 확인, 점포는 하루 1회 순차 수집. `upsert`라 재실행 안전.
- TLS: 바이너리는 `127.0.0.1:8080`에서 HTTP만. 앞단 **Caddy**가 HTTPS·인증서·압축 담당.
- i18n: Go 맵 두 개(`ko`, `ja`), URL 접두사 `/ko/…`, `/ja/…`. `/`는 `Accept-Language`로 리다이렉트. 현·시구정촌 이름 ja/ko 둘 다(시구정촌 ko 이름은 없으면 ja 표기 + 가나 읽기).

Go 의존성은 `modernc.org/sqlite` 하나뿐.

## 화면 (레퍼런스: sun.gomdori.app 의 레이아웃)
한 페이지 `/{lang}/` — 전체 화면 지도 위에 떠 있는 패널들. positron 배경 지도 + 파랑(쌈)↔회색↔빨강(비쌈) 5단계 발산형 색 (초록↔빨강은 색각이상자가 구분 못 해 교체).
- **확대 단계에 따라 층 전환**: 전국 줌에서는 현(공식 월간 소매가), 현을 확대하면 그 현의 시구정촌(마트 일간 가격). 수집 안 된 시구정촌은 회색 + "수집 예정".
- **상단 중앙 검색창**: 현·시구정촌 이름(ko·ja) → 이동.
- **왼쪽 패널 (기본)**: 헤드라인 질문("오늘 양배추, 어디가 제일 쌀까?"), 전국 평균·전월 대비, 비싼/싼 순 순위(현 또는 시구정촌).
- **왼쪽 패널 (현 선택)**: 큰 숫자(이번 달 ¥/kg, 전월·전년 대비), 소매가 월별 추이, 산지 도매 추이(있으면), 그 현의 시구정촌 순위. URL `/{lang}/pref/{code}`.
- **왼쪽 패널 (시구정촌 선택)**: 오늘 중앙값(1개 기준), 점포별 가격 목록(체인·점포명·가격·특가 여부·수집 시각), 최근 30일 추이. URL `/{lang}/muni/{code}`.
- **오른쪽 패널**: "지도 색 기준" 전환 — 소매가 / 전월 대비 / 마트 점포가 — 범례. (도매 CSV에 산지별 수량이 없어 '산지 출하량' 색칠은 뺐고, 도매는 패널 차트로만 보여줌.)
- **마우스 오버 카드**: 이름, 값, 전일(전월) 대비, 점포 수.
- **하단 푸터**: 데이터 출처(e-Stat, 도쿄도 중앙도매시장, 각 마트 소스, 국토수치정보, OpenMapTiles/OSM)·최종 수집 시각.
- 언어 토글(ko/ja)은 오른쪽 상단.

레퍼런스에서 뺀 것: 3D 지형·시간대 시뮬레이션, CSV 다운로드, 로그인·알림, 사용자 제보.

## 데이터 모델
```sql
area(code TEXT PRIMARY KEY,          -- 전국지방공공단체코드 5자리
     pref_code INT, name_ja TEXT, name_ko TEXT,
     population INT, enabled INT);   -- enabled=1 이 수집 대상 (확장은 여기만 바꿈)
store(id TEXT PRIMARY KEY,           -- "<source>:<source_store_id>"
      source TEXT, chain TEXT, name TEXT, area_code TEXT, lat REAL, lon REAL, active INT);
store_price(store_id TEXT, date TEXT, product TEXT, unit TEXT,   -- unit: whole|half|quarter
            yen_ex_tax INT, yen_in_tax INT, on_sale INT,
            PRIMARY KEY(store_id, date, product));
retail(city_code TEXT, pref_code INT, ym TEXT, yen_per_kg INT, PRIMARY KEY(city_code, ym));
wholesale(date TEXT, market TEXT, origin_pref INT, qty_kg INT, hi INT, mid INT, lo INT,
          PRIMARY KEY(date, market, origin_pref));
fetch_log(source TEXT PRIMARY KEY, last_ok TEXT, last_err TEXT);
```
`area`는 총무성 주민기본대장 시구정촌별 인구표(코드·이름·인구)로 초기 적재하고 194개만 `enabled=1`.

## 레이아웃
```
main.go          // 플래그, HTTP 서버, 수집 goroutine 시작
collect.go       // 공식 소스(e-Stat, 도매) 수집 + 파싱
stores.go        // 마트 점포 수집: 소스별 파서, robots 확인, 순차·간격 제어
store.go         // SQLite 스키마·쿼리
web.go           // 핸들러, i18n, SVG 차트
web/             // embed: *.tmpl, style.css, app.js, maplibre-gl.{js,css}, geo/pref.geojson, geo/muni/{pref}.geojson
testdata/        // 실제 응답 샘플 (토쿠바이 샘플은 사용자가 저장)
deploy/cabbage.service, deploy/Caddyfile
```
파일 몇 개짜리 flat `package main`. 커지면 그때 나눔.

## 배포 (Oracle Ampere)
1. DNS: `cabbage.zamonia500.com` A 레코드 → 인스턴스 공인 IP.
2. OCI VCN Security List에 80/443 인바운드 허용. **Oracle Ubuntu 이미지는 iptables가 기본 차단** → `iptables -I INPUT -p tcp --dport 80/443 -j ACCEPT` + `netfilter-persistent save`.
3. 빌드: `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o cabbage .`
4. `scp` → `/opt/cabbage/`.
5. systemd 유닛: ``-addr 127.0.0.1:8080`, `-data /var/lib/cabbage`(DB), `Restart=always`, 전용 비root 사용자.
6. Caddy (apt 설치, 자체 systemd): `/etc/caddy/Caddyfile`에
   ```
   cabbage.zamonia500.com {
       encode zstd gzip
       reverse_proxy 127.0.0.1:8080
   }
   ```
CI/CD, Docker 없음 — `make deploy` 한 줄(scp + systemctl restart)이면 충분.

## 구현 순서
1. 공식 소스: 형식 확정, 샘플 `testdata/` 저장 → 파서·테스트 → 수집기. (완료)
2. `area` 적재: 총무성 주민기본대장 인구표, 194개 enabled.
3. 마트 소스 조사 마무리: 이온(상품 ID 규칙·점포 목록·약관), 토쿠바이(사용자 약관 확인·샘플 저장). 통과한 소스부터 `stores.go`.
4. 경계 GeoJSON 준비(현 + 시구정촌 현별 분할, 단순화), MapLibre 페이지·패널, SVG 차트, i18n.
5. arm64 빌드, 서버 배포, Caddy 설정·인증서 확인.
6. 2~4주 수집 후 결과 검토 → 확장 지역 결정(`area.enabled` 갱신).

## 검증
- 구현 시: `go test ./...` (testdata 기반 파서 테스트), `go run .` 후 `localhost:8080/ko/`, `/ja/` 확인.
- 배포 후: `curl -I https://cabbage.zamonia500.com` 200 + 유효 인증서, `journalctl -u cabbage`에서 수집 성공 로그.
- 점포 수집: 하루 요청 수·소요 시간이 위 예산(≈600회, 1시간 이내) 안인지 `fetch_log`로 확인. enabled 194개 중 점포가 1곳 이상 잡힌 비율 확인.
- `ls -lh cabbage` 바이너리 크기 (목표: 시구정촌 GeoJSON 포함 ~25MB 이하).
