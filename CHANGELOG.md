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
- Hapag-Lloyd container tracking (hapag-lloyd.com) — 10 real MBLs, 10/10 success, 4.7x parallel speedup with 5 minnows
- Cookie-based auth test site — login persistence across navigations and lease cycles
