// Package tides manages scraping cadence — when and how often to scrape.
// Models scraping frequency as a sinusoidal wave with adaptive boosts,
// like the lowcountry tides that govern when the fish swim.
//
// Usage:
//
//	cadence := tides.New(tides.Config{
//	    Baseline:  time.Minute,       // minimum interval (low tide)
//	    Amplitude: 5 * time.Minute,   // how much slower at lowest point
//	    Period:    24 * time.Hour,     // one full cycle
//	    PeakHour:  14,                // peak activity at 2pm (market hours)
//	})
//
//	interval := cadence.Interval()   // current interval based on time of day
//	cadence.BoostVolatility(0.5)     // speed up 50% for volatility
//	interval = cadence.Interval()    // now faster
package tides

import (
	"math"
	"sync"
	"time"
)

// Config defines the tidal cadence parameters.
type Config struct {
	// Baseline is the fastest scrape interval (at high tide / peak).
	Baseline time.Duration

	// Amplitude is the additional slowdown at low tide.
	// At the trough, interval = Baseline + Amplitude.
	Amplitude time.Duration

	// Period is the length of one full tidal cycle.
	// Use 24h for daily rhythm, or market hours for trading cadence.
	Period time.Duration

	// PeakHour is the hour of day (0-23) when scraping is fastest.
	// For US markets: 13-14 (midday trading). For overnight: 2-3.
	PeakHour int
}

// DefaultConfig returns a market-hours-aware cadence.
// Fastest during US market hours (9:30-16:00 ET), slowest overnight.
func DefaultConfig() Config {
	return Config{
		Baseline:  1 * time.Minute,
		Amplitude: 9 * time.Minute,
		Period:    24 * time.Hour,
		PeakHour:  13, // 1pm ET — middle of market hours
	}
}

// Cadence manages the tidal scraping rhythm.
type Cadence struct {
	config Config
	mu     sync.RWMutex
	boosts map[string]float64 // named boost factors (0.0 = no boost, 1.0 = double speed)
}

// New creates a cadence from the given config.
func New(config Config) *Cadence {
	return &Cadence{
		config: config,
		boosts: make(map[string]float64),
	}
}

// Interval returns the current scrape interval based on time of day
// and any active boosts. Returns a duration between Baseline (fastest)
// and Baseline+Amplitude (slowest).
func (c *Cadence) Interval() time.Duration {
	return c.IntervalAt(time.Now())
}

// IntervalAt returns the interval for a specific time.
func (c *Cadence) IntervalAt(t time.Time) time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Sinusoidal wave: fastest at PeakHour, slowest half-period later
	hourOfDay := float64(t.Hour()) + float64(t.Minute())/60.0
	peakHour := float64(c.config.PeakHour)
	periodHours := c.config.Period.Hours()

	// Map time to angle: peakHour → 0, peakHour+period/2 → π
	angle := 2 * math.Pi * (hourOfDay - peakHour) / periodHours

	// cos(angle) = 1 at peak, -1 at trough
	// wave = 0 at peak (fastest), 1 at trough (slowest)
	wave := (1 - math.Cos(angle)) / 2
	baseInterval := c.config.Baseline + time.Duration(float64(c.config.Amplitude)*wave)

	// Apply boosts — each boost reduces the interval
	totalBoost := 0.0
	for _, b := range c.boosts {
		totalBoost += b
	}

	if totalBoost > 0 {
		// Boost shrinks the interval: interval / (1 + totalBoost)
		// Capped so we never go below Baseline/2
		factor := 1.0 / (1.0 + totalBoost)
		baseInterval = time.Duration(float64(baseInterval) * factor)
		minInterval := c.config.Baseline / 2
		if baseInterval < minInterval {
			baseInterval = minInterval
		}
	}

	return baseInterval
}

// SetBoost sets a named boost factor. Use 0.0 to clear.
// Common boosts: "volatility", "news", "sentiment"
func (c *Cadence) SetBoost(name string, factor float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if factor <= 0 {
		delete(c.boosts, name)
	} else {
		c.boosts[name] = factor
	}
}

// Boost returns the current value of a named boost.
func (c *Cadence) Boost(name string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.boosts[name]
}

// Status returns a snapshot of the current cadence state.
type Status struct {
	Interval time.Duration      `json:"interval"`
	Phase    string             `json:"phase"`    // "high", "rising", "low", "falling"
	Boosts   map[string]float64 `json:"boosts"`
}

func (c *Cadence) Status() Status {
	interval := c.Interval()

	c.mu.RLock()
	boosts := make(map[string]float64, len(c.boosts))
	for k, v := range c.boosts {
		boosts[k] = v
	}
	c.mu.RUnlock()

	// Determine phase
	maxInterval := c.config.Baseline + c.config.Amplitude
	ratio := float64(interval) / float64(maxInterval)
	phase := "high"
	if ratio > 0.75 {
		phase = "low"
	} else if ratio > 0.5 {
		phase = "falling"
	} else if ratio > 0.25 {
		phase = "rising"
	}

	return Status{
		Interval: interval,
		Phase:    phase,
		Boosts:   boosts,
	}
}
