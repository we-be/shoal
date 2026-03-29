package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/we-be/shoal/internal/api"
)

func TestExpireLeases(t *testing.T) {
	pool := NewPool()
	events := NewEventLog(time.Minute)

	id := pool.Register(api.RegisterRequest{Address: ":8181", Backend: api.BackendStub})
	lease, _ := pool.Acquire(api.LeaseRequest{Consumer: "test", Domain: "example.com"})

	// Simulate the agent being idle longer than TTL
	pool.mu.Lock()
	pool.agents[id].Identity.LastUsed = time.Now().Add(-10 * time.Minute)
	pool.mu.Unlock()

	hc := NewHealthChecker(pool, events, HealthConfig{
		CheckInterval:   time.Second,
		LeaseTTL:        5 * time.Minute,
		AgentTimeout:    time.Second,
		MaxMissedChecks: 3,
	})

	hc.expireLeases()

	// Lease should be expired
	status := pool.Status()
	if status.Leased != 0 {
		t.Fatalf("expected 0 leased after expiry, got %d", status.Leased)
	}
	if status.Available != 1 {
		t.Fatalf("expected 1 available after expiry, got %d", status.Available)
	}

	// Original lease should be gone
	_, err := pool.GetAgent(lease.ID)
	if err == nil {
		t.Fatal("expected lease to be expired")
	}
}

func TestRemoveDeadAgent(t *testing.T) {
	pool := NewPool()
	events := NewEventLog(time.Minute)

	// Register an agent on a port nothing is listening on
	pool.Register(api.RegisterRequest{Address: "127.0.0.1:19999", Backend: api.BackendStub})

	hc := NewHealthChecker(pool, events, HealthConfig{
		CheckInterval:   time.Second,
		LeaseTTL:        5 * time.Minute,
		AgentTimeout:    100 * time.Millisecond,
		MaxMissedChecks: 2,
	})

	// Poll twice — should fail both times
	hc.pollAgents()
	hc.pollAgents()

	// Agent should be removed after 2 missed checks
	if pool.Status().Total != 0 {
		t.Fatalf("expected agent to be removed, still have %d", pool.Status().Total)
	}
}

func TestHealthyAgentSurvives(t *testing.T) {
	// Start a fake agent server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(api.HealthStatus{Status: api.HealthOK})
	}))
	defer ts.Close()

	pool := NewPool()
	events := NewEventLog(time.Minute)

	addr := ts.Listener.Addr().String()
	pool.Register(api.RegisterRequest{Address: addr, Backend: api.BackendStub})

	hc := NewHealthChecker(pool, events, HealthConfig{
		CheckInterval:   time.Second,
		LeaseTTL:        5 * time.Minute,
		AgentTimeout:    time.Second,
		MaxMissedChecks: 2,
	})

	// Poll multiple times — agent responds healthy
	hc.pollAgents()
	hc.pollAgents()
	hc.pollAgents()

	if pool.Status().Total != 1 {
		t.Fatal("healthy agent should survive polling")
	}
}

func TestDeadAgentLeaseCleanup(t *testing.T) {
	pool := NewPool()
	events := NewEventLog(time.Minute)

	pool.Register(api.RegisterRequest{Address: "127.0.0.1:19999", Backend: api.BackendStub})
	lease, _ := pool.Acquire(api.LeaseRequest{Consumer: "test", Domain: "example.com"})

	if pool.Status().Leased != 1 {
		t.Fatal("expected 1 leased")
	}

	hc := NewHealthChecker(pool, events, HealthConfig{
		CheckInterval:   time.Second,
		LeaseTTL:        5 * time.Minute,
		AgentTimeout:    100 * time.Millisecond,
		MaxMissedChecks: 1,
	})

	hc.pollAgents()

	// Agent removed, lease should be cleaned up too
	if pool.Status().Total != 0 {
		t.Fatal("dead agent should be removed")
	}

	_, err := pool.GetAgent(lease.ID)
	if err == nil {
		t.Fatal("orphaned lease should be cleaned up")
	}
}
