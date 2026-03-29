"""
Shoal load balancer test — scrape pages through a pool of agents
and observe identity tracking, warm matching, and session persistence.

Usage:
    make run                    # start the cluster first
    python examples/scrape.py   # run this test
"""

import json
import requests
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass


CONTROLLER = "http://localhost:8180"

TARGETS = [
    "https://example.com",
    "https://httpbin.org/html",
    "https://httpbin.org/robots.txt",
    "https://httpbin.org/json",
    "https://httpbin.org/headers",
    "https://example.org",
]


@dataclass
class ShoalClient:
    """Minimal client for the Shoal controller API."""

    base_url: str

    def pool_status(self) -> dict:
        return requests.get(f"{self.base_url}/pool/status").json()

    def health(self) -> dict:
        return requests.get(f"{self.base_url}/health").json()

    def agents(self) -> list[dict]:
        return requests.get(f"{self.base_url}/pool/agents").json()

    def lease(self, consumer: str, domain: str) -> dict:
        resp = requests.post(
            f"{self.base_url}/lease",
            json={"consumer": consumer, "domain": domain},
        )
        resp.raise_for_status()
        return resp.json()

    def navigate(self, lease_id: str, url: str, max_timeout: int = 30000) -> dict:
        resp = requests.post(
            f"{self.base_url}/request",
            json={"lease_id": lease_id, "url": url, "max_timeout": max_timeout},
        )
        resp.raise_for_status()
        return resp.json()

    def release(self, lease_id: str) -> dict:
        resp = requests.post(
            f"{self.base_url}/release",
            json={"lease_id": lease_id},
        )
        resp.raise_for_status()
        return resp.json()


def print_agents(agents: list[dict]):
    """Pretty-print agent identities."""
    for a in agents:
        domains = a.get("domains", {})
        domain_info = []
        for d, state in domains.items():
            cookies = len(state.get("cookies", []))
            cf = " CF" if state.get("has_cf_clearance") else ""
            visits = state.get("visit_count", 0)
            domain_info.append(f"{d}({visits}v,{cookies}c{cf})")
        domain_str = ", ".join(domain_info) if domain_info else "no domains"
        print(f"  {a['id']:20s} | ip={a.get('ip','?'):15s} | uses={a['use_count']:2d} | {domain_str}")


def test_sequential(client: ShoalClient):
    """Acquire a lease, scrape several pages in sequence, release."""
    print("\n--- Sequential scrape (1 lease, multiple pages) ---\n")

    lease = client.lease("sequential-test", "httpbin.org")
    lease_id = lease["lease_id"]
    agent_id = lease["agent_id"]
    print(f"  lease -> {agent_id}")

    for url in TARGETS[:3]:
        t0 = time.perf_counter()
        result = client.navigate(lease_id, url)
        elapsed = time.perf_counter() - t0
        cookies = len(result.get("cookies", []))
        print(f"  {url}")
        print(f"    -> {result['status']} | {len(result['html']):,}b | {cookies} cookies | {elapsed:.2f}s")

    client.release(lease_id)


def test_concurrent(client: ShoalClient):
    """Acquire multiple leases, scrape concurrently, show agent distribution."""
    print("\n--- Concurrent scrape (1 lease per target) ---\n")

    pool = client.pool_status()
    n_agents = pool["available"]
    print(f"  pool: {pool['total']} agents, {pool['available']} available")

    if n_agents == 0:
        print("  no agents available!")
        return

    targets = TARGETS[: min(len(TARGETS), n_agents)]
    leases = []

    for url in targets:
        domain = url.split("/")[2]
        lease = client.lease("concurrent-test", domain)
        leases.append((lease, url))
        print(f"  {lease['agent_id']:20s} <- {domain}")

    def do_request(lease_url):
        lease, url = lease_url
        t0 = time.perf_counter()
        result = client.navigate(lease["lease_id"], url)
        elapsed = time.perf_counter() - t0
        return lease, url, result, elapsed

    print()
    results = []
    with ThreadPoolExecutor(max_workers=len(leases)) as pool_exec:
        futures = {pool_exec.submit(do_request, lu): lu for lu in leases}
        for future in as_completed(futures):
            lease, url, result, elapsed = future.result()
            results.append((lease, url, result, elapsed))
            size = len(result.get("html", ""))
            print(f"  {lease['agent_id']:20s} | {url} -> {result['status']} | {size:,}b | {elapsed:.2f}s")

    for lease, _, _, _ in results:
        client.release(lease["lease_id"])


