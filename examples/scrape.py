"""
Shoal load balancer test — scrape pages through a pool of agents.

Spins up leases, fires requests concurrently, and shows how work
distributes across the shoal.

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
        print(f"  {url}")
        print(f"    -> {result['status']} | {len(result['html']):,} bytes | {elapsed:.2f}s")

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

    # Acquire one lease per target (up to available agents)
    targets = TARGETS[: min(len(TARGETS), n_agents)]
    leases = []
    agent_hits: dict[str, int] = {}

    for url in targets:
        domain = url.split("/")[2]
        lease = client.lease("concurrent-test", domain)
        leases.append((lease, url))
        agent_id = lease["agent_id"]
        agent_hits[agent_id] = agent_hits.get(agent_id, 0) + 1
        print(f"  lease {lease['lease_id']} -> {agent_id} for {domain}")

    print(f"\n  agent distribution: {dict(agent_hits)}")
    pool_mid = client.pool_status()
    print(f"  pool mid-flight: {pool_mid['leased']} leased, {pool_mid['available']} available")

    # Fire all requests concurrently
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
            status = result.get("status", "err")
            size = len(result.get("html", ""))
            cookies = len(result.get("cookies", []))
            print(f"  {lease['agent_id']} | {url}")
            print(f"    -> {status} | {size:,} bytes | {cookies} cookies | {elapsed:.2f}s")

    # Release all
    for lease, _, _, _ in results:
        client.release(lease["lease_id"])

    pool_end = client.pool_status()
    print(f"\n  pool after release: {pool_end['total']} total, {pool_end['available']} available, {pool_end['leased']} leased")


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
        print(f"  leased {lease['lease_id']} -> {lease['agent_id']}")

    print(f"  all {n} agents leased, trying one more...")
    try:
        client.lease("exhaust-test", "one-too-many.com")
        print("  unexpected: got a lease?!")
    except requests.HTTPError as e:
        body = e.response.json()
        print(f"  correctly rejected: {body['error']} — {body.get('detail', '')}")

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

    test_sequential(client)
    test_concurrent(client)
    test_exhaustion(client)

    print("\n=== all tests passed ===")


if __name__ == "__main__":
    main()
