# Shoal

Browser orchestration platform — scale headless browsers like a school of fish.

Shoal separates **browser orchestration** (pool management, leasing, routing, scaling) from **browser automation** (the actual rendering engine). One Chrome "grouper" solves Cloudflare challenges, then a school of lightweight "minnows" ride the earned cookies for fast parallel scraping.

## Architecture

```
              ┌─────────────┐
 clients ────▶│  Controller │  pool, leases, warm matching, cookie handoff
              └──────┬──────┘
                     │
     ┌───────────────┼───────────────┐
     ▼                               ▼
┌──────────┐   cf_clearance    ┌───────────────┐
│ Grouper  │ ─────────────────▶│    Minnows    │
│ (Chrome) │   auto-handoff   │  (tls-client)  │
│  heavy   │                  │    light x N   │
└──────────┘                  └───────────────┘
 solves CF once                bulk requests
 full JS rendering             Chrome TLS fingerprint
```

**Controller** — manages the agent pool, leases, and routing. Tracks each browser's identity (cookies, CF clearance, domain history) and prefers "warm" agents that already have cookies for a requested domain. Automatically pushes `cf_clearance` cookies from groupers to minnows.

**Grouper** (heavy agent) — full Chrome browser with stealth injection (`navigator.webdriver` hidden, no `--headless` flag, xvfb-compatible). Solves Cloudflare Turnstile challenges and earns clearance cookies.

**Minnow** (light agent) — HTTP client with Chrome's exact TLS fingerprint (JA3/JA4) via [tls-client](https://github.com/bogdanfinn/tls-client). No browser overhead. Accepts injected cookies from groupers and makes fast bulk requests that Cloudflare can't distinguish from the real Chrome that earned the clearance.

## Results

Tested against Cloudflare Turnstile on [2captcha.com](https://2captcha.com/demo/cloudflare-turnstile-challenge) and real [Hapag-Lloyd](https://www.hapag-lloyd.com) container tracking:

| Metric | Value |
|--------|-------|
| CF Turnstile solve | < 1s |
| Minnow request (with CF cookies) | ~0.2s |
| Sequential (1 minnow, 10 MBLs) | 2.92s |
| Parallel (5 minnows, 10 MBLs) | 0.62s |
| **Speedup** | **4.7x** |
| Success rate | 10/10 |

## Quick Start

```bash
# Build
make build

# Start CF cluster: 1 Chrome grouper + 10 tls-client minnows
make run-cf

# Or with custom minnow count
make run-cf MINNOW_COUNT=20

# Start Lightpanda cluster (3 agents, no CF bypass)
make run

# Stop everything
make stop
```

## API

### Lease lifecycle

```bash
# Acquire a lease (auto-routes to best available agent)
curl -X POST localhost:8180/lease \
  -d '{"consumer": "my-scraper", "domain": "hapag-lloyd.com"}'

# Request a specific class
curl -X POST localhost:8180/lease \
  -d '{"consumer": "my-scraper", "domain": "hapag-lloyd.com", "class": "heavy"}'

# Make a request through the lease
curl -X POST localhost:8180/request \
  -d '{"lease_id": "lease-abc123", "url": "https://hapag-lloyd.com/tracking?blno=HLCU123"}'

# With browser actions (fill forms, click buttons)
curl -X POST localhost:8180/request -d '{
  "lease_id": "lease-abc123",
  "url": "https://example.com/login",
  "actions": [
    {"type": "fill", "selector": "#username", "value": "hunter"},
    {"type": "fill", "selector": "#password", "value": "shrimp"},
    {"type": "submit", "selector": "#login-form"}
  ]
}'

# Release the lease
curl -X POST localhost:8180/release \
  -d '{"lease_id": "lease-abc123"}'
```

### Pool status

```bash
# Pool counts
curl localhost:8180/pool/status

# All agent identities (cookies, domains, CF clearance, visit history)
curl localhost:8180/pool/agents

# Controller health
curl localhost:8180/health
```

## Browser Backends

Shoal is browser-agnostic. The agent accepts any implementation of:

```go
type BrowserBackend interface {
    Navigate(ctx context.Context, req NavigateRequest) (*NavigateResponse, error)
    Health() HealthStatus
    Close() error
}
```

Five backends ship today:

| Backend | Class | Use case | Flag |
|---------|-------|----------|------|
| **Chrome** | heavy | CF Turnstile solving, JS rendering | `-backend chrome` |
| **Lightpanda** | heavy | Fast headless browsing (no CF bypass) | `-backend lightpanda` |
| **CDP** | heavy | Connect to any CDP-speaking browser | `-backend cdp -cdp-url ws://...` |
| **tls-client** | light | Bulk HTTP with Chrome TLS fingerprint | `-backend tls-client` |
| **Stub** | heavy | Testing (plain HTTP GET) | `-backend stub` |

## Browser Identity

Each agent gets a persistent lowcountry fish identity — `redfish-a3b2`, `mullet-8d24`, `pompano-5c92`. The identity tracks:

- **IP address** — detected on startup
- **Cookies per domain** — accumulated across navigations, survive lease release
- **CF clearance** — flagged with expiry, drives warm matching
- **Visit history** — count and recency per domain
- **Use count** — total navigations

The controller uses warmth scoring for routing:

| Warmth | Meaning | Priority |
|--------|---------|----------|
| 3 | Has valid `cf_clearance` for domain | Highest |
| 2 | Has cookies for domain | |
| 1 | Has visited domain before | |
| 0 | Cold — never been there | Lowest |

## Cookie Handoff

When a grouper earns `cf_clearance`, the controller automatically pushes all cookies to every minnow via `POST /cookies/set`. Minnows can then make requests that Cloudflare accepts because:

1. Same `cf_clearance` cookie
2. Same TLS fingerprint (Chrome 146 JA3/JA4)
3. Same User-Agent string
4. Same IP address

## Project Structure

```
shoal/
├── cmd/
│   ├── controller/main.go      # Controller binary
│   └── agent/main.go           # Agent binary (all backends)
├── internal/
│   ├── controller/
│   │   ├── server.go           # HTTP API, cookie handoff
│   │   ├── pool.go             # Agent pool, leases, warm matching
│   │   └── identity.go         # Fish names, domain tracking
│   ├── agent/
│   │   ├── server.go           # Agent HTTP API
│   │   ├── backend.go          # BrowserBackend interface + stub
│   │   ├── cdp.go              # CDP backend (Lightpanda, generic)
│   │   ├── chrome.go           # Chrome with stealth + xvfb
│   │   └── tlsclient.go        # tls-client minnow backend
│   └── api/
│       ├── types.go            # Shared request/response types
│       └── constants.go        # Agent classes, states, backends
├── examples/
│   ├── scrape.py               # LB + identity test
│   ├── login_test.py           # Auth persistence test
│   ├── hlcu_test.py            # Hapag-Lloyd tracking test
│   ├── scale_test.py           # Multi-minnow parallel scaling
│   └── testsite/main.go        # Auth-gated test site
├── Makefile                    # build, run, run-cf, stop
├── Dockerfile
└── docker-compose.yml
```
