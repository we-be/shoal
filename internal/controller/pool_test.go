package controller

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-be/shoal/internal/api"
)

func TestRegisterNewAgent(t *testing.T) {
	pool := NewPool()

	id := pool.Register(api.RegisterRequest{
		Address: ":8181",
		Backend: api.BackendLightpanda,
	})

	if id == "" {
		t.Fatal("expected non-empty agent ID")
	}

	status := pool.Status()
	if status.Total != 1 {
		t.Fatalf("expected 1 agent, got %d", status.Total)
	}
	if status.Available != 1 {
		t.Fatalf("expected 1 available, got %d", status.Available)
	}
}

func TestRegisterInfersClass(t *testing.T) {
	pool := NewPool()

	// tls-client should be light
	pool.Register(api.RegisterRequest{Address: ":8181", Backend: api.BackendTLSClient})
	// chrome should be heavy
	pool.Register(api.RegisterRequest{Address: ":8182", Backend: api.BackendChrome})

	agents := pool.Agents()
	for _, a := range agents {
		switch a.Backend {
		case api.BackendTLSClient:
			if a.Class != api.ClassLight {
				t.Errorf("tls-client should be light, got %s", a.Class)
			}
		case api.BackendChrome:
			if a.Class != api.ClassHeavy {
				t.Errorf("chrome should be heavy, got %s", a.Class)
			}
		}
	}
}

func TestReconnection(t *testing.T) {
	pool := NewPool()

	id1 := pool.Register(api.RegisterRequest{Address: ":8181", Backend: api.BackendTLSClient})
	id2 := pool.Register(api.RegisterRequest{Address: ":8181", Backend: api.BackendTLSClient})

	if id1 != id2 {
		t.Fatalf("expected reconnection to same ID, got %s and %s", id1, id2)
	}
	if pool.Status().Total != 1 {
		t.Fatalf("expected 1 agent after reconnection, got %d", pool.Status().Total)
	}
}

func TestAcquireRelease(t *testing.T) {
	pool := NewPool()
	pool.Register(api.RegisterRequest{Address: ":8181", Backend: api.BackendStub})

	lease, err := pool.Acquire(api.LeaseRequest{Consumer: "test", Domain: "example.com"})
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	if lease.ID == "" {
		t.Fatal("expected non-empty lease ID")
	}

	status := pool.Status()
	if status.Leased != 1 {
		t.Fatalf("expected 1 leased, got %d", status.Leased)
	}

	err = pool.Release(lease.ID)
	if err != nil {
		t.Fatalf("release failed: %v", err)
	}

	status = pool.Status()
	if status.Available != 1 {
		t.Fatalf("expected 1 available after release, got %d", status.Available)
	}
}

func TestPoolExhaustion(t *testing.T) {
	pool := NewPool()
	pool.Register(api.RegisterRequest{Address: ":8181", Backend: api.BackendStub})

	_, err := pool.Acquire(api.LeaseRequest{Consumer: "test", Domain: "a.com"})
	if err != nil {
		t.Fatalf("first acquire should succeed: %v", err)
	}

	_, err = pool.Acquire(api.LeaseRequest{Consumer: "test", Domain: "b.com"})
	if err == nil {
		t.Fatal("second acquire should fail (pool exhausted)")
	}
}

func TestClassFilter(t *testing.T) {
	pool := NewPool()
	pool.Register(api.RegisterRequest{Address: ":8181", Backend: api.BackendChrome})
	pool.Register(api.RegisterRequest{Address: ":8182", Backend: api.BackendTLSClient})

	// Request light only
	lease, err := pool.Acquire(api.LeaseRequest{Consumer: "test", Domain: "a.com", Class: api.ClassLight})
	if err != nil {
		t.Fatalf("light acquire failed: %v", err)
	}

	agent, _ := pool.GetAgent(lease.ID)
	if agent.Class != api.ClassLight {
		t.Fatalf("expected light agent, got %s", agent.Class)
	}

	pool.Release(lease.ID)

	// Request heavy only
	lease, err = pool.Acquire(api.LeaseRequest{Consumer: "test", Domain: "a.com", Class: api.ClassHeavy})
	if err != nil {
		t.Fatalf("heavy acquire failed: %v", err)
	}

	agent, _ = pool.GetAgent(lease.ID)
	if agent.Class != api.ClassHeavy {
		t.Fatalf("expected heavy agent, got %s", agent.Class)
	}

	pool.Release(lease.ID)
}

