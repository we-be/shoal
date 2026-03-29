package controller

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/we-be/shoal/internal/api"
)

// poolSnapshot is the serialized pool state for persistence.
type poolSnapshot struct {
	Agents   map[string]*ManagedAgent `json:"agents"`
	SavedAt  time.Time               `json:"saved_at"`
}

// Store handles pool state persistence — periodic snapshots to a JSON file.
// No database, no dependencies. The school remembers where it's been.
type Store struct {
	pool     *Pool
	path     string
	interval time.Duration
	mu       sync.Mutex
	stopCh   chan struct{}
}

func NewStore(pool *Pool, path string, interval time.Duration) *Store {
	return &Store{
		pool:     pool,
		path:     path,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Load restores pool state from disk. Call before starting the server.
func (s *Store) Load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("store: no snapshot at %s, starting fresh", s.path)
			return nil
		}
		return err
	}

	var snap poolSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		log.Printf("store: corrupt snapshot at %s, starting fresh: %v", s.path, err)
		return nil
	}

	s.pool.mu.Lock()
	defer s.pool.mu.Unlock()

	loaded := 0
	for id, agent := range snap.Agents {
		// Restore identities but mark all as disconnected (agents must re-register)
		agent.State = api.StateAvailable
		s.pool.agents[id] = agent
		loaded++
	}

	// Don't restore leases — they're transient and agents need to reconnect
	log.Printf("store: loaded %d identities from %s (saved %s ago)",
		loaded, s.path, time.Since(snap.SavedAt).Round(time.Second))

	s.pool.updateGauges()
	return nil
}

// Start begins periodic snapshotting.
func (s *Store) Start() {
	go s.loop()
	log.Printf("store: snapshotting every %s to %s", s.interval, s.path)
}

func (s *Store) Stop() {
	close(s.stopCh)
	s.save() // final save on shutdown
}

func (s *Store) loop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.save()
		}
	}
}

func (s *Store) save() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pool.mu.RLock()
	snap := poolSnapshot{
		Agents:  s.pool.agents,
		SavedAt: time.Now(),
	}
	s.pool.mu.RUnlock()

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		log.Printf("store: marshal error: %v", err)
		return
	}

	// Write atomically: temp file + rename
	dir := filepath.Dir(s.path)
	os.MkdirAll(dir, 0o755)

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		log.Printf("store: write error: %v", err)
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		log.Printf("store: rename error: %v", err)
		return
	}
}
