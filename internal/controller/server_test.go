package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/we-be/shoal/internal/api"
)

func newTestServer() *Server {
	return NewServer()
}

func postJSON(srv http.Handler, path string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func getJSON(srv http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestServerRegister(t *testing.T) {
	srv := newTestServer()

	w := postJSON(srv, "/register", api.RegisterRequest{
		Address: ":8181",
		Backend: api.BackendStub,
	})

	if w.Code != 200 {
		t.Fatalf("register returned %d: %s", w.Code, w.Body.String())
	}

	var resp api.RegisterResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.AgentID == "" {
		t.Fatal("expected agent ID")
	}
}

func TestServerLeaseRelease(t *testing.T) {
	srv := newTestServer()

	// Register an agent
	postJSON(srv, "/register", api.RegisterRequest{Address: ":8181", Backend: api.BackendStub})

	// Lease
	w := postJSON(srv, "/lease", api.LeaseRequest{Consumer: "test", Domain: "example.com"})
	if w.Code != 200 {
		t.Fatalf("lease returned %d: %s", w.Code, w.Body.String())
	}

	var lease api.LeaseResponse
	json.NewDecoder(w.Body).Decode(&lease)
	if lease.LeaseID == "" {
		t.Fatal("expected lease ID")
	}

	// Check pool status
	w = getJSON(srv, "/pool/status")
	var status api.PoolStatus
	json.NewDecoder(w.Body).Decode(&status)
	if status.Leased != 1 {
		t.Fatalf("expected 1 leased, got %d", status.Leased)
	}

	// Release
	w = postJSON(srv, "/release", api.ReleaseRequest{LeaseID: lease.LeaseID})
	if w.Code != 200 {
		t.Fatalf("release returned %d: %s", w.Code, w.Body.String())
	}

	// Check pool status
	w = getJSON(srv, "/pool/status")
	json.NewDecoder(w.Body).Decode(&status)
	if status.Available != 1 {
		t.Fatalf("expected 1 available after release, got %d", status.Available)
	}
}

func TestServerPoolExhausted(t *testing.T) {
	srv := newTestServer()

	postJSON(srv, "/register", api.RegisterRequest{Address: ":8181", Backend: api.BackendStub})

	// First lease succeeds
	w := postJSON(srv, "/lease", api.LeaseRequest{Consumer: "test", Domain: "a.com"})
	if w.Code != 200 {
		t.Fatalf("first lease should succeed: %d", w.Code)
	}

	// Second lease should fail (only 1 agent)
	w = postJSON(srv, "/lease", api.LeaseRequest{Consumer: "test", Domain: "b.com"})
	if w.Code != 503 {
		t.Fatalf("expected 503 pool exhausted, got %d", w.Code)
	}
}

func TestServerHealth(t *testing.T) {
	srv := newTestServer()

	// No agents — should report no_agents
	w := getJSON(srv, "/health")
	var health api.HealthStatus
	json.NewDecoder(w.Body).Decode(&health)
	if health.Status != api.HealthNoAgents {
		t.Fatalf("expected no_agents, got %s", health.Status)
	}

	// Register agent — should be ok
	postJSON(srv, "/register", api.RegisterRequest{Address: ":8181", Backend: api.BackendStub})
	w = getJSON(srv, "/health")
	json.NewDecoder(w.Body).Decode(&health)
	if health.Status != api.HealthOK {
		t.Fatalf("expected ok, got %s", health.Status)
	}

	// Lease the only agent — should be saturated
	postJSON(srv, "/lease", api.LeaseRequest{Consumer: "test", Domain: "a.com"})
	w = getJSON(srv, "/health")
	json.NewDecoder(w.Body).Decode(&health)
	if health.Status != api.HealthSaturated {
		t.Fatalf("expected saturated, got %s", health.Status)
	}
}

func TestServerPoolAgents(t *testing.T) {
	srv := newTestServer()

	postJSON(srv, "/register", api.RegisterRequest{Address: ":8181", Backend: api.BackendChrome, IP: "1.2.3.4"})
	postJSON(srv, "/register", api.RegisterRequest{Address: ":8182", Backend: api.BackendTLSClient})

	w := getJSON(srv, "/pool/agents")
	var agents []api.BrowserIdentity
	json.NewDecoder(w.Body).Decode(&agents)

	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}
}

func TestServerMetrics(t *testing.T) {
	srv := newTestServer()

	w := getJSON(srv, "/metrics")
	if w.Code != 200 {
		t.Fatalf("metrics returned %d", w.Code)
	}

	body := w.Body.String()
	if len(body) == 0 {
		t.Fatal("expected prometheus metrics output")
	}
}

func TestServerDashboard(t *testing.T) {
	srv := newTestServer()

	w := getJSON(srv, "/dashboard")
	if w.Code != 200 {
		t.Fatalf("dashboard returned %d", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "text/html" {
		t.Fatalf("expected text/html, got %s", ct)
	}
}

func TestServerRenewNoGrouper(t *testing.T) {
	srv := newTestServer()

	// Only register a minnow — renewal should fail (no grouper)
	postJSON(srv, "/register", api.RegisterRequest{Address: ":8181", Backend: api.BackendTLSClient})

	w := postJSON(srv, "/renew", map[string]string{"domain": "example.com"})
	if w.Code != 202 {
		t.Fatalf("renew should accept: %d", w.Code)
	}
}

func TestServerBadRequests(t *testing.T) {
	srv := newTestServer()

	// Lease with no body
	w := postJSON(srv, "/lease", "invalid")
	if w.Code != 400 {
		t.Fatalf("expected 400 for bad lease body, got %d", w.Code)
	}

	// Request with missing fields
	postJSON(srv, "/register", api.RegisterRequest{Address: ":8181", Backend: api.BackendStub})
	leaseW := postJSON(srv, "/lease", api.LeaseRequest{Consumer: "test", Domain: "a.com"})
	var lease api.LeaseResponse
	json.NewDecoder(leaseW.Body).Decode(&lease)

	// Request with empty URL is allowed (stateful multi-step flows, issue #7)
	// but missing lease_id should still be 400
	w = postJSON(srv, "/request", api.RequestPayload{LeaseID: "", URL: ""})
	if w.Code != 400 {
		t.Fatalf("expected 400 for missing lease_id, got %d", w.Code)
	}

	// Release nonexistent lease
	w = postJSON(srv, "/release", api.ReleaseRequest{LeaseID: "fake"})
	if w.Code != 404 {
		t.Fatalf("expected 404 for fake lease, got %d", w.Code)
	}
}

func TestTidesStatus(t *testing.T) {
	srv := newTestServer()

	w := getJSON(srv, "/tides/status")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var status map[string]any
	json.NewDecoder(w.Body).Decode(&status)

	if _, ok := status["interval"]; !ok {
		t.Fatal("expected interval in tides status")
	}
	if _, ok := status["phase"]; !ok {
		t.Fatal("expected phase in tides status")
	}
}

func TestTidesBoost(t *testing.T) {
	srv := newTestServer()

	// Set boost
	w := postJSON(srv, "/tides/boost", map[string]any{"name": "volatility", "factor": 1.5})
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status map[string]any
	json.NewDecoder(w.Body).Decode(&status)
	boosts, ok := status["boosts"].(map[string]any)
	if !ok {
		t.Fatal("expected boosts map in response")
	}
	if boosts["volatility"] != 1.5 {
		t.Fatalf("expected volatility=1.5, got %v", boosts["volatility"])
	}

	// Clear boost
	w = postJSON(srv, "/tides/boost", map[string]any{"name": "volatility", "factor": 0})
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestTidesBoostBadRequest(t *testing.T) {
	srv := newTestServer()

	w := postJSON(srv, "/tides/boost", map[string]any{"factor": 1.0})
	if w.Code != 400 {
		t.Fatalf("expected 400 for missing name, got %d", w.Code)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	srv := newTestServer()

	w := getJSON(srv, "/metrics")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if len(body) < 100 {
		t.Fatal("expected prometheus metrics output")
	}
}

func TestDashboardEndpoint(t *testing.T) {
	srv := newTestServer()

	w := getJSON(srv, "/dashboard")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "text/html" {
		t.Fatalf("expected text/html, got %s", w.Header().Get("Content-Type"))
	}
}
