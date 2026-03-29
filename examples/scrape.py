"""
Shoal load balancer test — scrape pages through a pool of agents
and observe identity tracking + warm matching.

Usage:
    make run                    # start the cluster first
    python examples/scrape.py   # run this test
"""

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
    print(f"  lease {lease_id} -> {agent_id}")

    for url in TARGETS[:3]:
        t0 = time.perf_counter()
        result = client.navigate(lease_id, url)
        elapsed = time.perf_counter() - t0
        cookies = len(result.get("cookies", []))
        print(f"  {url}")
        print(f"    -> {result['status']} | {len(result['html']):,}b | {cookies} cookies | {elapsed:.2f}s")

    client.release(lease_id)
    print(f"  released {lease_id}")


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
    agent_hits: dict[str, int] = {}

    for url in targets:
        domain = url.split("/")[2]
        lease = client.lease("concurrent-test", domain)
        leases.append((lease, url))
        agent_id = lease["agent_id"]
        agent_hits[agent_id] = agent_hits.get(agent_id, 0) + 1
        print(f"  lease {lease['lease_id'][:16]:16s} -> {agent_id} for {domain}")

    print(f"\n  agent distribution: {dict(agent_hits)}")

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
            cookies = len(result.get("cookies", []))
            print(f"  {lease['agent_id']:20s} | {url}")
            print(f"    -> {result['status']} | {size:,}b | {cookies} cookies | {elapsed:.2f}s")

    for lease, _, _, _ in results:
        client.release(lease["lease_id"])


def test_warm_matching(client: ShoalClient):
    """
    Test that the controller prefers warm agents for repeat domains.

    1. Scrape httpbin.org with one agent (builds up cookies/domain state)
    2. Release that agent
    3. Request httpbin.org again — should get the SAME fish back (warm match)
    """
    print("\n--- Warm matching test ---\n")

    # Step 1: first visit — any agent
    lease1 = client.lease("warm-test", "httpbin.org")
    fish1 = lease1["agent_id"]
    print(f"  first visit: {fish1}")

    result = client.navigate(lease1["lease_id"], "https://httpbin.org/cookies/set/session_id/abc123")
    cookies1 = len(result.get("cookies", []))
    print(f"    set cookie -> {cookies1} cookies tracked")

    client.release(lease1["lease_id"])
    print(f"  released {fish1}")

    # Step 2: second visit — should get the same warm fish
    lease2 = client.lease("warm-test", "httpbin.org")
    fish2 = lease2["agent_id"]
    warm = "WARM MATCH" if fish1 == fish2 else "COLD (different agent)"
    print(f"  second visit: {fish2} <- {warm}")

    client.release(lease2["lease_id"])


def test_exhaustion(client: ShoalClient):
    """Lease all agents, then try one more to trigger pool_exhausted."""
    print("\n--- Pool exhaustion test ---\n")

    pool = client.pool_status()
    n = pool["total"]
    print(f"  pool has {n} agents")

    leases = []
    for i in range(n):
        lease = client.lease("exhaust-test", f"domain-{i}.com")
        leases.append(lease)
        print(f"  leased {lease['lease_id'][:16]:16s} -> {lease['agent_id']}")

    print(f"  all {n} agents leased, trying one more...")
    try:
        client.lease("exhaust-test", "one-too-many.com")
        print("  unexpected: got a lease?!")
    except requests.HTTPError as e:
        body = e.response.json()
        print(f"  correctly rejected: {body['error']}")

    for lease in leases:
        client.release(lease["lease_id"])
    print(f"  released all {n} leases")


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

    # Show initial identities
    print("\n--- Initial identities ---")
    print_agents(client.agents())

    test_sequential(client)
    test_concurrent(client)
    test_warm_matching(client)
    test_exhaustion(client)

    # Show final identities — should see accumulated domain state
    print("\n--- Final identities (accumulated state) ---")
    print_agents(client.agents())

    print("\n=== all tests passed ===")


if __name__ == "__main__":
    main()
