# Shoal

Browser orchestration platform. Two Go binaries: `controller` (xiphosura) and `agent` (mullet).

## Architecture

Controller manages a pool of agents. Each agent wraps a browser backend. Three tiers:
- **Grouper** (heavy): Chrome with stealth injection, solves CF Turnstile
- **Redfish** (medium): Lightpanda, headless JS rendering without CF bypass
- **Minnow** (light): tls-client with Chrome 146 TLS fingerprint, bulk HTTP

Controller auto-propagates CF clearance cookies from groupers to minnows.

## Key Patterns

- **Fish identity**: persistent name (e.g. `redfish-a3b2c5d6`) with tracked cookies, domain state, CF clearance. Survives restarts via JSON snapshot + agent reconnection.
- **Warm matching**: leases prefer agents with existing cookies/CF clearance for the requested domain. Warmth: 3=CF clearance, 2=cookies, 1=visited, 0=cold.
- **Lazy cookie catch-up**: on lease, controller pushes cookies to minnows that missed the initial handoff.
- **Persistent tab**: CDP backends keep one tab alive for the agent's lifetime. Cookies survive across navigations. Tab cleanup runs after every navigate.
- **Stateful navigation**: URL is optional in NavigateRequest — omit to stay on current page for multi-step flows (PingFederate, Okta).
- **XHR capture**: `capture_xhr: true` intercepts XHR/Fetch responses via CDP Network events. Use `capture_xhr_filter` for URL substring matching.
- **CF auto-renewal**: background renewer watches `cf_expiry` timestamps and re-solves before lapse.
- **Proxy pool**: controller assigns proxies round-robin from file/HTTP pool. Health tracking per proxy.

## Build & Run

```bash
make build                # controller + agent + testsite
make school-lp COUNT=5    # 5 Lightpanda agents
make school-cf COUNT=10   # 1 Chrome grouper + 10 minnows
make school-minnow        # 5 tls-client agents
make school-mixed          # 2 Lightpanda + 5 minnows
make stop
```

## Testing

```bash
go test -race ./...                  # Go unit tests (39+)
python examples/stress_test.py       # reliability suite
python examples/scale_test.py        # multi-minnow parallel (needs school-cf)
python examples/login_test.py        # auth persistence (needs school-lp + testsite)
python examples/hlcu_test.py [MBL]   # CF-protected carrier (needs school-cf)
python examples/tides.py             # lowcountry tides (needs any school)
```

## API Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| POST | /fetch | One-shot: auto lease→navigate→release |
| POST | /lease | Acquire agent for a domain |
| POST | /request | Navigate via leased agent |
| POST | /release | Return agent to pool |
| POST | /renew | Force CF clearance renewal |
| POST | /register | Agent self-registration |
| GET | /pool/status | Pool counts |
| GET | /pool/agents | All fish identities |
| GET | /health | Controller health |
| GET | /metrics | Prometheus metrics |
| GET | /dashboard | Live web UI |
| GET | /dashboard/agents | Agent details (with state) |
| GET | /dashboard/timeseries | Event buckets for charts |

## Constants

Use `api.ConstantName` — never raw strings. Defined in `internal/api/constants.go`:
- Classes: `ClassHeavy`, `ClassMedium`, `ClassLight`
- States: `StateAvailable`, `StateLeased`
- Backends: `BackendStub`, `BackendCDP`, `BackendLightpanda`, `BackendChrome`, `BackendTLSClient`
- Health/Error constants for all status codes

## Environment Variables

All flags have `SHOAL_*` env var equivalents for container deployments:
- `SHOAL_ADDR`, `SHOAL_STORE`, `SHOAL_BACKEND`, `SHOAL_CONTROLLER`
- `SHOAL_HEALTH_INTERVAL`, `SHOAL_LEASE_TTL`, `SHOAL_MAX_MISSED_CHECKS`
- `SHOAL_PROXY_URL`, `SHOAL_PROXY_USER`, `SHOAL_PROXY_PASS`
- `SHOAL_PROXY_FILE`, `SHOAL_PROXY_API`, `SHOAL_PROXY_REFRESH`
- `SHOAL_CDP_URL`, `SHOAL_LIGHTPANDA_BIN`, `SHOAL_CHROME_BIN`, `SHOAL_USER_AGENT`

## File Layout

### Entry points
- `cmd/controller/main.go` — controller binary, flag/env parsing, proxy provider, graceful shutdown
- `cmd/agent/main.go` — agent binary, backend selection, graceful shutdown

### Controller
- `internal/controller/server.go` — HTTP handlers, /fetch one-shot, cookie handoff, lazy catch-up
- `internal/controller/pool.go` — agent pool, leases, AcquireWait (waiter queue), warm matching, reconnection
- `internal/controller/identity.go` — fish names (4-byte entropy), domain state, CF tracking
- `internal/controller/health.go` — background health checks, dead agent removal, lease TTL expiry
- `internal/controller/store.go` — JSON snapshot persistence (30s interval)
- `internal/controller/renewer.go` — proactive CF clearance renewal
- `internal/controller/proxy.go` — proxy pool provider, round-robin, health tracking, file/HTTP refresh
- `internal/controller/metrics.go` — Prometheus metric definitions
- `internal/controller/dashboard.go` — built-in web UI (HTML + JS)
- `internal/controller/timeseries.go` — event ring buffer for dashboard charts

### Agent
- `internal/agent/cdp.go` — CDP backend, persistent tab, CF challenge wait, actions, XHR capture, tab cleanup, output format
- `internal/agent/xhr.go` — XHR/fetch response capture via CDP Network events
- `internal/agent/chrome.go` — Chrome launcher, stealth injection, xvfb, CDP proxy auth, Flatpak support
- `internal/agent/tlsclient.go` — tls-client minnow, Chrome 146 TLS fingerprint, cookie injection, ProxySetter
- `internal/agent/agent.go` — agent lifecycle, registration loop, IP detection, ProxySetter application
- `internal/agent/server.go` — agent HTTP API (/navigate, /cookies/set, /health)
- `internal/agent/backend.go` — BrowserBackend + CookieSetter + ProxySetter interfaces, stub

### API
- `internal/api/types.go` — shared types (NavigateRequest, FetchRequest, XHRResponse, ProxyConfig, etc.)
- `internal/api/constants.go` — string constants for classes, states, backends, errors
- `internal/api/version.go` — version injection + codenames (xiphosura/mullet)
- `internal/api/env.go` — EnvOr, EnvDuration, EnvInt helpers

### Client libraries
- `pkg/shoal/client.go` — Go client (Fetch, Lease, Navigate, Release, Renew)
- `clients/python/shoal.py` — Python client (Shoal, Session, ShoalResponse)

## Release Naming

- **Major (vN.0.0)** = sealife species. Pre-1.0 uses tidal names.
- **Minor (vN.X.0)** = lowcountry events (King Tide, Shrimp Run, Pluff Mud)
- **Patch (vN.X.Y)** = no subtitle

Previous: v0.1.0 The First Tide, v0.2.0 Slack Tide, v0.3.0 Cast Net, v0.4.0 Low Tide
