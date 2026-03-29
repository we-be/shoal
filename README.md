<div align="center">

**Scale headless browsers like a school of fish.**

[![CI](https://github.com/we-be/shoal/actions/workflows/ci.yml/badge.svg)](https://github.com/we-be/shoal/actions/workflows/ci.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/we-be/shoal)](https://go.dev)
[![License](https://img.shields.io/github/license/we-be/shoal)](LICENSE)
[![Release](https://img.shields.io/github/v/release/we-be/shoal)](https://github.com/we-be/shoal/releases)

| CF solve | Minnow latency | Parallel speedup | Success rate |
|:--:|:--:|:--:|:--:|
| **< 1s** | **~0.2s** | **4.7x** | **10/10** |

</div>

---

Shoal separates **orchestration** from **automation**. One Chrome grouper solves Cloudflare, then a school of minnows ride the earned cookies for fast parallel scraping.

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
```

## Quick Start

```bash
make build
make run-cf                    # 1 grouper + 10 minnows
make run-cf MINNOW_COUNT=20   # scale up
make run                       # lightpanda cluster (no CF)
make stop
```

Dashboard at `localhost:8180/dashboard`. Prometheus metrics at `localhost:8180/metrics`.

## API

```bash
# Lease an agent
curl -X POST localhost:8180/lease \
  -d '{"consumer": "my-scraper", "domain": "example.com"}'

# Navigate
curl -X POST localhost:8180/request \
  -d '{"lease_id": "lease-abc123", "url": "https://example.com"}'

# With actions
curl -X POST localhost:8180/request -d '{
  "lease_id": "lease-abc123",
  "url": "https://example.com/login",
  "actions": [
    {"type": "fill", "selector": "#username", "value": "user"},
    {"type": "submit", "selector": "#form"}
  ]
}'

# Release
curl -X POST localhost:8180/release -d '{"lease_id": "lease-abc123"}'

# Force CF renewal
curl -X POST localhost:8180/renew -d '{"domain": "example.com"}'
```

## Backends

```go
type BrowserBackend interface {
    Navigate(ctx context.Context, req NavigateRequest) (*NavigateResponse, error)
    Health() HealthStatus
    Close() error
}
```

| Backend | Class | Flag |
|---------|-------|------|
| Chrome | heavy | `-backend chrome` |
| [Lightpanda](https://github.com/lightpanda-io/browser) | heavy | `-backend lightpanda` |
| CDP (any browser) | heavy | `-backend cdp -cdp-url ws://...` |
| tls-client | light | `-backend tls-client` |
| Stub | heavy | `-backend stub` |

Proxy: `-proxy-url http://host:port -proxy-user user -proxy-pass pass`

## Identity & Warm Matching

Each agent gets a persistent lowcountry fish name (`redfish-a3b2`, `mullet-8d24`). The controller tracks cookies, CF clearance, and visit history per domain, then routes leases to the warmest available agent:

| Warmth | Meaning |
|--------|---------|
| 3 | Valid `cf_clearance` |
| 2 | Has cookies |
| 1 | Visited before |
| 0 | Cold |

## Reliability

- **Health checks** — dead agents removed after 3 missed polls
- **Lease TTLs** — abandoned leases auto-expire
- **Pool persistence** — JSON snapshots, survives controller restarts
- **Agent reconnection** — same address = same fish, cookies preserved
- **CF auto-renewal** — clearance refreshed before expiry
- **Cookie catch-up** — late-joining minnows get cookies on first lease
