# Examples

All examples assume the controller is running on `localhost:8180`.

## Getting Started

```bash
make build
```

| Example | Cluster | Command |
|---------|---------|---------|
| **tides.py** | `make run` | `python examples/tides.py` |
| **scrape.py** | `make run` | `python examples/scrape.py` |
| **login_test.py** | `make run` | `python examples/login_test.py` |
| **scale_test.py** | `make run-cf` | `python examples/scale_test.py` |
| **hlcu_test.py** | `make run-cf` | `python examples/hlcu_test.py [MBL]` |
| **stress_test.py** | `make run` | `python examples/stress_test.py` |

## Demos

### tides.py — Lowcountry Tides

Pulls high/low tide predictions for Charleston-area NOAA stations through Shoal. No auth, no CF — just a school of fish checking the tides.

```
$ python examples/tides.py
Charleston Harbor, SC (8665530)
  ↑ HIGH  2026-03-29 06:04  +5.5 ft
  ↓ LOW   2026-03-29 12:18  +0.1 ft
  ↑ HIGH  2026-03-29 18:33  +5.3 ft
```

### scrape.py — Load Balancer + Identity

Exercises the full pool: sequential scraping, concurrent fan-out across agents, warm matching, pool exhaustion. Shows how fish accumulate domain state and cookies across leases.

### login_test.py — Auth Persistence

Logs into a test site (`examples/testsite`) via `fetch()`, then verifies the session cookie persists across page navigations, detours to other domains, and lease release/re-acquire cycles.

Requires `make run` which starts the test site on `:9090`.

## CF Bypass

These require `make run-cf` (1 Chrome grouper + N minnows).

### scale_test.py — Multi-Minnow Scaling

One grouper solves CF, then N minnows scrape in parallel. Compares sequential vs parallel throughput and reports speedup. Proven 4.7x with 5 minnows on 10 pages.

### hlcu_test.py — Real-World CF Tracking

Scrapes a CF-protected shipping carrier site. Grouper busts through Cloudflare, hands cookies to minnow, minnow loads tracking pages. Pass an MBL number as argument for real data.

## Reliability

### stress_test.py — Stress Suite

Six tests: burst load (50 concurrent), lease TTL, agent reconnection, pool persistence, sustained throughput (30s), metrics integrity. Runs against any cluster.

## Test Site

`examples/testsite/main.go` is a small Go HTTP server with cookie-based auth (login: `hunter` / `shrimp`). Used by `login_test.py`. Started automatically by `make run`.
