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
