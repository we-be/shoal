// Package api defines the shared types between controller and agent.
package api

import "time"

// --- Agent Registration ---

// RegisterRequest is sent by an agent to the controller on startup.
type RegisterRequest struct {
	Address string `json:"address"` // host:port the agent is listening on
	Backend string `json:"backend"` // backend type: "stub", "lightpanda", "chrome", "tls-client", etc.
	Class   string `json:"class"`   // "heavy" (full browser) or "light" (HTTP client)
	IP      string `json:"ip,omitempty"` // external IP of the agent
}

type RegisterResponse struct {
	AgentID string       `json:"agent_id"`
	Proxy   *ProxyConfig `json:"proxy,omitempty"` // assigned proxy from the pool
}

// --- Proxy Config ---

type ProxyConfig struct {
	URL      string `json:"url"`                // http://host:port or socks5://host:port
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// --- Lease API (client -> controller) ---

type LeaseRequest struct {
	Consumer string `json:"consumer"`         // who's requesting, e.g. "oolu-scraper"
	Domain   string `json:"domain"`           // target domain, e.g. "example.com"
	Class    string `json:"class,omitempty"`   // "heavy", "light", or "" (auto)
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
	URL              string   `json:"url"`
	MaxTimeout       int      `json:"max_timeout,omitempty"`       // milliseconds
	Actions          []Action `json:"actions,omitempty"`           // post-navigation actions
	CaptureXHR       bool     `json:"capture_xhr,omitempty"`      // capture XHR/fetch responses
	CaptureXHRFilter string   `json:"capture_xhr_filter,omitempty"` // URL substring filter
}

// Action is a browser automation step — fill a form, click a button, wait.
type Action struct {
	Type     string `json:"type"`               // "fill", "click", "wait"
	Selector string `json:"selector"`           // CSS selector
	Value    string `json:"value,omitempty"`     // for "fill"
	WaitMS   int    `json:"wait_ms,omitempty"`  // for "wait"
}

type NavigateResponse struct {
	URL          string            `json:"url"`
	Status       int               `json:"status"`
	HTML         string            `json:"html"`
	Cookies      []Cookie          `json:"cookies,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	UserAgent    string            `json:"user_agent,omitempty"`
	XHRResponses []XHRResponse     `json:"xhr_responses,omitempty"`
}

// XHRResponse is a captured XHR/fetch response from the browser.
type XHRResponse struct {
	URL     string            `json:"url"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body"`
}

type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain,omitempty"`
	Path     string  `json:"path,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
	HTTPOnly bool    `json:"http_only,omitempty"`
	Expires  float64 `json:"expires,omitempty"` // seconds since epoch, -1 for session
}

// --- Cookie Injection (controller -> agent) ---

type SetCookiesRequest struct {
	URL     string   `json:"url"`
	Cookies []Cookie `json:"cookies"`
}

// --- Fetch (simple one-shot API) ---

type FetchRequest struct {
	URL              string   `json:"url"`
	Consumer         string   `json:"consumer,omitempty"`         // who's requesting (default: "fetch")
	Class            string   `json:"class,omitempty"`            // "heavy", "light", or "" (auto)
	MaxTimeout       int      `json:"max_timeout,omitempty"`      // milliseconds
	Actions          []Action `json:"actions,omitempty"`
	CaptureXHR       bool     `json:"capture_xhr,omitempty"`
	CaptureXHRFilter string   `json:"capture_xhr_filter,omitempty"`
}

// --- Request (client -> controller, routed to agent) ---

type RequestPayload struct {
	LeaseID          string   `json:"lease_id"`
	URL              string   `json:"url"`
	MaxTimeout       int      `json:"max_timeout,omitempty"`
	Actions          []Action `json:"actions,omitempty"`
	CaptureXHR       bool     `json:"capture_xhr,omitempty"`
	CaptureXHRFilter string   `json:"capture_xhr_filter,omitempty"`
}

// --- Pool Status ---

type PoolStatus struct {
	Total     int `json:"total"`
	Available int `json:"available"`
	Leased    int `json:"leased"`
}

// --- Browser Identity ---
// Each fish in the shoal has a persistent identity — its accumulated
// cookies, sessions, and fingerprint history. This is what makes a warm
// browser valuable: it's already swum through these waters.

type BrowserIdentity struct {
	ID        string                  `json:"id"`         // e.g. "redfish-a3b2"
	IP        string                  `json:"ip,omitempty"`
	Backend   string                  `json:"backend"`
	Class     string                  `json:"class"`      // "heavy" (grouper) or "light" (minnow)
	CreatedAt time.Time               `json:"created_at"`
	LastUsed  time.Time               `json:"last_used"`
	UseCount  int                     `json:"use_count"`
	Domains   map[string]*DomainState `json:"domains"`
}

// DomainState tracks what a browser has accumulated on a specific domain —
// the dirt and oil on its hands.
type DomainState struct {
	LastVisited    time.Time         `json:"last_visited"`
	VisitCount     int               `json:"visit_count"`
	Cookies        []Cookie          `json:"cookies"`
	HasCFClearance bool              `json:"has_cf_clearance"`
	CFExpiry       *time.Time        `json:"cf_expiry,omitempty"`
	CFURL          string            `json:"cf_url,omitempty"` // URL that earned the clearance (for renewal)
	Tokens         map[string]string `json:"tokens,omitempty"` // named login sessions
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
