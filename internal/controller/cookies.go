package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/we-be/shoal/internal/api"
)

// propagateCookiesToMinnows pushes cookies from a grouper to all light agents.
// Retries failed handoffs to handle minnows that haven't finished starting.
func (s *Server) propagateCookiesToMinnows(navURL string, cookies []api.Cookie) {
	minnows := s.pool.LightAgents()
	if len(minnows) == 0 {
		return
	}

	setCookiesReq := api.SetCookiesRequest{
		URL:     navURL,
		Cookies: cookies,
	}
	body, err := json.Marshal(setCookiesReq)
	if err != nil {
		log.Printf("failed to marshal cookies for minnow handoff: %v", err)
		return
	}

	for _, m := range minnows {
		go func(addr string, id string) {
			for attempt := range 3 {
				url := fmt.Sprintf("http://%s/cookies/set", addr)
				resp, err := s.client.Post(url, "application/json", bytes.NewReader(body))
				if err != nil {
					log.Printf("cookie handoff to %s attempt %d failed: %v", id, attempt+1, err)
					time.Sleep(time.Duration(attempt+1) * time.Second)
					continue
				}
				resp.Body.Close()
				cfHandoffsTotal.Inc()
				log.Printf("cookie handoff to %s: %d cookies for %s", id, len(cookies), navURL)
				return
			}
			log.Printf("cookie handoff to %s FAILED after 3 attempts", id)
		}(m.Address, m.Identity.ID)
	}
}

// ensureMinnowCookies checks if a minnow being leased has cookies for
// the requested domain. If not, copies them from a warm agent (grouper
// or another minnow that has them). Lazy catch-up for minnows that
// missed the initial handoff.
func (s *Server) ensureMinnowCookies(agent *ManagedAgent, domain string) {
	if agent.Class != api.ClassLight {
		return
	}

	// All reads under one lock to avoid racing with RecordNavigation
	s.pool.mu.RLock()
	if state, ok := agent.Identity.Domains[domain]; ok && len(state.Cookies) > 0 {
		s.pool.mu.RUnlock()
		return
	}

	var sourceCookies []api.Cookie
	var sourceURL string
	for _, other := range s.pool.agents {
		if state, ok := other.Identity.Domains[domain]; ok && len(state.Cookies) > 0 {
			sourceCookies = make([]api.Cookie, len(state.Cookies))
			copy(sourceCookies, state.Cookies)
			sourceURL = state.CFURL
			if sourceURL == "" {
				sourceURL = "https://www." + domain + "/"
			}
			break
		}
	}
	s.pool.mu.RUnlock()

	if len(sourceCookies) == 0 {
		return
	}

	setCookiesReq := api.SetCookiesRequest{
		URL:     sourceURL,
		Cookies: sourceCookies,
	}
	body, err := json.Marshal(setCookiesReq)
	if err != nil {
		log.Printf("lazy cookie push marshal failed: %v", err)
		return
	}
	url := fmt.Sprintf("http://%s/cookies/set", agent.Address)
	resp, err := s.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("lazy cookie push to %s failed: %v", agent.Identity.ID, err)
		return
	}
	resp.Body.Close()
	log.Printf("lazy cookie push to %s: %d cookies for %s", agent.Identity.ID, len(sourceCookies), domain)
}

// hasCFClearance checks if a cookie set contains cf_clearance.
func hasCFClearance(cookies []api.Cookie) bool {
	for _, c := range cookies {
		if c.Name == api.CookieCFClearance {
			return true
		}
	}
	return false
}
