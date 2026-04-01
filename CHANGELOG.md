## v0.7.0 — Dock Lights (2026-03-31)

Reliability hardening. Tests, error handling, and actionable responses.

### Testing
- **Remora 93% coverage** — all 8 detection systems tested (17 tests)
- **Agent 19.4% coverage** — tls-client navigate, redirect, 404, proxy, cookie injection tests
- **Controller** — tides status/boost, metrics, dashboard, remora integration tests
- **83 total tests** across 5 packages, all with race detection

### Improvements
- **`block_system` + `block_suggest` in response** — callers see exactly which bot management system blocked them and what to do about it
- **Smart auto-retry** — Python client uses `block_suggest` instead of hint-matching heuristic
- **Python client error handling** — `ConnectionError`, `Timeout`, non-JSON all raise clean `ShoalError` with descriptive messages
- **`is_available()` / `IsAvailable()` / `WaitAvailable()`** — controller reachability check in both clients
- **Remora stats on dashboard** — Activity card shows good/blocked/queued counts
- **Redfish on Fleet card** — dashboard shows all three agent tiers

### Refactoring
- Extracted `cookies.go` from `server.go`

---

## v0.6.0 — Shrimp Run (2026-03-30)

New modules, new rhythms. The shoal moves with the tides.

### New Packages
- **tides** (`internal/tides`) — sinusoidal scraping cadence. Interval follows a cosine wave keyed to market hours (fast at peak, slow overnight). Adaptive boosts for volatility, news velocity, and sentiment shifts. 5 tests.

### New Features
- **GET /tides/status** — current interval, phase (high/rising/falling/low), active boosts
- **POST /tides/boost** — set named boost factors from external systems
- **Tides on dashboard** — live card showing interval, phase, and boosts
- **Remora Prometheus metrics** — `shoal_remora_blocked_total{system,type}` and `shoal_remora_quality_total{quality}`
- **Three new remora detectors** — Imperva/Incapsula, AWS WAF, Shape Security (F5). Total: 8 systems.
- **Auto-retry in Python client** — blocked responses auto-escalate light→medium→heavy, but only for real bot detection (not rate limits)
- **Tides in Go + Python clients** — `client.Tides()`, `client.SetTidesBoost()`, `s.tides()`, `s.set_boost()`
- **Redfish count on dashboard** — Fleet card shows all three tiers (groupers/redfish/minnows)

### Refactoring
- Extracted `cookies.go` from `server.go` (533→431 lines)
- Fixed auto-retry escalating API endpoints to Lightpanda unnecessarily

---

## v0.5.0 — Neap Tide (2026-03-30)

Quality, resilience, and structure. The shoal knows when it's stuck.

### New Packages
- **remora** (`internal/remora`) — block detection module. Scans responses for Cloudflare, Akamai, DataDome, PerimeterX, Kasada, paywalls, rate limits, geo-blocks, JS shells, and app errors. Returns system, type, confidence, and suggested action (retry_heavy, retry_proxy, wait, skip). 8 tests.

### New Features
- **Response metadata** — `title`, `content_size`, `redirected`, `quality`, `quality_hints` on every response. Callers know what they got without parsing HTML.
- **Text output** — `output_format: "text"` strips HTML via JS, returns clean `innerText`. No more fragile `<p>` tag parsing.
- **Wait queue** — `POST /fetch` now queues instead of rejecting when pool is full. Requests wait for an agent instead of 503. `POST /lease` stays non-blocking.
- **Medium agent class** — Lightpanda classified as `medium` (headless, no display) between `heavy` (Chrome+xvfb) and `light` (tls-client).
- **Formations** — `make school-minnow`, `school-lp`, `school-cf`, `school-mixed` with `COUNT=N`.
- **Action retry** — fill/click/submit retry 3x with 500ms backoff for DOM timing resilience.
- **Network idle wait** — after navigation, waits for fetch/XHR activity to settle before actions.
- **Proxy pool refresh** — `--proxy-refresh` interval re-reads proxy list from file/HTTP source.

