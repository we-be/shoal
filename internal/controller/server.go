package controller

import (
	"bytes"
	"context"
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
	pool     *Pool
	events   *EventLog
	health   *HealthChecker
	store    *Store
	renewer  *CFRenewer
	proxies  ProxyProvider
	mux      *http.ServeMux
	client   *http.Client
	started  time.Time
}

func NewServer() *Server {
	return NewServerWithConfig(DefaultHealthConfig(), "shoal-pool.json", ":8180")
}

func NewServerWithConfig(healthCfg HealthConfig, storePath string, listenAddr string) *Server {
	pool := NewPool()
	events := NewEventLog(10 * time.Minute)

	// Restore pool state from disk
	store := NewStore(pool, storePath, 30*time.Second)
	if err := store.Load(); err != nil {
		log.Printf("store load error (continuing fresh): %v", err)
	}
	store.Start()

	s := &Server{
		pool:   pool,
		events: events,
		store:  store,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
		started: time.Now(),
	}

	// Start health checker and CF renewer
	s.health = NewHealthChecker(pool, events, healthCfg)
	s.health.Start()

	selfURL := fmt.Sprintf("http://localhost%s", listenAddr)
	s.renewer = NewCFRenewer(pool, events, DefaultRenewalConfig(), selfURL)
	s.renewer.Start()

	s.mux = http.NewServeMux()

	// Agent registration
	s.mux.HandleFunc("POST /register", s.handleRegister)

	// Simple API — fire and forget
	s.mux.HandleFunc("POST /fetch", s.handleFetch)

	// Lease API — full control
	s.mux.HandleFunc("POST /lease", s.handleLease)
	s.mux.HandleFunc("POST /request", s.handleRequest)
	s.mux.HandleFunc("POST /release", s.handleRelease)
	s.mux.HandleFunc("POST /renew", s.handleRenew)

	// Status & identity
	s.mux.HandleFunc("GET /pool/status", s.handlePoolStatus)
	s.mux.HandleFunc("GET /pool/agents", s.handlePoolAgents)
	s.mux.HandleFunc("GET /health", s.handleHealth)

	// Dashboard & metrics
	s.mux.HandleFunc("GET /dashboard", s.handleDashboard)
	s.mux.HandleFunc("GET /dashboard/agents", s.handleDashboardAgents)
	s.mux.HandleFunc("GET /dashboard/timeseries", s.handleTimeseries)
	s.mux.Handle("GET /metrics", promhttp.Handler())

	return s
}

