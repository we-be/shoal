package controller

import (
	"crypto/rand"
	"fmt"
	"log"
	"sync"

	"github.com/we-be/shoal/internal/api"
)

// ManagedAgent tracks an agent in the pool.
type ManagedAgent struct {
	Address  string              `json:"address"`
	Backend  string              `json:"backend"`
	State    string              `json:"state"` // "available", "leased"
	Identity *api.BrowserIdentity `json:"identity"`
}

// Lease tracks an active lease binding a client to an agent.
type Lease struct {
	ID       string `json:"id"`
	AgentID  string `json:"agent_id"` // the fish ID
	Consumer string `json:"consumer"`
	Domain   string `json:"domain"`
}

// Pool manages the school — tracks registration, identities, leases, and routing.
type Pool struct {
	mu     sync.RWMutex
	agents map[string]*ManagedAgent // fish_id -> agent
	leases map[string]*Lease        // lease_id -> lease
}

func NewPool() *Pool {
	return &Pool{
		agents: make(map[string]*ManagedAgent),
		leases: make(map[string]*Lease),
	}
}

// Register adds a new agent to the pool and gives it a fish identity.
func (p *Pool) Register(req api.RegisterRequest) string {
	p.mu.Lock()
	defer p.mu.Unlock()

	identity := newIdentity(req.Backend, req.IP)

	p.agents[identity.ID] = &ManagedAgent{
		Address:  req.Address,
		Backend:  req.Backend,
		State:    "available",
		Identity: identity,
	}

	return identity.ID
}

// Acquire finds the best available agent for the requested domain.
// Preference order: warm (has CF clearance) > tepid (has cookies) > cold.
func (p *Pool) Acquire(req api.LeaseRequest) (*Lease, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var bestAgent *ManagedAgent
	var bestID string
	bestWarmth := -1

	for id, a := range p.agents {
		if a.State != "available" {
			continue
		}

		warmth := domainWarmth(a.Identity, req.Domain)
		if warmth > bestWarmth {
			bestWarmth = warmth
			bestAgent = a
			bestID = id
		}
	}

	if bestAgent == nil {
		return nil, fmt.Errorf("pool_exhausted")
	}

	bestAgent.State = "leased"

	leaseID := newLeaseID()
	lease := &Lease{
		ID:       leaseID,
		AgentID:  bestID,
		Consumer: req.Consumer,
		Domain:   req.Domain,
	}
	p.leases[leaseID] = lease

	if bestWarmth > 0 {
		log.Printf("warm match: %s has warmth %d for %s", bestID, bestWarmth, req.Domain)
	}

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

// GetAgent returns the managed agent for a given lease.
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

// RecordNavigation updates an agent's identity after a navigation.
func (p *Pool) RecordNavigation(leaseID string, navURL string, cookies []api.Cookie) {
	p.mu.Lock()
	defer p.mu.Unlock()

	lease, ok := p.leases[leaseID]
	if !ok {
		return
	}

	agent, ok := p.agents[lease.AgentID]
	if !ok {
		return
	}

	updateIdentity(agent.Identity, navURL, cookies)
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

// Agents returns all browser identities in the pool.
func (p *Pool) Agents() []api.BrowserIdentity {
	p.mu.RLock()
	defer p.mu.RUnlock()

	out := make([]api.BrowserIdentity, 0, len(p.agents))
	for _, a := range p.agents {
		out = append(out, *a.Identity)
	}
	return out
}

func newLeaseID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("lease-%x", b)
}
