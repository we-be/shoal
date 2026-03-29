# Shoal

Browser orchestration platform — scale headless browsers like a school of fish.

Shoal separates **browser orchestration** (pool management, leasing, routing, scaling) from **browser automation** (the actual rendering engine). Plug in any browser backend — [Lightpanda](https://github.com/lightpanda-io/browser), Chrome, Camoufox, FlareSolverr — and Shoal handles the rest.

## Architecture

```
              ┌─────────────┐
 clients ────▶│  Controller │  manages pool, leases, routing
              └──────┬──────┘
                     │
        ┌────────────┼────────────┐
        ▼            ▼            ▼
   ┌─────────┐ ┌─────────┐ ┌─────────┐
   │  Agent  │ │  Agent  │ │  Agent  │  each wraps one browser
   └─────────┘ └─────────┘ └─────────┘
```

**Controller** — singleton process that manages the agent pool. Clients talk to the controller to acquire leases, make requests, and release agents. The controller routes requests to agents and tracks pool state.

**Agent** — lightweight sidecar that wraps a single browser instance. Implements the `BrowserBackend` interface. Agents register with the controller on startup and respond to navigate requests. This is the unit of horizontal scaling.

## Quick Start

```bash
# Build both binaries
go build -o bin/controller ./cmd/controller
go build -o bin/agent ./cmd/agent

# Start controller
./bin/controller -addr :8180

# Start agents (in separate terminals)
./bin/agent -addr :8181 -controller http://localhost:8180
./bin/agent -addr :8182 -controller http://localhost:8180

# Or use docker compose
docker compose up
```

## Usage

```bash
# Check pool status
curl localhost:8180/pool/status

# Acquire a lease
curl -X POST localhost:8180/lease \
  -d '{"consumer": "my-scraper", "domain": "example.com"}'

# Make a request through the lease
curl -X POST localhost:8180/request \
  -d '{"lease_id": "lease-1", "url": "https://example.com"}'

# Release the lease
curl -X POST localhost:8180/release \
  -d '{"lease_id": "lease-1"}'
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

Currently ships with a `StubBackend` (plain HTTP fetch) for testing. Real backends coming soon:
- Lightpanda (CDP)
- Chrome/Chromium (CDP)
- Camoufox (Playwright)