### Bug Fixes
- Fill action dispatches input/change events for Angular/React/SSO forms (#16)
- Dashboard fish ordering stable (sorted by ID, no more jumping)
- Dashboard domain overflow scrollable
- Tab cleanup uses `target.CloseTarget` (more reliable than `page.Close`)
- Default CDP timeout bumped from 30s to 60s
- `wait_for` action with configurable timeout for JS-rendered elements

### Refactoring
- Extracted `actions.go` from `cdp.go` (534→367 lines)
- Removed `quality.go` (replaced by remora)
- Long-lived multi-grouper stress test (2,427 req, 100% success)

---

## v0.4.0 — Low Tide (2026-03-30)

Client libraries and container-readiness. Both Go and Python consumers can call the shoal.

### New Features
- **Python client** (`clients/python/shoal.py`) — `Shoal.fetch()` one-liner, `session()` context manager, XHR capture, `ShoalResponse` with `cookies_dict()` and `json()` helpers
- **`POST /fetch`** — one-shot endpoint, no lease management needed. Auto lease→navigate→release
- **Environment variables** — all config via `SHOAL_*` env vars for container deployments (CLI flags still take precedence)
- **Docker images in CI** — release workflow builds and pushes to ghcr.io (controller, minnow, grouper)
- **Version injection** — `api.Version` set via `-ldflags` at build time, component codenames (controller=xiphosura, agent=mullet)
- **Proxy pool application** — agents now apply controller-assigned proxies at registration via `ProxySetter` interface (PR #12)

### Bug Fixes
- Fixed agent server rejecting empty URL when actions are present (stateful multi-step flows)
- Fixed fish ID collision — increased entropy from 2 to 4 bytes
- Fixed proxy pool: controller assigned proxies but agents never applied them

---

## v0.3.0 — Cast Net (2026-03-29)

Production-readiness release. Client library, proxy pools, XHR capture, and quality hardening.

### New Features
- **Go client library** (`pkg/shoal`) — `client.Fetch(ctx, url, consumer)` one-liner, or low-level Lease/Navigate/Release with full control
- **XHR/Fetch response capture** — `capture_xhr: true` on NavigateRequest intercepts API responses via CDP Network events, with optional URL substring filter (#10)
- **Stateful multi-step navigation** — URL is now optional in NavigateRequest, enabling multi-page login flows (PingFederate, Okta) without losing page state (#7)
- **Proxy pool provider** — controller-level proxy management with round-robin selection, health-based filtering, file/HTTP loading (#8)
- **Proxy assignment on registration** — controller assigns proxies to agents from the pool, returned in RegisterResponse

### Bug Fixes
- **Chrome tab leak** — each navigation leaked a page target; now cleaned up via `cleanupExtraTabs()` after every Navigate (#4)
- **Race condition** — `ensureMinnowCookies` read identity domains without lock
- **StubBackend race** — shared client Timeout mutated during concurrent requests, now uses context timeout
- **Double metric counting** — `cfRenewalsTotal` was incremented twice per renewal
- **Context propagation** — `forwardToAgent` now uses caller's context so client disconnects cancel in-flight requests
- **Agent graceful shutdown** — SIGINT/SIGTERM handler calls `backend.Close()`

### Quality
- **39 Go tests** with `-race` detection in CI
- **staticcheck** clean
- **CLAUDE.md** project context for agents and contributors
- Removed dead code (unused constants, imports)
- Fixed flaky tests (map iteration order)

---

## v0.2.0 — Slack Tide (2026-03-29)

Reliability and observability release. The shoal now heals itself.

### Controller
- **Health checks** — background loop polls agent `/health` every 15s, removes dead fish after 3 missed checks
- **Lease TTLs** — auto-expires abandoned leases after 5m idle (configurable `--lease-ttl`)
- **Pool persistence** — snapshots pool state to JSON every 30s, restores on restart. Fish identities (cookies, domains, CF clearance) survive controller restarts
- **Agent reconnection** — agents re-registering from the same address re-attach their old identity. No cookie/warmth loss on agent restart
- **Graceful shutdown** — SIGINT/SIGTERM saves final snapshot before exit
- **CF auto-renewal** — background renewer scans for expiring `cf_clearance` cookies and proactively re-solves via the grouper before they lapse
- **`POST /renew`** — manual endpoint to force CF clearance renewal for a domain
- **CFURL tracking** — domain state remembers which URL earned the clearance for accurate renewal
- **Cookie handoff retry** — propagation retries 3x with backoff for minnows that aren't ready
- **Lazy cookie catch-up** — on lease, controller checks if minnow has cookies for the domain and copies from a warm agent if not. Eliminates the 1/N silent failure rate
- **Configurable** — `--health-interval`, `--lease-ttl`, `--max-missed-checks`, `--store` flags

### Metrics
- `shoal_lease_expired_total` — leases auto-expired by TTL
- `shoal_agent_removed_total` — agents removed for failed health checks
- `shoal_agent_reconnections_total` — agent identity re-attachments
- `shoal_cf_renewals_total` — successful proactive CF renewals
- `shoal_cf_renewals_failed_total` — failed renewal attempts

### Testing
- **Stress test suite** — burst load (50 req, 50/50 OK), sustained throughput (16 req/s, 0 errors over 30s), agent reconnection, pool persistence, metrics integrity
- **CF renewal test** — forced renewal via `POST /renew`, verified re-solve + cookie handoff

### Bug Fixes
- Fixed cookie propagation race where exactly 1/N minnows never received CF cookies (#2)
- Fixed CF renewal navigating to wrong URL (bare domain vs www subdomain)

---

## v0.1.0 — The First Tide (2026-03-29)

Initial release. Built from scratch in a single session.

### Controller
- **Pool management** — register agents, acquire/release leases, track pool state
- **Warm matching** — prefer agents with existing cookies/CF clearance for a domain (warmth scoring: 3=CF clearance, 2=cookies, 1=visited, 0=cold)
- **Browser identity** — each agent gets a lowcountry fish name (`redfish-a3b2`, `mullet-8d24`) with persistent domain state, cookie tracking, use counts
- **Cookie handoff** — auto-propagates `cf_clearance` from groupers (heavy) to minnows (light)
- **Agent classes** — `heavy` (full browser) vs `light` (HTTP client), filterable in lease requests
- **Prometheus metrics** — pool gauges, request histograms, CF solve/handoff counters, warm match tracking
- **Built-in dashboard** — live web UI at `/dashboard` with pool status, agent table, throughput/error timeseries charts
- **API endpoints** — `/lease`, `/request`, `/release`, `/pool/status`, `/pool/agents`, `/health`, `/metrics`, `/dashboard`

### Agent
- **BrowserBackend interface** — pluggable browser implementations behind a standard contract
- **Chrome backend** — non-headless with xvfb support, stealth injection (`navigator.webdriver` hidden), CF challenge detection + auto-wait, Flatpak Chrome support, CDP proxy auth (`Fetch.continueWithAuth`)
- **Lightpanda backend** — fast headless browser via CDP, persistent tab with cookie retention
- **CDP backend** — generic connector for any CDP-speaking browser
- **tls-client backend** — Chrome 146 TLS fingerprint (JA3/JA4) via bogdanfinn/tls-client, cookie injection support for grouper handoff
- **Stub backend** — plain HTTP fetch for testing
- **Proxy support** — `--proxy-url`, `--proxy-user`, `--proxy-pass` flags for Chrome (via `--proxy-server` + CDP Fetch auth) and tls-client (native proxy)
- **Browser actions** — `fill`, `click`, `submit`, `wait`, `eval` via JS Runtime.evaluate for cross-browser compatibility
- **Persistent tabs** — cookies and login sessions survive across navigations and lease cycles
- **IP detection** — agents detect and report their external IP on startup
- **Auto-registration** — agents register with controller on startup with retry

### Tested Against
- Cloudflare Turnstile on 2captcha.com demo — solved in <1s
- CF-protected carrier tracking site — 10 real tracking pages, 10/10 success, 4.7x parallel speedup with 5 minnows
- Cookie-based auth test site — login persistence across navigations and lease cycles
