package controller

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/we-be/shoal/internal/api"
)

// GnoTrace v1 compatibility layer. Translates GnoTrace's session-based
// API into Shoal's lease API so existing consumers can migrate without
// code changes. Mount at /v1.

// v1Request is the GnoTrace v1 envelope format.
type v1Request struct {
	Cmd          string            `json:"cmd"`
	URL          string            `json:"url,omitempty"`
	Session      string            `json:"session,omitempty"`
	MaxTimeout   int               `json:"maxTimeout,omitempty"`
	Cookies      []json.RawMessage `json:"cookies,omitempty"`
	Actions      []json.RawMessage `json:"actions,omitempty"`
	DisableMedia *bool             `json:"disableMedia,omitempty"`
}

// v1Response wraps Shoal responses in GnoTrace's format.
type v1Response struct {
	Status         string      `json:"status"`
	Message        string      `json:"message,omitempty"`
	Session        string      `json:"session,omitempty"`
	Solution       *v1Solution `json:"solution,omitempty"`
	StartTimestamp int64       `json:"startTimestamp,omitempty"`
	EndTimestamp   int64       `json:"endTimestamp,omitempty"`
}

type v1Solution struct {
	URL       string          `json:"url,omitempty"`
	Status    int             `json:"status,omitempty"`
	Response  string          `json:"response,omitempty"` // HTML body
	Cookies   json.RawMessage `json:"cookies,omitempty"`
	UserAgent string          `json:"userAgent,omitempty"`
}

func (s *Server) handleV1(w http.ResponseWriter, r *http.Request) {
	var req v1Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, v1Response{Status: "error", Message: err.Error()})
		return
	}

	switch req.Cmd {
	case "sessions.create":
		s.v1CreateSession(w, r, req)
	case "sessions.list":
		s.v1ListSessions(w, r)
	case "sessions.destroy":
		s.v1DestroySession(w, r, req)
	case "request.get", "request.post":
		s.v1Request(w, r, req)
	default:
		writeJSON(w, http.StatusBadRequest, v1Response{Status: "error", Message: "unknown cmd: " + req.Cmd})
	}
}

// sessions.create → POST /lease
func (s *Server) v1CreateSession(w http.ResponseWriter, r *http.Request, req v1Request) {
	lease, err := s.pool.Acquire(api.LeaseRequest{
		Consumer: "v1-compat",
		Domain:   "v1-session",
	})
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, v1Response{Status: "error", Message: "pool exhausted"})
		return
	}

	log.Printf("v1 compat: session created %s -> %s", lease.ID, lease.AgentID)
	writeJSON(w, http.StatusOK, v1Response{
		Status:  "ok",
		Session: lease.ID,
	})
}

// sessions.list → list active leases
func (s *Server) v1ListSessions(w http.ResponseWriter, r *http.Request) {
	s.pool.mu.RLock()
	sessions := make([]string, 0, len(s.pool.leases))
	for id := range s.pool.leases {
		sessions = append(sessions, id)
	}
	s.pool.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"sessions": sessions,
	})
}

// sessions.destroy → POST /release
func (s *Server) v1DestroySession(w http.ResponseWriter, r *http.Request, req v1Request) {
	if err := s.pool.Release(req.Session); err != nil {
		writeJSON(w, http.StatusNotFound, v1Response{Status: "error", Message: err.Error()})
		return
	}

	log.Printf("v1 compat: session destroyed %s", req.Session)
	writeJSON(w, http.StatusOK, v1Response{Status: "ok"})
}

// request.get/request.post → POST /request
func (s *Server) v1Request(w http.ResponseWriter, r *http.Request, req v1Request) {
	leaseID := req.Session
	if leaseID == "" {
		// Sessionless: auto-lease, request, release
		lease, err := s.pool.Acquire(api.LeaseRequest{Consumer: "v1-compat", Domain: extractDomain(req.URL)})
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, v1Response{Status: "error", Message: "pool exhausted"})
			return
		}
		leaseID = lease.ID
		defer s.pool.Release(leaseID)
	}

	agent, err := s.pool.GetAgent(leaseID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, v1Response{Status: "error", Message: err.Error()})
		return
	}

	timeout := req.MaxTimeout
	if timeout == 0 {
		timeout = 60000
	}

	navReq := api.NavigateRequest{
		URL:        req.URL,
		MaxTimeout: timeout,
	}

	resp, err := s.forwardToAgent(r.Context(), agent, navReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, v1Response{Status: "error", Message: err.Error()})
		return
	}

	s.pool.RecordNavigation(leaseID, req.URL, resp.Cookies)

	// Translate cookies to JSON
	cookiesJSON, _ := json.Marshal(resp.Cookies)

	writeJSON(w, http.StatusOK, v1Response{
		Status: "ok",
		Solution: &v1Solution{
			URL:       resp.URL,
			Status:    resp.Status,
			Response:  resp.HTML,
			Cookies:   cookiesJSON,
			UserAgent: resp.UserAgent,
		},
	})
}
