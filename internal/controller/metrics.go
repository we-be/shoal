package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Pool gauges — the school's vital signs
	poolAgentsTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "shoal",
		Subsystem: "pool",
		Name:      "agents_total",
		Help:      "Total agents in the pool by class and backend.",
	}, []string{"class", "backend"})

	poolAgentsAvailable = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "shoal",
		Subsystem: "pool",
		Name:      "agents_available",
		Help:      "Available (unleased) agents by class.",
	}, []string{"class"})

	poolAgentsLeased = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "shoal",
		Subsystem: "pool",
		Name:      "agents_leased",
		Help:      "Currently leased agents by class.",
	}, []string{"class"})

	// Lease counters
	leasesAcquired = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "shoal",
		Subsystem: "lease",
		Name:      "acquired_total",
		Help:      "Total leases acquired by class and consumer.",
	}, []string{"class", "consumer"})

	leasesDenied = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "shoal",
		Subsystem: "lease",
		Name:      "denied_total",
		Help:      "Total lease requests denied (pool exhausted).",
	})

	leasesReleased = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "shoal",
		Subsystem: "lease",
		Name:      "released_total",
		Help:      "Total leases released.",
	})

	// Request metrics
	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "shoal",
		Subsystem: "request",
		Name:      "total",
		Help:      "Total requests by domain, class, and result.",
	}, []string{"domain", "class", "result"})

	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "shoal",
		Subsystem: "request",
		Name:      "duration_seconds",
		Help:      "Request duration in seconds by domain and class.",
		Buckets:   []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
	}, []string{"domain", "class"})

	// CF clearance tracking
	cfSolvesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "shoal",
		Subsystem: "cf",
		Name:      "solves_total",
		Help:      "Total CF clearance cookies earned by groupers.",
	})

	cfHandoffsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "shoal",
		Subsystem: "cf",
		Name:      "handoffs_total",
		Help:      "Total cookie handoffs to minnows.",
	})

	// Agent registration
	agentRegistrations = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "shoal",
		Subsystem: "agent",
		Name:      "registrations_total",
		Help:      "Total agent registrations by backend.",
	}, []string{"backend", "class"})

	// Warm matching
	warmMatches = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "shoal",
		Subsystem: "pool",
		Name:      "warm_matches_total",
		Help:      "Lease acquisitions by warmth level.",
	}, []string{"warmth"})

	// CF auto-renewal
	cfRenewalsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "shoal",
		Subsystem: "cf",
		Name:      "renewals_total",
		Help:      "Total successful proactive CF clearance renewals.",
	})

	cfRenewalsFailed = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "shoal",
		Subsystem: "cf",
		Name:      "renewals_failed_total",
		Help:      "Total failed CF clearance renewal attempts.",
	})

	// Agent reconnection
	agentReconnections = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "shoal",
		Subsystem: "agent",
		Name:      "reconnections_total",
		Help:      "Total agent reconnections (same address, identity preserved).",
	})

	// Queue
	leaseQueued = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "shoal",
		Subsystem: "lease",
		Name:      "queued_total",
		Help:      "Total lease requests that waited in queue (pool was full).",
	})

	// Health checks
	leasesExpired = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "shoal",
		Subsystem: "lease",
		Name:      "expired_total",
		Help:      "Total leases auto-expired by health checker.",
	})

	agentsRemoved = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "shoal",
		Subsystem: "agent",
		Name:      "removed_total",
		Help:      "Total agents removed due to failed health checks.",
	})
)
