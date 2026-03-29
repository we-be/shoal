"""
Login test — authenticate against the test site through Shoal and verify
the session persists across navigations and lease cycles.

Usage:
    make run                       # start cluster + test site
    python examples/login_test.py  # run this test
"""

import json
import requests

CONTROLLER = "http://localhost:8180"
SITE = "http://localhost:9090"


def shoal(method, path, **kwargs):
    resp = requests.request(method, f"{CONTROLLER}{path}", **kwargs)
    resp.raise_for_status()
    return resp.json()


def main():
    print("=== Shoal Login Test ===\n")

    pool = shoal("GET", "/pool/status")
    print(f"pool: {pool['total']} agents, {pool['available']} available\n")

    # --- Phase 1: Login ---
    print("--- Phase 1: Login ---\n")

    lease = shoal("POST", "/lease", json={"consumer": "login-test", "domain": "localhost"})
    fish = lease["agent_id"]
    lid = lease["lease_id"]
    print(f"  leased {fish}")

    # Check we're not logged in
    r = shoal("POST", "/request", json={"lease_id": lid, "url": f"{SITE}/"})
    assert "not logged in" in r["html"].lower(), "expected 'not logged in' on index"
    print(f"  GET / -> not logged in")

    # Navigate to login page (establishes origin for fetch)
    r = shoal("POST", "/request", json={"lease_id": lid, "url": f"{SITE}/login"})
    assert "login-form" in r["html"], "expected login form"
    print(f"  GET /login -> form loaded")

    # Login via fetch (Lightpanda supports Fetch API, not form.submit)
    r = shoal("POST", "/request", json={
        "lease_id": lid,
        "url": f"{SITE}/login",
        "actions": [
            {
                "type": "eval",
                "value": 'fetch("/login", {method: "POST", headers: {"Content-Type": "application/x-www-form-urlencoded"}, body: "username=hunter&password=shrimp"})',
            },
            {"type": "wait", "wait_ms": 500},
        ],
    })

    session_cookie = next((c for c in r.get("cookies", []) if c["name"] == "session"), None)
    assert session_cookie, f"no session cookie! cookies={r.get('cookies', [])}"
    token = session_cookie["value"]
    print(f"  POST /login -> session={token[:24]}...")

    # Navigate to dashboard — should be authenticated
    r = shoal("POST", "/request", json={"lease_id": lid, "url": f"{SITE}/dashboard"})
    assert "Welcome back" in r["html"], f"expected dashboard, got: {r['html'][:200]}"
    print(f"  GET /dashboard -> Welcome back, hunter")

    # --- Phase 2: Session persistence within lease ---
    print("\n--- Phase 2: Session persists across pages ---\n")

    # Hit the API endpoint
    r = shoal("POST", "/request", json={"lease_id": lid, "url": f"{SITE}/api/me"})
    me = extract_json(r["html"])
    assert me["authenticated"] is True, f"expected authenticated, got: {me}"
    print(f"  GET /api/me -> {me}")

    # Navigate back to dashboard
    r = shoal("POST", "/request", json={"lease_id": lid, "url": f"{SITE}/dashboard"})
    assert "Welcome back" in r["html"]
    print(f"  GET /dashboard -> still authenticated")

    # Navigate to a completely different site and back
    r = shoal("POST", "/request", json={"lease_id": lid, "url": "https://example.com"})
    print(f"  GET example.com -> {len(r['html'])}b (detour)")

    r = shoal("POST", "/request", json={"lease_id": lid, "url": f"{SITE}/api/me"})
    me = extract_json(r["html"])
    assert me["authenticated"] is True, "lost auth after visiting another domain!"
    print(f"  GET /api/me -> still authenticated after detour")

    # --- Phase 3: Session survives release + warm re-lease ---
    print("\n--- Phase 3: Session survives release cycle ---\n")

    shoal("POST", "/release", json={"lease_id": lid})
    print(f"  released {fish}")

    # Inspect identity
    agents = shoal("GET", "/pool/agents")
    our_fish = next(a for a in agents if a["id"] == fish)
    localhost_cookies = our_fish.get("domains", {}).get("localhost", {}).get("cookies", [])
    cookie_names = [c["name"] for c in localhost_cookies]
    print(f"  {fish} identity: cookies={cookie_names}, uses={our_fish['use_count']}")

    # Re-lease same domain — should warm match
    lease2 = shoal("POST", "/lease", json={"consumer": "login-test", "domain": "localhost"})
    fish2 = lease2["agent_id"]
    lid2 = lease2["lease_id"]
    warm = "WARM MATCH" if fish == fish2 else f"COLD (got {fish2}!)"
    print(f"  re-leased: {fish2} <- {warm}")
    assert fish == fish2, f"expected warm match to {fish}, got {fish2}"

    # Verify still authenticated
    r = shoal("POST", "/request", json={"lease_id": lid2, "url": f"{SITE}/api/me"})
    me2 = extract_json(r["html"])
    assert me2["authenticated"] is True, f"session lost after re-lease! got: {me2}"
    print(f"  GET /api/me -> {me2}")

    r = shoal("POST", "/request", json={"lease_id": lid2, "url": f"{SITE}/dashboard"})
    assert "Welcome back" in r["html"]
    print(f"  GET /dashboard -> Welcome back, hunter")

    shoal("POST", "/release", json={"lease_id": lid2})

    # --- Summary ---
    print("\n--- Final identity ---\n")
    agents = shoal("GET", "/pool/agents")
    our_fish = next(a for a in agents if a["id"] == fish)
    print(f"  {fish}:")
    print(f"    backend: {our_fish['backend']}")
    print(f"    ip: {our_fish.get('ip', '?')}")
    print(f"    uses: {our_fish['use_count']}")
    for domain, state in our_fish.get("domains", {}).items():
        cookies = [c["name"] for c in state.get("cookies", [])]
        cf = " (CF clearance)" if state.get("has_cf_clearance") else ""
        print(f"    {domain}: {state['visit_count']} visits, cookies={cookies}{cf}")

    print("\n=== all tests passed ===")


def extract_json(html: str) -> dict:
    """Extract JSON from HTML (handles both raw JSON and <pre> wrapped)."""
    if "<pre>" in html:
        return json.loads(html.split("<pre>")[1].split("</pre>")[0])
    # Try parsing the whole thing (API endpoints return raw JSON in body)
    try:
        return json.loads(html.split("<body>")[1].split("</body>")[0])
    except (ValueError, IndexError):
        return json.loads(html)


if __name__ == "__main__":
    main()
