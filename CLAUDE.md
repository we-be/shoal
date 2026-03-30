# Shoal

Browser orchestration platform. Two Go binaries: `controller` and `agent`.

## Architecture

Controller manages a pool of agents. Each agent wraps a browser backend. Two tiers:
- **Grouper** (heavy): Chrome with stealth injection, solves CF Turnstile
- **Minnow** (light): tls-client with Chrome TLS fingerprint, bulk HTTP

Controller auto-propagates CF clearance cookies from groupers to minnows.

## Key Patterns

- **Fish identity**: each agent gets a persistent name (e.g. `redfish-a3b2c5d6`) with tracked cookies, domain state, CF clearance. Survives restarts via JSON snapshot.
- **Warm matching**: leases prefer agents that already have cookies/CF clearance for the requested domain.
- **Lazy cookie catch-up**: on lease, controller pushes cookies to minnows that missed the initial handoff.
- **CDP backends**: chrome.go manages Flatpak Chrome (attaches to existing page target, not creating new ones). cdp.go has persistent tab (not fresh tab per request) for cookie retention.
- **Tab cleanup**: `cleanupExtraTabs()` runs after every Navigate to close leaked Chrome tabs.
- **Stateful navigation**: URL is optional in NavigateRequest — omit it to stay on the current page for multi-step flows (PingFederate, Okta login sequences).
- **XHR capture**: `capture_xhr: true` intercepts XHR/Fetch responses via CDP Network events. Use `capture_xhr_filter` for URL substring matching.

## Build & Run

```bash
make build          # builds bin/controller + bin/agent + bin/testsite
make run            # 3 Lightpanda agents
make run-cf         # 1 Chrome grouper + N tls-client minnows (MINNOW_COUNT=10)
make stop
```

## Testing

```bash
go test -race ./...                  # Go unit tests (39 tests)
python examples/scrape.py            # LB + identity test (needs make run)
python examples/login_test.py        # auth persistence (needs make run)
python examples/scale_test.py        # multi-minnow parallel (needs make run-cf)
python examples/stress_test.py       # reliability suite
python examples/hlcu_test.py [MBL]   # CF-protected carrier test (needs make run-cf)
```

## Constants

Use `api.ConstantName` instead of string literals. Defined in `internal/api/constants.go`:
- Classes: `ClassHeavy`, `ClassLight`
- States: `StateAvailable`, `StateLeased`
- Backends: `BackendStub`, `BackendCDP`, `BackendLightpanda`, `BackendChrome`, `BackendTLSClient`
- Health: `HealthOK`, `HealthUnhealthy`, `HealthSaturated`, `HealthNoAgents`
- Errors: `ErrPoolExhausted`, `ErrLeaseNotFound`, etc.

## File Layout

- `cmd/controller/main.go` — controller binary, flag parsing, graceful shutdown, proxy provider setup
- `cmd/agent/main.go` — agent binary, backend selection, graceful shutdown
- `internal/controller/server.go` — HTTP handlers, cookie handoff, `/fetch` one-shot, `/renew`, context propagation
- `internal/controller/pool.go` — agent pool, leases, warm matching, reconnection, proxy assignment
- `internal/controller/identity.go` — fish names, domain state, CF tracking, CFURL
- `internal/controller/health.go` — background health checks, dead agent removal, lease TTL expiry
- `internal/controller/store.go` — JSON snapshot persistence
- `internal/controller/renewer.go` — proactive CF clearance renewal
- `internal/controller/proxy.go` — proxy pool provider, round-robin selection, file/HTTP loading
- `internal/controller/metrics.go` — Prometheus metrics definitions
- `internal/controller/dashboard.go` — built-in web UI HTML + JS
- `internal/controller/timeseries.go` — event ring buffer for dashboard charts
- `internal/agent/cdp.go` — CDP backend, persistent tab, CF challenge wait, actions, XHR capture, tab cleanup
- `internal/agent/chrome.go` — Chrome launcher, stealth injection, xvfb, CDP proxy auth, Flatpak support
- `internal/agent/tlsclient.go` — tls-client minnow, Chrome 146 TLS fingerprint, cookie injection
- `internal/agent/server.go` — agent HTTP API, cookie setter interface
- `internal/agent/backend.go` — BrowserBackend interface + stub
- `internal/api/types.go` — shared request/response types (NavigateRequest, XHRResponse, FetchRequest, ProxyConfig)
- `internal/api/constants.go` — string constants
- `pkg/shoal/client.go` — Go client library (Fetch, Lease, Navigate, Release)
