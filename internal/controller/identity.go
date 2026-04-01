package controller

import (
	"crypto/rand"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/we-be/shoal/internal/api"
)

// Lowcountry fish — each browser gets a name from these waters.
var fish = []string{
	"redfish", "mullet", "flounder", "sheepshead", "drum",
	"trout", "tarpon", "snapper", "grouper", "wahoo",
	"cobia", "mahi", "pompano", "spot", "croaker",
	"whiting", "shad", "pinfish", "pigfish", "minnow",
}

// newFishID generates a unique browser identity like "redfish-a3b2c5d6".
func newFishID() string {
	b := make([]byte, 4)
	rand.Read(b)
	name := fish[b[0]%byte(len(fish))]
	return fmt.Sprintf("%s-%x", name, b)
}

// newIdentity creates a fresh browser identity.
func newIdentity(backend, class, ip string) *api.BrowserIdentity {
	now := time.Now()
	return &api.BrowserIdentity{
		ID:        newFishID(),
		IP:        ip,
		Backend:   backend,
		Class:     class,
		CreatedAt: now,
		LastUsed:  now,
		UseCount:  0,
		Domains:   make(map[string]*api.DomainState),
	}
}

// extractDomain pulls the registrable domain from a URL.
// "https://www.example.com/foo" -> "example.com"
func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	host := u.Hostname()

	// Strip www. prefix
	host = strings.TrimPrefix(host, "www.")
	return host
}

// updateIdentity records what a browser learned from a navigation.
// Tracks cookies per domain, flags CF clearance, updates visit counts.
func updateIdentity(identity *api.BrowserIdentity, navURL string, cookies []api.Cookie) {
	domain := extractDomain(navURL)
	now := time.Now()

	identity.LastUsed = now
	identity.UseCount++

	state, ok := identity.Domains[domain]
	if !ok {
		state = &api.DomainState{
			Cookies: []api.Cookie{},
			Tokens:  make(map[string]string),
		}
		identity.Domains[domain] = state
	}

	state.LastVisited = now
	state.VisitCount++

	// Merge cookies — update existing, add new
	state.Cookies = mergeCookies(state.Cookies, cookies, domain)

	// Check for CF clearance
	state.HasCFClearance = false
	state.CFExpiry = nil
	for _, c := range state.Cookies {
		if c.Name == api.CookieCFClearance {
			state.HasCFClearance = true
			state.CFURL = navURL // remember which URL earned it
			if c.Expires > 0 {
				exp := time.Unix(int64(c.Expires), 0)
				state.CFExpiry = &exp
			}
			break
		}
	}
}

// mergeCookies updates tracked cookies with new ones from the response.
// Matches by name+domain, adds new ones, preserves existing ones not in the update.
func mergeCookies(existing, incoming []api.Cookie, navDomain string) []api.Cookie {
	byKey := make(map[string]api.Cookie)

	// Index existing
	for _, c := range existing {
		key := cookieKey(c)
		byKey[key] = c
	}

	// Upsert incoming
	for _, c := range incoming {
		// If cookie has no domain, assign the navigation domain
		if c.Domain == "" {
			c.Domain = navDomain
		}
		key := cookieKey(c)
		byKey[key] = c
	}

	// Collect and prune expired
	now := float64(time.Now().Unix())
	result := make([]api.Cookie, 0, len(byKey))
	for _, c := range byKey {
		if c.Expires > 0 && c.Expires < now {
			continue // expired, drop it
		}
		result = append(result, c)
	}

	return result
}

func cookieKey(c api.Cookie) string {
	return c.Name + "@" + c.Domain + c.Path
}

// domainWarmth scores how warm a browser is for a given domain.
// Higher = better match.
//
//	3 = has valid cf_clearance (golden — skip the challenge)
//	2 = has cookies for this domain (knows the waters)
//	1 = has visited before (been in the neighborhood)
//	0 = cold (never been here)
func domainWarmth(identity *api.BrowserIdentity, domain string) int {
	state, ok := identity.Domains[domain]
	if !ok {
		return 0
	}

	if state.HasCFClearance {
		// Check if CF clearance is still valid
		if state.CFExpiry == nil || state.CFExpiry.After(time.Now()) {
			return 3
		}
	}

	if len(state.Cookies) > 0 {
		return 2
	}

	if state.VisitCount > 0 {
		return 1
	}

	return 0
}