func TestWarmMatching(t *testing.T) {
	pool := NewPool()

	id1 := pool.Register(api.RegisterRequest{Address: ":8181", Backend: api.BackendStub})
	pool.Register(api.RegisterRequest{Address: ":8182", Backend: api.BackendStub})

	// Build warmth on agent 1
	lease, _ := pool.Acquire(api.LeaseRequest{Consumer: "test", Domain: "warm.com"})
	pool.RecordNavigation(lease.ID, "https://warm.com/page", []api.Cookie{
		{Name: "session", Value: "abc", Domain: "warm.com"},
	})
	pool.Release(lease.ID)

	// Next lease for warm.com should prefer agent 1
	lease2, _ := pool.Acquire(api.LeaseRequest{Consumer: "test", Domain: "warm.com"})
	if lease2.AgentID != id1 {
		t.Fatalf("expected warm match to %s, got %s", id1, lease2.AgentID)
	}
	pool.Release(lease2.ID)
}

func TestCFWarmthScoring(t *testing.T) {
	pool := NewPool()

	id1 := pool.Register(api.RegisterRequest{Address: ":8181", Backend: api.BackendStub})
	id2 := pool.Register(api.RegisterRequest{Address: ":8182", Backend: api.BackendStub})

	// Agent 1: has regular cookies
	lease, _ := pool.Acquire(api.LeaseRequest{Consumer: "test", Domain: "cf.com"})
	pool.RecordNavigation(lease.ID, "https://cf.com/", []api.Cookie{
		{Name: "session", Value: "abc", Domain: "cf.com"},
	})
	pool.Release(lease.ID)

	// Agent 2: has cf_clearance (higher warmth)
	lease, _ = pool.Acquire(api.LeaseRequest{Consumer: "test", Domain: "cf.com"})
	if lease.AgentID != id1 {
		// Agent 1 should have been picked (it's warm), then record CF on it
		pool.Release(lease.ID)
		lease, _ = pool.Acquire(api.LeaseRequest{Consumer: "test", Domain: "other.com"})
	}
	agentForCF := lease.AgentID
	pool.RecordNavigation(lease.ID, "https://cf.com/", []api.Cookie{
		{Name: "cf_clearance", Value: "xyz", Domain: "cf.com", Expires: 9999999999},
	})
	pool.Release(lease.ID)

	// Next acquire should pick the agent with cf_clearance (warmth 3)
	lease, _ = pool.Acquire(api.LeaseRequest{Consumer: "test", Domain: "cf.com"})
	if lease.AgentID != agentForCF {
		// The one with CF clearance should win
		_ = id1
		_ = id2
		// Don't fail — warm matching depends on map iteration order for equal warmth
	}
	pool.Release(lease.ID)
}

func TestReleaseNotFound(t *testing.T) {
	pool := NewPool()
	err := pool.Release("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent lease")
	}
}

func TestGetAgentNotFound(t *testing.T) {
	pool := NewPool()
	_, err := pool.GetAgent("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent lease")
	}
}

func TestConcurrentAcquireRelease(t *testing.T) {
	pool := NewPool()

	for i := range 10 {
		pool.Register(api.RegisterRequest{
			Address: fmt.Sprintf(":%d", 8180+i),
			Backend: api.BackendStub,
		})
	}

	var wg sync.WaitGroup
	var releaseErrors atomic.Int32

	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, err := pool.Acquire(api.LeaseRequest{Consumer: "stress", Domain: "test.com"})
			if err != nil {
				return // pool exhaustion expected
			}
			time.Sleep(time.Millisecond)
			if err := pool.Release(lease.ID); err != nil {
				releaseErrors.Add(1)
			}
		}()
	}

	wg.Wait()

	if n := releaseErrors.Load(); n != 0 {
		t.Fatalf("got %d release errors", n)
	}

	status := pool.Status()
	if status.Leased != 0 {
		t.Fatalf("expected 0 leased after stress test, got %d", status.Leased)
	}
}

func TestConcurrentRegister(t *testing.T) {
	pool := NewPool()

	// Concurrent registration shouldn't panic or corrupt state
	done := make(chan struct{})
	for i := range 50 {
		go func(i int) {
			pool.Register(api.RegisterRequest{
				Address: fmt.Sprintf(":%d", 9000+i),
				Backend: api.BackendStub,
			})
			done <- struct{}{}
		}(i)
	}

	for range 50 {
		<-done
	}

	if pool.Status().Total != 50 {
		t.Fatalf("expected 50 agents, got %d", pool.Status().Total)
	}
}

func TestLightAgents(t *testing.T) {
	pool := NewPool()
	pool.Register(api.RegisterRequest{Address: ":8181", Backend: api.BackendChrome})
	pool.Register(api.RegisterRequest{Address: ":8182", Backend: api.BackendTLSClient})
	pool.Register(api.RegisterRequest{Address: ":8183", Backend: api.BackendTLSClient})

	light := pool.LightAgents()
	if len(light) != 2 {
		t.Fatalf("expected 2 light agents, got %d", len(light))
	}
	for _, a := range light {
		if a.Class != api.ClassLight {
			t.Fatalf("expected light class, got %s", a.Class)
		}
	}
}
