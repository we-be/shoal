package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

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

	// Status
	s.mux.HandleFunc("GET /pool/status", s.handlePoolStatus)
	s.mux.HandleFunc("GET /health", s.handleHealth)

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// --- Agent Registration ---

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req api.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: "bad_request", Detail: err.Error()})
		return
	}

	id := s.pool.Register(req)
	log.Printf("agent registered: %s (%s @ %s)", id, req.Backend, req.Address)

	writeJSON(w, http.StatusOK, api.RegisterResponse{AgentID: id})
}

// --- Lease API ---

func (s *Server) handleLease(w http.ResponseWriter, r *http.Request) {
	var req api.LeaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: "bad_request", Detail: err.Error()})
		return
	}

	lease, err := s.pool.Acquire(req)
	if err != nil {
		log.Printf("lease denied: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, api.ErrorResponse{
			Error:  "pool_exhausted",
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
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: "bad_request", Detail: err.Error()})
		return
	}

	if req.LeaseID == "" || req.URL == "" {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: "bad_request", Detail: "lease_id and url are required"})
		return
	}

	agent, err := s.pool.GetAgent(req.LeaseID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, api.ErrorResponse{Error: "lease_not_found", Detail: err.Error()})
		return
	}

	// Forward to agent
	navReq := api.NavigateRequest{
		URL:        req.URL,
		MaxTimeout: req.MaxTimeout,
	}
	resp, err := s.forwardToAgent(agent, navReq)
	if err != nil {
		log.Printf("agent %s error: %v", agent.ID, err)
		writeJSON(w, http.StatusBadGateway, api.ErrorResponse{Error: "agent_error", Detail: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRelease(w http.ResponseWriter, r *http.Request) {
	var req api.ReleaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: "bad_request", Detail: err.Error()})
		return
	}

	if err := s.pool.Release(req.LeaseID); err != nil {
		writeJSON(w, http.StatusNotFound, api.ErrorResponse{Error: "lease_not_found", Detail: err.Error()})
		return
	}

	log.Printf("lease released: %s", req.LeaseID)
	writeJSON(w, http.StatusOK, api.ReleaseResponse{Status: "ok"})
}

// --- Status ---

func (s *Server) handlePoolStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.pool.Status())
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := s.pool.Status()
	health := "ok"
	if status.Total > 0 && status.Available == 0 {
		health = "saturated"
	}
	if status.Total == 0 {
		health = "no_agents"
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
		return nil, fmt.Errorf("contacting agent %s: %w", agent.ID, err)
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("json encode error: %v", err)
	}
}
