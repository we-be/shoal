package api

// Agent classes — groupers vs minnows.
const (
	ClassHeavy  = "heavy"  // full browser with display: Chrome, Camoufox, FlareSolverr
	ClassMedium = "medium" // headless browser, no display: Lightpanda
	ClassLight  = "light"  // HTTP client: tls-client, curl-impersonate
)

// Agent states.
const (
	StateAvailable = "available"
	StateLeased    = "leased"
)

// Backend types.
const (
	BackendStub       = "stub"
	BackendCDP        = "cdp"
	BackendLightpanda = "lightpanda"
	BackendChrome     = "chrome"
	BackendTLSClient  = "tls-client"
)

// Health statuses.
const (
	HealthOK        = "ok"
	HealthUnhealthy = "unhealthy"
	HealthSaturated = "saturated"
	HealthNoAgents  = "no_agents"
)

// Error codes.
const (
	ErrPoolExhausted = "pool_exhausted"
	ErrLeaseNotFound = "lease_not_found"
	ErrAgentNotFound = "agent_not_found"
	ErrBadRequest    = "bad_request"
	ErrAgentError    = "agent_error"
	ErrNavigateError = "navigate_error"
	ErrTimeout       = "timeout"
	ErrNotSupported  = "not_supported"
)
