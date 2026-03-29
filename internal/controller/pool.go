package controller

import (
	"fmt"
	"sync"

	"github.com/we-be/shoal/internal/api"
)

// ManagedAgent tracks an agent in the pool.
type ManagedAgent struct {
	ID      string `json:"id"`
	Address string `json:"address"` // host:port
	Backend string `json:"backend"` // "stub", "lightpanda", "chrome", etc.
	State   string `json:"state"`   // "available", "leased"
}

// Lease tracks an active lease binding a client to an agent.
type Lease struct {
	ID       string `json:"id"`
	AgentID  string `json:"agent_id"`
	Consumer string `json:"consumer"`
	Domain   string `json:"domain"`
}

// Pool manages the school of agents — tracks registration, leases, and routing.
type Pool struct {
	mu     sync.RWMutex
	agents map[string]*ManagedAgent // agent_id -> agent
	leases map[string]*Lease        // lease_id -> lease
	nextID int
}

func NewPool() *Pool {
	return &Pool{
		agents: make(map[string]*ManagedAgent),
		leases: make(map[string]*Lease),
	}
}

// Register adds a new agent to the pool.
func (p *Pool) Register(req api.RegisterRequest) string {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.nextID++
	id := fmt.Sprintf("agent-%d", p.nextID)

	p.agents[id] = &ManagedAgent{
		ID:      id,
		Address: req.Address,
		Backend: req.Backend,
		State:   "available",
	}

	return id
}

// Acquire finds an available agent and creates a lease.
func (p *Pool) Acquire(req api.LeaseRequest) (*Lease, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Find an available agent
	var target *ManagedAgent
	for _, a := range p.agents {
		if a.State == "available" {
			target = a
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("pool_exhausted")
	}

	target.State = "leased"

	leaseID := fmt.Sprintf("lease-%d", len(p.leases)+1)
	lease := &Lease{
		ID:       leaseID,
		AgentID:  target.ID,
		Consumer: req.Consumer,
		Domain:   req.Domain,
	}
	p.leases[leaseID] = lease

	return lease, nil
}

// Release returns an agent to the available pool.
func (p *Pool) Release(leaseID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	lease, ok := p.leases[leaseID]
	if !ok {
		return fmt.Errorf("lease_not_found")
	}

	agent, ok := p.agents[lease.AgentID]
	if ok {
		agent.State = "available"
	}

	delete(p.leases, leaseID)
	return nil
}

// GetAgent returns the agent address for a given lease.
func (p *Pool) GetAgent(leaseID string) (*ManagedAgent, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	lease, ok := p.leases[leaseID]
	if !ok {
		return nil, fmt.Errorf("lease_not_found")
	}

	agent, ok := p.agents[lease.AgentID]
	if !ok {
		return nil, fmt.Errorf("agent_not_found")
	}

	return agent, nil
}

// Status returns current pool counts.
func (p *Pool) Status() api.PoolStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	available := 0
	leased := 0
	for _, a := range p.agents {
		switch a.State {
		case "available":
			available++
		case "leased":
			leased++
		}
	}

	return api.PoolStatus{
		Total:     len(p.agents),
		Available: available,
		Leased:    leased,
	}
}
