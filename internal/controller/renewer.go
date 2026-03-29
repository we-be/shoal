package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/we-be/shoal/internal/api"
)

// RenewalConfig controls automatic CF clearance renewal.
type RenewalConfig struct {
	CheckInterval time.Duration // how often to scan for expiring clearances (default 30s)
	RenewBefore   time.Duration // renew this long before expiry (default 5m)
	RequestTimeout time.Duration // timeout for the renewal navigation (default 60s)
}

func DefaultRenewalConfig() RenewalConfig {
	return RenewalConfig{
		CheckInterval:  30 * time.Second,
		RenewBefore:    5 * time.Minute,
		RequestTimeout: 60 * time.Second,
	}
}

// CFRenewer watches for expiring cf_clearance cookies and proactively
// renews them via the grouper. The tide keeps flowing.
type CFRenewer struct {
	pool          *Pool
	events        *EventLog
	config        RenewalConfig
	controllerURL string // self-referencing URL for internal lease/request calls
	client        *http.Client
	// Track in-flight renewals to avoid duplicates
	renewing   map[string]bool // domain -> actively renewing
	renewingMu sync.Mutex
	stopCh     chan struct{}
}

func NewCFRenewer(pool *Pool, events *EventLog, config RenewalConfig, controllerURL string) *CFRenewer {
	return &CFRenewer{
		pool:          pool,
		events:        events,
		config:        config,
		controllerURL: controllerURL,
		client:        &http.Client{Timeout: config.RequestTimeout + 10*time.Second},
		renewing:      make(map[string]bool),
		stopCh:        make(chan struct{}),
	}
}

func (r *CFRenewer) Start() {
	go r.loop()
	log.Printf("cf renewer started (interval=%s, renew_before=%s)",
		r.config.CheckInterval, r.config.RenewBefore)
}

func (r *CFRenewer) Stop() {
	close(r.stopCh)
}

func (r *CFRenewer) loop() {
	ticker := time.NewTicker(r.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.scan()
		}
	}
}

// scan checks all domain states across all agents for expiring CF clearance.
func (r *CFRenewer) scan() {
	r.pool.mu.RLock()

	// Collect domains with expiring CF clearance
	type expiringDomain struct {
		domain string
		expiry time.Time
		url    string // we'll construct a URL to visit
	}

	now := time.Now()
	seen := make(map[string]bool)
	var expiring []expiringDomain

	for _, agent := range r.pool.agents {
		for domain, state := range agent.Identity.Domains {
			if seen[domain] || !state.HasCFClearance {
				continue
			}
			seen[domain] = true

			if state.CFExpiry == nil {
				continue
			}

			timeUntilExpiry := state.CFExpiry.Sub(now)
			if timeUntilExpiry <= r.config.RenewBefore && timeUntilExpiry > 0 {
				expiring = append(expiring, expiringDomain{
					domain: domain,
					expiry: *state.CFExpiry,
					url:    "https://" + domain + "/",
				})
			}
		}
	}
	r.pool.mu.RUnlock()

	// Renew each expiring domain
	for _, ed := range expiring {
		r.renewingMu.Lock()
		if r.renewing[ed.domain] {
			r.renewingMu.Unlock()
			continue
		}
		r.renewing[ed.domain] = true
		r.renewingMu.Unlock()

		go func(domain, url string, expiry time.Time) {
			defer func() {
				r.renewingMu.Lock()
				delete(r.renewing, domain)
				r.renewingMu.Unlock()
			}()

			timeLeft := time.Until(expiry).Round(time.Second)
			log.Printf("cf renewer: %s expires in %s, renewing...", domain, timeLeft)

			if err := r.renewDomain(domain, url); err != nil {
				log.Printf("cf renewer: %s renewal failed: %v", domain, err)
				cfRenewalsFailed.Inc()
			} else {
				log.Printf("cf renewer: %s renewed successfully", domain)
				cfRenewalsTotal.Inc()
			}
		}(ed.domain, ed.url, ed.expiry)
	}
}

// renewDomain acquires a grouper, navigates to the domain (triggering a CF
// solve if needed), and releases. The cookie handoff to minnows happens
// automatically in the controller's request handler.
func (r *CFRenewer) renewDomain(domain, url string) error {
	controllerURL := r.controllerURL

	// Acquire a grouper
	leaseReq := api.LeaseRequest{
		Consumer: "cf-renewer",
		Domain:   domain,
		Class:    api.ClassHeavy,
	}
	body, _ := json.Marshal(leaseReq)

	resp, err := r.client.Post(controllerURL+"/lease", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("acquiring grouper: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("no grouper available (status %d)", resp.StatusCode)
	}

	var leaseResp api.LeaseResponse
	json.NewDecoder(resp.Body).Decode(&leaseResp)

	// Navigate to the domain — triggers CF solve + cookie handoff
	navReq := api.RequestPayload{
		LeaseID:    leaseResp.LeaseID,
		URL:        url,
		MaxTimeout: int(r.config.RequestTimeout / time.Millisecond),
	}
	navBody, _ := json.Marshal(navReq)

	navResp, err := r.client.Post(controllerURL+"/request", "application/json", bytes.NewReader(navBody))
	if err != nil {
		// Release on error
		releaseBody, _ := json.Marshal(api.ReleaseRequest{LeaseID: leaseResp.LeaseID})
		r.client.Post(controllerURL+"/release", "application/json", bytes.NewReader(releaseBody))
		return fmt.Errorf("navigation failed: %w", err)
	}
	navResp.Body.Close()

	// Release the grouper
	releaseBody, _ := json.Marshal(api.ReleaseRequest{LeaseID: leaseResp.LeaseID})
	r.client.Post(controllerURL+"/release", "application/json", bytes.NewReader(releaseBody))

	return nil
}
