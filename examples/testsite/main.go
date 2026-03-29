// A tiny auth-gated test site for exercising Shoal's session persistence.
// Login with hunter/shrimp, get a session cookie, access protected pages.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

var (
	sessions   = map[string]string{} // token -> username
	sessionsMu sync.RWMutex
)

func main() {
	addr := flag.String("addr", ":9090", "listen address")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", handleIndex)
	mux.HandleFunc("GET /login", handleLoginPage)
	mux.HandleFunc("POST /login", handleLogin)
	mux.HandleFunc("GET /dashboard", handleDashboard)
	mux.HandleFunc("GET /api/me", handleMe)
	mux.HandleFunc("GET /logout", handleLogout)

	log.Printf("test site listening on %s (login: hunter / shrimp)", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	if user != "" {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>Shoal Test Site</title></head>
<body>
<h1>Welcome to the Dock</h1>
<p>You are not logged in.</p>
<a href="/login">Login</a>
</body></html>`)
}

func handleLoginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>Login</title></head>
<body>
<h1>Login</h1>
<form method="POST" action="/login" id="login-form">
  <label for="username">Username:</label>
  <input type="text" name="username" id="username" />
  <label for="password">Password:</label>
  <input type="password" name="password" id="password" />
  <button type="submit" id="submit-btn">Login</button>
</form>
</body></html>`)
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	user := r.FormValue("username")
	pass := r.FormValue("password")

	if user == "hunter" && pass == "shrimp" {
		token := fmt.Sprintf("tok_%d", time.Now().UnixNano())
		sessionsMu.Lock()
		sessions[token] = user
		sessionsMu.Unlock()

		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			MaxAge:   3600,
		})
		log.Printf("login success: %s -> %s", user, token)
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}

	w.WriteHeader(http.StatusUnauthorized)
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>Login Failed</title></head>
<body>
<h1>Login Failed</h1>
<p>Bad credentials. Try again.</p>
<a href="/login">Back to login</a>
</body></html>`)
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	if user == "" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><title>Dashboard</title></head>
<body>
<h1>Welcome back, %s</h1>
<p>You're in. The tide is high.</p>
<p>Session data:</p>
<ul>
  <li>Username: <span id="username">%s</span></li>
  <li>Authenticated: <span id="authenticated">true</span></li>
</ul>
<a href="/api/me">API: /api/me</a> |
<a href="/logout">Logout</a>
</body></html>`, user, user)
}

func handleMe(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	if user == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"authenticated": false,
			"error":         "not logged in",
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"authenticated": true,
		"username":      user,
	})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("session"); err == nil {
		sessionsMu.Lock()
		delete(sessions, c.Value)
		sessionsMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:   "session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func getUser(r *http.Request) string {
	c, err := r.Cookie("session")
	if err != nil {
		return ""
	}
	sessionsMu.RLock()
	defer sessionsMu.RUnlock()
	return sessions[c.Value]
}
