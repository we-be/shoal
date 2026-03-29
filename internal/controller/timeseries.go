package controller

import (
	"sync"
	"time"
)

// EventLog records timestamped events for the dashboard timeseries.
// Keeps a rolling window of events — no external dependencies needed.
type EventLog struct {
	mu     sync.Mutex
	events []event
	window time.Duration
}

type event struct {
	ts     time.Time
	kind   string // "ok", "error", "cf_solve"
	domain string
	class  string
}

// TimeseriesBucket is one data point in the timeseries.
type TimeseriesBucket struct {
	Time   int64 `json:"t"`      // unix seconds
	OK     int   `json:"ok"`
	Errors int   `json:"errors"`
	CF     int   `json:"cf"`
}

func NewEventLog(window time.Duration) *EventLog {
	return &EventLog{
		events: make([]event, 0, 1024),
		window: window,
	}
}

func (el *EventLog) Record(kind, domain, class string) {
	el.mu.Lock()
	defer el.mu.Unlock()
	el.events = append(el.events, event{
		ts:     time.Now(),
		kind:   kind,
		domain: domain,
		class:  class,
	})
	el.prune()
}

// Buckets returns the event log bucketed into intervals.
func (el *EventLog) Buckets(bucketSize time.Duration) []TimeseriesBucket {
	el.mu.Lock()
	defer el.mu.Unlock()
	el.prune()

	if len(el.events) == 0 {
		return []TimeseriesBucket{}
	}

	now := time.Now()
	start := now.Add(-el.window).Truncate(bucketSize)
	nBuckets := int(el.window/bucketSize) + 1

	buckets := make([]TimeseriesBucket, nBuckets)
	for i := range buckets {
		buckets[i].Time = start.Add(time.Duration(i) * bucketSize).Unix()
	}

	for _, e := range el.events {
		idx := int(e.ts.Sub(start) / bucketSize)
		if idx < 0 || idx >= nBuckets {
			continue
		}
		switch e.kind {
		case "ok":
			buckets[idx].OK++
		case "error":
			buckets[idx].Errors++
		case "cf_solve":
			buckets[idx].CF++
		}
	}

	return buckets
}

// prune removes events outside the window.
func (el *EventLog) prune() {
	cutoff := time.Now().Add(-el.window)
	i := 0
	for i < len(el.events) && el.events[i].ts.Before(cutoff) {
		i++
	}
	if i > 0 {
		el.events = el.events[i:]
	}
}
