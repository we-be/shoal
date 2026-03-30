<div align="center">

<img src="assets/logo.png" alt="Shoal" height="160">

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

## Go Client

```go
import "github.com/we-be/shoal/pkg/shoal"

client := shoal.NewClient("http://localhost:8180")

// One-liner fetch
resp, _ := client.Fetch(ctx, "https://example.com", "my-scraper")
fmt.Println(resp.HTML)

// With options
resp, _ = client.Fetch(ctx, "https://example.com", "my-scraper",
    shoal.WithClass("heavy"),
    shoal.WithActions([]api.Action{{Type: "click", Selector: "#btn"}}),
    shoal.WithCaptureXHR("api/v1"),
)

// Low-level lease control
lease, _ := client.Lease(ctx, "my-scraper", "example.com")
resp, _ = client.Navigate(ctx, lease.LeaseID, "https://example.com")
client.Release(ctx, lease.LeaseID)
```

## API

```bash
# Simple fetch (auto lease/release)
curl -X POST localhost:8180/fetch \
  -d '{"url": "https://example.com", "consumer": "my-scraper"}'

# Manual lease lifecycle
curl -X POST localhost:8180/lease \
  -d '{"consumer": "my-scraper", "domain": "example.com"}'
curl -X POST localhost:8180/request \
  -d '{"lease_id": "lease-abc123", "url": "https://example.com"}'
curl -X POST localhost:8180/release \
  -d '{"lease_id": "lease-abc123"}'

# Stateful multi-step flow (omit URL to stay on current page)
curl -X POST localhost:8180/request -d '{
  "lease_id": "lease-abc123",
  "actions": [{"type": "click", "selector": "#next-page"}]
}'

# Capture XHR/Fetch responses from the page
curl -X POST localhost:8180/request -d '{
  "lease_id": "lease-abc123",
  "url": "https://example.com/app",
  "capture_xhr": true,
  "capture_xhr_filter": "api/v1"
}'

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

## Proxy Support

Per-agent: `-proxy-url http://host:port -proxy-user user -proxy-pass pass`

Controller-level pool: `--proxy-file proxies.json` or `--proxy-api http://api/proxies`

Proxies are assigned round-robin to agents on registration.

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
- **Tab cleanup** — leaked Chrome tabs closed after every navigation
- **39 Go tests** with `-race` in CI
