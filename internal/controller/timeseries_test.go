package controller

import (
	"testing"
	"time"
)

func TestEventLogRecord(t *testing.T) {
	el := NewEventLog(1 * time.Minute)

	el.Record("ok", "example.com", "light")
	el.Record("ok", "example.com", "light")
	el.Record("error", "example.com", "light")

	buckets := el.Buckets(5 * time.Second)

	totalOK := 0
	totalErr := 0
	for _, b := range buckets {
		totalOK += b.OK
		totalErr += b.Errors
	}

	if totalOK != 2 {
		t.Fatalf("expected 2 ok events, got %d", totalOK)
	}
	if totalErr != 1 {
		t.Fatalf("expected 1 error event, got %d", totalErr)
	}
}

func TestEventLogPrune(t *testing.T) {
	el := NewEventLog(100 * time.Millisecond)

	el.Record("ok", "a.com", "light")
	time.Sleep(200 * time.Millisecond)

	// After window, events should be pruned
	buckets := el.Buckets(50 * time.Millisecond)
	totalOK := 0
	for _, b := range buckets {
		totalOK += b.OK
	}

	if totalOK != 0 {
		t.Fatalf("expected 0 events after prune, got %d", totalOK)
	}
}

func TestEventLogCFSolve(t *testing.T) {
	el := NewEventLog(1 * time.Minute)

	el.Record("cf_solve", "example.com", "heavy")

	buckets := el.Buckets(5 * time.Second)
	totalCF := 0
	for _, b := range buckets {
		totalCF += b.CF
	}

	if totalCF != 1 {
		t.Fatalf("expected 1 cf_solve, got %d", totalCF)
	}
}
