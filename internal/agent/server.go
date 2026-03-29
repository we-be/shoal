package agent

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/we-be/shoal/internal/api"
)

// Server is the agent's HTTP server — the interface the controller talks to.
type Server struct {
	backend BrowserBackend
	mux     *http.ServeMux
}

func NewServer(backend BrowserBackend) *Server {
	s := &Server{backend: backend}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("POST /navigate", s.handleNavigate)
	s.mux.HandleFunc("GET /health", s.handleHealth)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleNavigate(w http.ResponseWriter, r *http.Request) {
	var req api.NavigateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{
			Error:  "bad_request",
			Detail: err.Error(),
		})
		return
	}

	if req.URL == "" {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{
			Error:  "bad_request",
			Detail: "url is required",
		})
		return
	}

	log.Printf("navigating to %s", req.URL)

	resp, err := s.backend.Navigate(r.Context(), req)
	if err != nil {
		log.Printf("navigate error: %v", err)
		writeJSON(w, http.StatusBadGateway, api.ErrorResponse{
			Error:  "navigate_error",
			Detail: err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.backend.Health())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("json encode error: %v", err)
	}
}
