// Package api defines the shared types between controller and agent.
package api

// --- Agent Registration ---

// RegisterRequest is sent by an agent to the controller on startup.
type RegisterRequest struct {
	Address string `json:"address"` // host:port the agent is listening on
	Backend string `json:"backend"` // backend type: "stub", "lightpanda", "chrome", etc.
}

type RegisterResponse struct {
	AgentID string `json:"agent_id"`
}

// --- Lease API (client -> controller) ---

type LeaseRequest struct {
	Consumer string `json:"consumer"` // who's requesting, e.g. "oolu-scraper"
	Domain   string `json:"domain"`   // target domain, e.g. "hapag-lloyd.com"
}

type LeaseResponse struct {
	LeaseID string `json:"lease_id"`
	AgentID string `json:"agent_id"`
}

type ReleaseRequest struct {
	LeaseID string `json:"lease_id"`
}

type ReleaseResponse struct {
	Status string `json:"status"`
}

// --- Navigate (controller -> agent) ---

type NavigateRequest struct {
	URL        string `json:"url"`
	MaxTimeout int    `json:"max_timeout,omitempty"` // milliseconds
}

type NavigateResponse struct {
	URL       string            `json:"url"`
	Status    int               `json:"status"`
	HTML      string            `json:"html"`
	Cookies   []Cookie          `json:"cookies,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	UserAgent string            `json:"user_agent,omitempty"`
}

type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain,omitempty"`
	Path     string `json:"path,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	HTTPOnly bool   `json:"http_only,omitempty"`
}

// --- Request (client -> controller, routed to agent) ---

type RequestPayload struct {
	LeaseID    string `json:"lease_id"`
	URL        string `json:"url"`
	MaxTimeout int    `json:"max_timeout,omitempty"`
}

// --- Pool Status ---

type PoolStatus struct {
	Total     int `json:"total"`
	Available int `json:"available"`
	Leased    int `json:"leased"`
}

// --- Health ---

type HealthStatus struct {
	Status  string `json:"status"`            // "ok", "degraded", "unhealthy"
	Backend string `json:"backend,omitempty"`  // agent only
	Uptime  int64  `json:"uptime,omitempty"`   // seconds
	MemMB   int    `json:"mem_mb,omitempty"`   // memory usage
}

// --- Errors ---

type ErrorResponse struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}
