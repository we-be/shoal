package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/we-be/shoal/internal/api"
)

// Server is the controller's HTTP server — the tide that directs the shoal.
type Server struct {
	pool    *Pool
	mux     *http.ServeMux
	client  *http.Client
	started time.Time
}

func NewServer() *Server {
	s := &Server{
		pool: NewPool(),
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
		started: time.Now(),
	}
	s.mux = http.NewServeMux()

	// Agent registration
	s.mux.HandleFunc("POST /register", s.handleRegister)

	// Client-facing lease API
	s.mux.HandleFunc("POST /lease", s.handleLease)
	s.mux.HandleFunc("POST /request", s.handleRequest)
	s.mux.HandleFunc("POST /release", s.handleRelease)

	// Status & identity
	s.mux.HandleFunc("GET /pool/status", s.handlePoolStatus)
	s.mux.HandleFunc("GET /pool/agents", s.handlePoolAgents)
	s.mux.HandleFunc("GET /health", s.handleHealth)

	// Dashboard & metrics
	s.mux.HandleFunc("GET /dashboard", s.handleDashboard)
	s.mux.HandleFunc("GET /dashboard/agents", s.handleDashboardAgents)
	s.mux.Handle("GET /metrics", promhttp.Handler())

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// --- Agent Registration ---

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req api.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: api.ErrBadRequest, Detail: err.Error()})
		return
	}

	id := s.pool.Register(req)
	log.Printf("agent registered: %s (%s @ %s, ip=%s)", id, req.Backend, req.Address, req.IP)

	writeJSON(w, http.StatusOK, api.RegisterResponse{AgentID: id})
}

// --- Lease API ---

func (s *Server) handleLease(w http.ResponseWriter, r *http.Request) {
	var req api.LeaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: api.ErrBadRequest, Detail: err.Error()})
		return
	}

	lease, err := s.pool.Acquire(req)
	if err != nil {
		log.Printf("lease denied: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, api.ErrorResponse{
			Error:  api.ErrPoolExhausted,
			Detail: "no agents available — the shoal is fully committed",
		})
		return
	}

	log.Printf("lease granted: %s -> %s (consumer=%s, domain=%s)", lease.ID, lease.AgentID, req.Consumer, req.Domain)

	writeJSON(w, http.StatusOK, api.LeaseResponse{
		LeaseID: lease.ID,
		AgentID: lease.AgentID,
	})
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	var req api.RequestPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: api.ErrBadRequest, Detail: err.Error()})
		return
	}

	if req.LeaseID == "" || req.URL == "" {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: api.ErrBadRequest, Detail: "lease_id and url are required"})
		return
	}

	agent, err := s.pool.GetAgent(req.LeaseID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, api.ErrorResponse{Error: api.ErrLeaseNotFound, Detail: err.Error()})
		return
	}

	// Forward to agent
	domain := extractDomain(req.URL)
	timer := prometheus.NewTimer(requestDuration.WithLabelValues(domain, agent.Class))

	navReq := api.NavigateRequest{
		URL:        req.URL,
		MaxTimeout: req.MaxTimeout,
		Actions:    req.Actions,
	}
	resp, err := s.forwardToAgent(agent, navReq)
	timer.ObserveDuration()

	if err != nil {
		log.Printf("agent %s error: %v", agent.Identity.ID, err)
		requestsTotal.WithLabelValues(domain, agent.Class, "error").Inc()
		writeJSON(w, http.StatusBadGateway, api.ErrorResponse{Error: api.ErrAgentError, Detail: err.Error()})
		return
	}

	requestsTotal.WithLabelValues(domain, agent.Class, "ok").Inc()

	// Record what this fish learned — cookies, CF clearance, domain state
	s.pool.RecordNavigation(req.LeaseID, req.URL, resp.Cookies)

	// If a grouper just earned CF clearance, hand the cookies to minnows
	if agent.Class == api.ClassHeavy && hasCFClearance(resp.Cookies) {
		cfSolvesTotal.Inc()
		go s.propagateCookiesToMinnows(req.URL, resp.Cookies)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRelease(w http.ResponseWriter, r *http.Request) {
	var req api.ReleaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: api.ErrBadRequest, Detail: err.Error()})
		return
	}

	if err := s.pool.Release(req.LeaseID); err != nil {
		writeJSON(w, http.StatusNotFound, api.ErrorResponse{Error: api.ErrLeaseNotFound, Detail: err.Error()})
		return
	}

	log.Printf("lease released: %s", req.LeaseID)
	writeJSON(w, http.StatusOK, api.ReleaseResponse{Status: api.HealthOK})
}

// --- Status & Identity ---

func (s *Server) handlePoolStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.pool.Status())
}

func (s *Server) handlePoolAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.pool.Agents())
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := s.pool.Status()
	health := api.HealthOK
	if status.Total > 0 && status.Available == 0 {
		health = api.HealthSaturated
	}
	if status.Total == 0 {
		health = api.HealthNoAgents
	}

	writeJSON(w, http.StatusOK, api.HealthStatus{
		Status: health,
		Uptime: int64(time.Since(s.started).Seconds()),
	})
}

// --- Agent Communication ---

func (s *Server) forwardToAgent(agent *ManagedAgent, req api.NavigateRequest) (*api.NavigateResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	url := fmt.Sprintf("http://%s/navigate", agent.Address)
	resp, err := s.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("contacting agent %s: %w", agent.Identity.ID, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading agent response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp api.ErrorResponse
		json.Unmarshal(respBody, &errResp)
		return nil, fmt.Errorf("agent returned %d: %s", resp.StatusCode, errResp.Detail)
	}

	var navResp api.NavigateResponse
	if err := json.Unmarshal(respBody, &navResp); err != nil {
		return nil, fmt.Errorf("decoding agent response: %w", err)
	}

	return &navResp, nil
}

// propagateCookiesToMinnows pushes cookies from a grouper to all light agents.
// This is the handoff — the grouper earned cf_clearance, now the minnows
// can make fast requests with it.
func (s *Server) propagateCookiesToMinnows(navURL string, cookies []api.Cookie) {
	minnows := s.pool.LightAgents()
	if len(minnows) == 0 {
		return
	}

	setCookiesReq := api.SetCookiesRequest{
		URL:     navURL,
		Cookies: cookies,
	}
	body, err := json.Marshal(setCookiesReq)
	if err != nil {
		log.Printf("failed to marshal cookies for minnow handoff: %v", err)
		return
	}

	for _, m := range minnows {
		go func(addr string, id string) {
			url := fmt.Sprintf("http://%s/cookies/set", addr)
			resp, err := s.client.Post(url, "application/json", bytes.NewReader(body))
			if err != nil {
				log.Printf("cookie handoff to %s failed: %v", id, err)
				return
			}
			resp.Body.Close()
			cfHandoffsTotal.Inc()
			log.Printf("cookie handoff to %s: %d cookies for %s", id, len(cookies), navURL)
		}(m.Address, m.Identity.ID)
	}
}

// hasCFClearance checks if a cookie set contains cf_clearance.
func hasCFClearance(cookies []api.Cookie) bool {
	for _, c := range cookies {
		if c.Name == "cf_clearance" {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("json encode error: %v", err)
	}
}