def test_auth_persistence(client: ShoalClient):
    """
    Test that login/session state persists across navigations AND leases.

    1. Lease a fish, "log in" (set session cookie)
    2. Navigate to other pages — cookie persists (same tab)
    3. Verify auth state via /cookies endpoint
    4. Release the fish
    5. Re-lease same domain — warm match gets the SAME fish
    6. Verify cookie survived the release cycle
    """
    print("\n--- Auth persistence test ---\n")

    # Step 1: lease + set session cookie
    lease = client.lease("auth-test", "httpbin.org")
    fish = lease["agent_id"]
    lid = lease["lease_id"]
    print(f"  leased {fish}")

    result = client.navigate(lid, "https://httpbin.org/cookies/set/session_id/user_hunter_authenticated")
    session_cookies = [c for c in result.get("cookies", []) if c["name"] == "session_id"]
    assert session_cookies, "cookie not set!"
    print(f"  logged in: session_id={session_cookies[0]['value']}")

    # Step 2: navigate to different pages — cookie should follow
    for url in ["https://httpbin.org/html", "https://httpbin.org/json"]:
        r = client.navigate(lid, url)
        has_session = any(c["name"] == "session_id" for c in r.get("cookies", []))
        print(f"  {url.split('/')[-1]:10s} -> session cookie present: {has_session}")
        assert has_session, f"session cookie lost on {url}!"

    # Step 3: verify via /cookies endpoint
    r = client.navigate(lid, "https://httpbin.org/cookies")
    cookie_data = json.loads(r["html"].split("<pre>")[1].split("</pre>")[0])
    print(f"  httpbin confirms: {cookie_data['cookies']}")
    assert cookie_data["cookies"].get("session_id") == "user_hunter_authenticated"

    # Step 4: release
    client.release(lid)
    print(f"  released {fish}")

    # Step 5: re-lease — should warm match to same fish
    lease2 = client.lease("auth-test", "httpbin.org")
    fish2 = lease2["agent_id"]
    lid2 = lease2["lease_id"]
    warm = "WARM MATCH" if fish == fish2 else f"COLD (got {fish2}!)"
    print(f"  re-leased: {fish2} <- {warm}")
    assert fish == fish2, f"expected warm match to {fish}, got {fish2}"

    # Step 6: verify cookies survived
    r2 = client.navigate(lid2, "https://httpbin.org/cookies")
    cookie_data2 = json.loads(r2["html"].split("<pre>")[1].split("</pre>")[0])
    survived = cookie_data2["cookies"].get("session_id") == "user_hunter_authenticated"
    print(f"  session survived release: {survived}")
    print(f"  httpbin confirms: {cookie_data2['cookies']}")
    assert survived, "session cookie lost across lease cycle!"

    client.release(lid2)
    print(f"  PASS — auth persists within lease AND across warm re-lease")


def test_exhaustion(client: ShoalClient):
    """Lease all agents, then try one more to trigger pool_exhausted."""
    print("\n--- Pool exhaustion test ---\n")

    pool = client.pool_status()
    n = pool["total"]

    leases = []
    for i in range(n):
        lease = client.lease("exhaust-test", f"domain-{i}.com")
        leases.append(lease)
        print(f"  leased {lease['agent_id']}")

    print(f"  all {n} leased, trying one more...")
    try:
        client.lease("exhaust-test", "one-too-many.com")
        print("  unexpected: got a lease?!")
    except requests.HTTPError as e:
        body = e.response.json()
        print(f"  correctly rejected: {body['error']}")

    for lease in leases:
        client.release(lease["lease_id"])


def main():
    client = ShoalClient(CONTROLLER)

    print("=== Shoal LB Test ===")
    print(f"controller: {CONTROLLER}")

    health = client.health()
    print(f"health: {health['status']} (up {health['uptime']}s)")

    pool = client.pool_status()
    print(f"pool: {pool['total']} agents, {pool['available']} available")

    if pool["total"] == 0:
        print("\nno agents registered — run `make run` first")
        return

    print("\n--- Identities ---")
    print_agents(client.agents())

    test_sequential(client)
    test_concurrent(client)
    test_auth_persistence(client)
    test_exhaustion(client)

    print("\n--- Final identities ---")
    print_agents(client.agents())

    print("\n=== all tests passed ===")


if __name__ == "__main__":
    main()
