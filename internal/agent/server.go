package agent

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/we-be/shoal/internal/api"
)

// CookieSetter is an optional interface backends can implement to accept
// injected cookies from the controller. This is how grouper cookies flow
// downstream to minnows.
type CookieSetter interface {
	SetCookies(targetURL string, cookies []api.Cookie) error
}

// Server is the agent's HTTP server — the interface the controller talks to.
type Server struct {
	backend BrowserBackend
	mux     *http.ServeMux
}

func NewServer(backend BrowserBackend) *Server {
	s := &Server{backend: backend}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("POST /navigate", s.handleNavigate)
	s.mux.HandleFunc("POST /cookies/set", s.handleSetCookies)
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
			Error:  api.ErrBadRequest,
			Detail: err.Error(),
		})
		return
	}

	if req.URL == "" && len(req.Actions) == 0 {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{
			Error:  api.ErrBadRequest,
			Detail: "url or actions required",
		})
		return
	}

	log.Printf("navigating to %s", req.URL)

	resp, err := s.backend.Navigate(r.Context(), req)
	if err != nil {
		log.Printf("navigate error: %v", err)
		writeJSON(w, http.StatusBadGateway, api.ErrorResponse{
			Error:  api.ErrNavigateError,
			Detail: err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSetCookies(w http.ResponseWriter, r *http.Request) {
	setter, ok := s.backend.(CookieSetter)
	if !ok {
		writeJSON(w, http.StatusNotImplemented, api.ErrorResponse{
			Error:  api.ErrNotSupported,
			Detail: "this backend does not support cookie injection",
		})
		return
	}

	var req api.SetCookiesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.ErrorResponse{Error: api.ErrBadRequest, Detail: err.Error()})
		return
	}

	if err := setter.SetCookies(req.URL, req.Cookies); err != nil {
		writeJSON(w, http.StatusInternalServerError, api.ErrorResponse{Error: "cookie_error", Detail: err.Error()})
		return
	}

	log.Printf("injected %d cookies for %s", len(req.Cookies), req.URL)
	writeJSON(w, http.StatusOK, map[string]string{"status": api.HealthOK})
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
