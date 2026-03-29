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
	Class    string              `json:"class"` // "heavy" (grouper) or "light" (minnow)
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

	class := req.Class
	if class == "" {
		// Infer class from backend type
		switch req.Backend {
		case api.BackendTLSClient:
			class = api.ClassLight
		default:
			class = api.ClassHeavy
		}
	}

	identity := newIdentity(req.Backend, class, req.IP)

	p.agents[identity.ID] = &ManagedAgent{
		Address:  req.Address,
		Backend:  req.Backend,
		Class:    class,
		State:    api.StateAvailable,
		Identity: identity,
	}

	return identity.ID
}

// Acquire finds the best available agent for the requested domain.
// Filters by class if specified, then ranks by domain warmth.
func (p *Pool) Acquire(req api.LeaseRequest) (*Lease, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var bestAgent *ManagedAgent
	var bestID string
	bestWarmth := -1

	for id, a := range p.agents {
		if a.State != api.StateAvailable {
			continue
		}

		// Filter by class if requested
		if req.Class != "" && a.Class != req.Class {
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
		return nil, fmt.Errorf(api.ErrPoolExhausted)
	}

	bestAgent.State = api.StateLeased

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
		return fmt.Errorf(api.ErrLeaseNotFound)
	}

	agent, ok := p.agents[lease.AgentID]
	if ok {
		agent.State = api.StateAvailable
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
		return nil, fmt.Errorf(api.ErrLeaseNotFound)
	}

	agent, ok := p.agents[lease.AgentID]
	if !ok {
		return nil, fmt.Errorf(api.ErrAgentNotFound)
	}

	return agent, nil
}

// LightAgents returns all available light-class agents (minnows).
func (p *Pool) LightAgents() []*ManagedAgent {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var out []*ManagedAgent
	for _, a := range p.agents {
		if a.Class == api.ClassLight {
			out = append(out, a)
		}
	}
	return out
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
		case api.StateAvailable:
			available++
		case api.StateLeased:
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