// SetProxyProvider configures the proxy pool. Agents that register will
// be assigned proxies from this provider.
func (s *Server) SetProxyProvider(p ProxyProvider) {
	s.proxies = p
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Shutdown saves pool state and stops background goroutines.
func (s *Server) Shutdown() {
	s.renewer.Stop()
	s.health.Stop()
	s.store.Stop() // final snapshot
	log.Printf("controller shutdown complete")
}

// --- Simple API ---

func (s *Server) handleFetch(w http.ResponseWriter, r *http.Request) {
	var req api.FetchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: api.ErrBadRequest, Detail: err.Error()})
		return
	}

	if req.URL == "" {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: api.ErrBadRequest, Detail: "url is required"})
		return
	}

	consumer := req.Consumer
	if consumer == "" {
		consumer = "fetch"
	}
	domain := extractDomain(req.URL)

	// Acquire
	lease, err := s.pool.Acquire(api.LeaseRequest{Consumer: consumer, Domain: domain, Class: req.Class})
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, api.ErrorResponse{
			Error:  api.ErrPoolExhausted,
			Detail: "no agents available — the shoal is fully committed",
		})
		return
	}
	defer s.pool.Release(lease.ID)

	agent, err := s.pool.GetAgent(lease.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, api.ErrorResponse{Error: api.ErrAgentNotFound, Detail: err.Error()})
		return
	}

	// Lazy cookie catch-up
	go s.ensureMinnowCookies(agent, domain)

	// Navigate
	navReq := api.NavigateRequest{
		URL:              req.URL,
		MaxTimeout:       req.MaxTimeout,
		Actions:          req.Actions,
		CaptureXHR:       req.CaptureXHR,
		CaptureXHRFilter: req.CaptureXHRFilter,
	}

	timer := prometheus.NewTimer(requestDuration.WithLabelValues(domain, agent.Class))
	resp, err := s.forwardToAgent(r.Context(), agent, navReq)
	timer.ObserveDuration()

	if err != nil {
		requestsTotal.WithLabelValues(domain, agent.Class, "error").Inc()
		s.events.Record("error", domain, agent.Class)
		writeJSON(w, http.StatusBadGateway, api.ErrorResponse{Error: api.ErrAgentError, Detail: err.Error()})
		return
	}

	requestsTotal.WithLabelValues(domain, agent.Class, "ok").Inc()
	s.events.Record("ok", domain, agent.Class)
	s.pool.RecordNavigation(lease.ID, req.URL, resp.Cookies)

	if agent.Class == api.ClassHeavy && hasCFClearance(resp.Cookies) {
		cfSolvesTotal.Inc()
		s.events.Record("cf_solve", domain, agent.Class)
		go s.propagateCookiesToMinnows(req.URL, resp.Cookies)
	}

	writeJSON(w, http.StatusOK, resp)
}

// --- Agent Registration ---

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req api.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: api.ErrBadRequest, Detail: err.Error()})
		return
	}

	id := s.pool.Register(req)

	// Assign a proxy from the pool if available
	var proxy *api.ProxyConfig
	if s.proxies != nil {
		p, err := s.proxies.GetProxy()
		if err != nil {
			log.Printf("proxy assignment for %s failed: %v", id, err)
		} else {
			proxy = p
			log.Printf("agent registered: %s (%s @ %s, ip=%s, proxy=%s)", id, req.Backend, req.Address, req.IP, p.URL)
		}
	}
	if proxy == nil {
		log.Printf("agent registered: %s (%s @ %s, ip=%s)", id, req.Backend, req.Address, req.IP)
	}

	writeJSON(w, http.StatusOK, api.RegisterResponse{AgentID: id, Proxy: proxy})
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

	// Lazy catch-up: if a minnow is leased for a domain it has no cookies for,
	// copy them from a warm agent. Fixes the race where a minnow misses the
	// initial handoff because it wasn't ready when the grouper solved CF.
	if agent, err := s.pool.GetAgent(lease.ID); err == nil {
		go s.ensureMinnowCookies(agent, req.Domain)
	}

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

	if req.LeaseID == "" {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: api.ErrBadRequest, Detail: "lease_id is required"})
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
		URL:              req.URL,
		MaxTimeout:       req.MaxTimeout,
		Actions:          req.Actions,
		CaptureXHR:       req.CaptureXHR,
		CaptureXHRFilter: req.CaptureXHRFilter,
	}
	resp, err := s.forwardToAgent(r.Context(), agent, navReq)
	timer.ObserveDuration()

	if err != nil {
		log.Printf("agent %s error: %v", agent.Identity.ID, err)
		requestsTotal.WithLabelValues(domain, agent.Class, "error").Inc()
		s.events.Record("error", domain, agent.Class)
		writeJSON(w, http.StatusBadGateway, api.ErrorResponse{Error: api.ErrAgentError, Detail: err.Error()})
		return
	}

	requestsTotal.WithLabelValues(domain, agent.Class, "ok").Inc()
	s.events.Record("ok", domain, agent.Class)

	// Record what this fish learned — cookies, CF clearance, domain state
	s.pool.RecordNavigation(req.LeaseID, req.URL, resp.Cookies)

	// If a grouper just earned CF clearance, hand the cookies to minnows
	if agent.Class == api.ClassHeavy && hasCFClearance(resp.Cookies) {
		cfSolvesTotal.Inc()
		s.events.Record("cf_solve", domain, agent.Class)
		go s.propagateCookiesToMinnows(req.URL, resp.Cookies)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRenew(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Domain == "" {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: api.ErrBadRequest, Detail: "domain is required"})
		return
	}

	// Find the URL that earned CF clearance for this domain
	url := "https://www." + req.Domain + "/"
	s.pool.mu.RLock()
	for _, agent := range s.pool.agents {
		if state, ok := agent.Identity.Domains[req.Domain]; ok && state.CFURL != "" {
			url = state.CFURL
			break
		}
	}
	s.pool.mu.RUnlock()

	log.Printf("manual CF renewal requested for %s (url=%s)", req.Domain, url)
	go func() {
		if err := s.renewer.renewDomain(req.Domain, url); err != nil {
			log.Printf("manual renewal failed for %s: %v", req.Domain, err)
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status": "renewal_started",
		"domain": req.Domain,
	})
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

