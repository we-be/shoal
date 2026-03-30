# Shoal

Browser orchestration platform. Separates orchestration (pool, leasing, routing) from automation (browser engines). Two Go binaries: `controller` and `agent`.

## Mental Model

The controller manages a pool of agents. Each agent wraps one browser backend. Three tiers:
- **Grouper** (heavy): full Chrome, solves CF Turnstile
- **Redfish** (medium): Lightpanda, JS rendering without CF
- **Minnow** (light): tls-client, HTTP with Chrome TLS fingerprint

Each agent gets a persistent lowcountry fish identity. The controller tracks cookies, CF clearance, and domain history per fish, then routes leases to the warmest match. CF clearance cookies auto-propagate from groupers to minnows.

## Key Architecture Decisions

- **Persistent tab, not fresh tab per request.** CDP backends keep one tab alive so cookies survive across navigations. Tab cleanup runs after each nav to close leaked popups.
- **Warm matching over round-robin.** A fish that already has `cf_clearance` for a domain is worth more than a cold one. Warmth scoring drives routing.
- **Lazy cookie catch-up.** If a minnow misses the initial CF handoff (race condition, late start), it gets cookies pushed on first lease for that domain.
- **Stateful navigation.** URL is optional in requests — omit it to run actions on the current page. Enables multi-step login flows (PingFederate, Okta).
- **Controller self-heals.** Health checks remove dead agents, lease TTLs expire abandoned leases, CF renewer refreshes clearance before expiry, pool snapshots to disk for restart recovery.
- **Agents reconnect.** Same address = same fish. Identity preserved across agent restarts.

## Build & Run

```bash
make build
make school-cf COUNT=10   # grouper + minnows (CF sites)
make school-lp COUNT=5    # lightpanda school (JS, no CF)
make school-minnow        # tls-client school (HTTP only)
make stop
```

## Testing

```bash
go test -race ./...       # unit tests
python examples/tides.py  # quick smoke test
```

## Conventions

- Use `api.ConstantName` for string literals (classes, states, backends, errors) — see `internal/api/constants.go`
- All CLI flags have `SHOAL_*` env var equivalents — see `internal/api/env.go`
- Agent codename: mullet. Controller codename: xiphosura.
- Release names follow lowcountry tidal/sealife themes — see `/release` command.
