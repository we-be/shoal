package controller

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/we-be/shoal/internal/api"
)

// HealthConfig controls the health check loop behavior.
type HealthConfig struct {
	CheckInterval    time.Duration // how often to run checks (default 15s)
	LeaseTTL         time.Duration // max lease duration before auto-expire (default 5m)
	AgentTimeout     time.Duration // how long to wait for agent /health (default 5s)
	MaxMissedChecks  int           // remove agent after N consecutive failures (default 3)
}

func DefaultHealthConfig() HealthConfig {
	return HealthConfig{
		CheckInterval:   15 * time.Second,
		LeaseTTL:        5 * time.Minute,
		AgentTimeout:    5 * time.Second,
		MaxMissedChecks: 3,
	}
}

// healthState tracks per-agent health check state.
type healthState struct {
	missedChecks int
	lastCheck    time.Time
	lastOK       time.Time
}

// HealthChecker runs periodic health checks on the pool.
// Detects dead agents, expires stale leases, and keeps the school healthy.
type HealthChecker struct {
	pool    *Pool
	events  *EventLog
	config  HealthConfig
	client  *http.Client
	states  map[string]*healthState // agent_id -> state
	mu      sync.Mutex
	stopCh  chan struct{}
}

func NewHealthChecker(pool *Pool, events *EventLog, config HealthConfig) *HealthChecker {
	return &HealthChecker{
		pool:   pool,
		events: events,
		config: config,
		client: &http.Client{Timeout: config.AgentTimeout},
		states: make(map[string]*healthState),
		stopCh: make(chan struct{}),
	}
}

// Start begins the health check loop in a background goroutine.
func (hc *HealthChecker) Start() {
	go hc.loop()
	log.Printf("health checker started (interval=%s, lease_ttl=%s, max_missed=%d)",
		hc.config.CheckInterval, hc.config.LeaseTTL, hc.config.MaxMissedChecks)
}

func (hc *HealthChecker) Stop() {
	close(hc.stopCh)
}

func (hc *HealthChecker) loop() {
	ticker := time.NewTicker(hc.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-hc.stopCh:
			return
		case <-ticker.C:
			hc.check()
		}
	}
}

func (hc *HealthChecker) check() {
	hc.expireLeases()
	hc.pollAgents()
}

// expireLeases auto-releases leases that have exceeded the TTL.
func (hc *HealthChecker) expireLeases() {
	hc.pool.mu.Lock()
	defer hc.pool.mu.Unlock()

	now := time.Now()
	var expired []string

	for leaseID, lease := range hc.pool.leases {
		agent, ok := hc.pool.agents[lease.AgentID]
		if !ok {
			expired = append(expired, leaseID)
			continue
		}

		// Check if the lease has exceeded the TTL based on agent's last use time
		if now.Sub(agent.Identity.LastUsed) > hc.config.LeaseTTL {
			expired = append(expired, leaseID)
		}
	}

	for _, leaseID := range expired {
		lease := hc.pool.leases[leaseID]
		agent, ok := hc.pool.agents[lease.AgentID]
		if ok {
			agent.State = api.StateAvailable
		}
		delete(hc.pool.leases, leaseID)
		leasesExpired.Inc()
		log.Printf("lease expired: %s (agent=%s, consumer=%s, domain=%s)",
			leaseID, lease.AgentID, lease.Consumer, lease.Domain)
	}

	if len(expired) > 0 {
		hc.pool.updateGauges()
	}
}

// pollAgents checks each agent's /health endpoint.
func (hc *HealthChecker) pollAgents() {
	hc.pool.mu.RLock()
	agents := make(map[string]*ManagedAgent, len(hc.pool.agents))
	for id, a := range hc.pool.agents {
		agents[id] = a
	}
	hc.pool.mu.RUnlock()

	var deadFish []string

	for id, agent := range agents {
		healthy := hc.checkAgent(agent)

		hc.mu.Lock()
		state, ok := hc.states[id]
		if !ok {
			state = &healthState{}
			hc.states[id] = state
		}
		state.lastCheck = time.Now()

		if healthy {
			state.missedChecks = 0
			state.lastOK = time.Now()
		} else {
			state.missedChecks++
			if state.missedChecks >= hc.config.MaxMissedChecks {
				deadFish = append(deadFish, id)
			}
		}
		hc.mu.Unlock()
	}

	// Remove dead agents from pool
	if len(deadFish) > 0 {
		hc.pool.mu.Lock()
		for _, id := range deadFish {
			agent, ok := hc.pool.agents[id]
			if !ok {
				continue
			}

			// Release any active lease for this agent
			for leaseID, lease := range hc.pool.leases {
				if lease.AgentID == id {
					delete(hc.pool.leases, leaseID)
					leasesExpired.Inc()
					log.Printf("lease orphaned: %s (dead agent %s)", leaseID, id)
				}
			}

			log.Printf("removing dead agent: %s (%s @ %s, missed %d checks)",
				id, agent.Backend, agent.Address, hc.config.MaxMissedChecks)
			delete(hc.pool.agents, id)
			agentsRemoved.Inc()
		}
		hc.pool.updateGauges()
		hc.pool.mu.Unlock()

		// Clean up health states
		hc.mu.Lock()
		for _, id := range deadFish {
			delete(hc.states, id)
		}
		hc.mu.Unlock()
	}
}

// checkAgent pings a single agent's health endpoint.
func (hc *HealthChecker) checkAgent(agent *ManagedAgent) bool {
	url := fmt.Sprintf("http://%s/health", agent.Address)
	resp, err := hc.client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var health api.HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return false
	}

	return health.Status == api.HealthOK
}