func (s *Server) forwardToAgent(ctx context.Context, agent *ManagedAgent, req api.NavigateRequest) (*api.NavigateResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	url := fmt.Sprintf("http://%s/navigate", agent.Address)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(httpReq)
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
// Retries failed handoffs to handle minnows that haven't finished starting.
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
			// Retry up to 3 times with backoff
			for attempt := range 3 {
				url := fmt.Sprintf("http://%s/cookies/set", addr)
				resp, err := s.client.Post(url, "application/json", bytes.NewReader(body))
				if err != nil {
					log.Printf("cookie handoff to %s attempt %d failed: %v", id, attempt+1, err)
					time.Sleep(time.Duration(attempt+1) * time.Second)
					continue
				}
				resp.Body.Close()
				cfHandoffsTotal.Inc()
				log.Printf("cookie handoff to %s: %d cookies for %s", id, len(cookies), navURL)
				return
			}
			log.Printf("cookie handoff to %s FAILED after 3 attempts", id)
		}(m.Address, m.Identity.ID)
	}
}

// ensureMinnowCookies checks if a minnow being leased has cookies for
// the requested domain. If not, copies them from a warm agent (grouper
// or another minnow that has them). This is the lazy catch-up for
// minnows that missed the initial handoff.
func (s *Server) ensureMinnowCookies(agent *ManagedAgent, domain string) {
	if agent.Class != api.ClassLight {
		return
	}

	// All reads under one lock to avoid racing with RecordNavigation
	s.pool.mu.RLock()
	// Check if this minnow already has cookies for the domain
	if state, ok := agent.Identity.Domains[domain]; ok && len(state.Cookies) > 0 {
		s.pool.mu.RUnlock()
		return
	}

	// Find a warm agent that has cookies for this domain
	var sourceCookies []api.Cookie
	var sourceURL string
	for _, other := range s.pool.agents {
		if state, ok := other.Identity.Domains[domain]; ok && len(state.Cookies) > 0 {
			sourceCookies = make([]api.Cookie, len(state.Cookies))
			copy(sourceCookies, state.Cookies)
			sourceURL = state.CFURL
			if sourceURL == "" {
				sourceURL = "https://www." + domain + "/"
			}
			break
		}
	}
	s.pool.mu.RUnlock()

	if len(sourceCookies) == 0 {
		return
	}

	// Push cookies to the minnow
	setCookiesReq := api.SetCookiesRequest{
		URL:     sourceURL,
		Cookies: sourceCookies,
	}
	body, _ := json.Marshal(setCookiesReq)
	url := fmt.Sprintf("http://%s/cookies/set", agent.Address)
	resp, err := s.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("lazy cookie push to %s failed: %v", agent.Identity.ID, err)
		return
	}
	resp.Body.Close()
	log.Printf("lazy cookie push to %s: %d cookies for %s", agent.Identity.ID, len(sourceCookies), domain)
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
