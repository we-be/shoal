package tides

import (
	"testing"
	"time"
)

func TestIntervalAtPeak(t *testing.T) {
	c := New(Config{
		Baseline:  1 * time.Minute,
		Amplitude: 9 * time.Minute,
		Period:    24 * time.Hour,
		PeakHour:  13,
	})

	// At peak hour, interval should be near baseline
	peak := time.Date(2026, 3, 30, 13, 0, 0, 0, time.UTC)
	interval := c.IntervalAt(peak)

	if interval > 2*time.Minute {
		t.Fatalf("at peak, expected ~1m, got %s", interval)
	}
}

func TestIntervalAtTrough(t *testing.T) {
	c := New(Config{
		Baseline:  1 * time.Minute,
		Amplitude: 9 * time.Minute,
		Period:    24 * time.Hour,
		PeakHour:  13,
	})

	// 12 hours from peak = trough, interval should be near baseline+amplitude
	trough := time.Date(2026, 3, 30, 1, 0, 0, 0, time.UTC)
	interval := c.IntervalAt(trough)

	if interval < 8*time.Minute {
		t.Fatalf("at trough, expected ~10m, got %s", interval)
	}
}

func TestBoostSpeedsUp(t *testing.T) {
	c := New(DefaultConfig())

	// Get baseline interval at a fixed time
	fixed := time.Date(2026, 3, 30, 1, 0, 0, 0, time.UTC) // trough
	before := c.IntervalAt(fixed)

	c.SetBoost("volatility", 1.0) // double speed
	after := c.IntervalAt(fixed)

	if after >= before {
		t.Fatalf("boost should reduce interval: before=%s after=%s", before, after)
	}
	// With boost=1.0, interval should be roughly halved
	ratio := float64(after) / float64(before)
	if ratio > 0.6 {
		t.Fatalf("expected ~0.5x with boost=1.0, got %.2fx", ratio)
	}
}

func TestBoostClear(t *testing.T) {
	c := New(DefaultConfig())
	fixed := time.Date(2026, 3, 30, 6, 0, 0, 0, time.UTC)

	before := c.IntervalAt(fixed)
	c.SetBoost("news", 2.0)
	c.SetBoost("news", 0) // clear
	after := c.IntervalAt(fixed)

	if before != after {
		t.Fatalf("clearing boost should restore interval: before=%s after=%s", before, after)
	}
}

func TestStatus(t *testing.T) {
	c := New(DefaultConfig())
	c.SetBoost("volatility", 0.5)

	status := c.Status()
	if status.Interval <= 0 {
		t.Fatal("interval should be positive")
	}
	if status.Boosts["volatility"] != 0.5 {
		t.Fatalf("expected volatility boost 0.5, got %f", status.Boosts["volatility"])
	}
	validPhases := map[string]bool{"high": true, "rising": true, "falling": true, "low": true}
	if !validPhases[status.Phase] {
		t.Fatalf("unexpected phase: %s", status.Phase)
	}
}
