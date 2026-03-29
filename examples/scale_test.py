"""
Multi-minnow scaling test — one grouper solves CF, then a school of
minnows hammers the site in parallel.

Usage:
    make run-cf MINNOW_COUNT=10
    python examples/scale_test.py
"""

import json
import time
import requests
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass

CONTROLLER = "http://localhost:8180"

# Real Hapag-Lloyd MBLs to scrape
MBLS = [
    "HLCUSGN2512AVIN5",
    "HLCUSGN2512ARMO0",
    "HLCUSHA2512BFJR5",
    "HLCUALY260206574",
    "HLCUSGN2512AUSY0",
    "HLCUSGN2512BDMA7",
    "HLCUSGN2512BERD0",
    "HLCUSGN2512BHGH7",
    "HLCUSGN251168187",
    "HLCUTPE251224795",
]

HLCU_TRACK = "https://www.hapag-lloyd.com/en/online-business/track/track-by-booking-solution.html"


def shoal(method, path, **kwargs):
    return requests.request(method, f"{CONTROLLER}{path}", **kwargs).json()


def main():
    print("=== Shoal Multi-Minnow Scaling Test ===\n")

    pool = shoal("GET", "/pool/status")
    agents = shoal("GET", "/pool/agents")
    heavy = [a for a in agents if a["class"] == "heavy"]
    light = [a for a in agents if a["class"] == "light"]

    print(f"pool: {pool['total']} agents")
    print(f"  groupers: {len(heavy)}")
    print(f"  minnows:  {len(light)}")

    if not heavy:
        print("\nno grouper — run: make run-cf")
        return
    if not light:
        print("\nno minnows — run: make run-cf MINNOW_COUNT=5")
        return

    n_minnows = len(light)
    n_mbls = min(len(MBLS), n_minnows * 2)  # 2 MBLs per minnow
    targets = MBLS[:n_mbls]

    # --- Phase 1: Grouper solves CF ---
    print(f"\n--- Phase 1: Grouper busts CF on hapag-lloyd.com ---\n")

    lease = shoal("POST", "/lease", json={
        "consumer": "scale-test", "domain": "hapag-lloyd.com", "class": "heavy",
    })
    grouper = lease["agent_id"]
    print(f"  grouper: {grouper}")

    t0 = time.perf_counter()
    r = shoal("POST", "/request", json={
        "lease_id": lease["lease_id"],
        "url": HLCU_TRACK,
        "max_timeout": 60000,
    }, timeout=90)
    elapsed = time.perf_counter() - t0

    cf = [c for c in r.get("cookies", []) if c["name"] == "cf_clearance"]
    print(f"  CF solved in {elapsed:.1f}s")
    print(f"  cf_clearance: {'YES' if cf else 'NO'}")

    shoal("POST", "/release", json={"lease_id": lease["lease_id"]})
    time.sleep(1)  # let cookies propagate

    # --- Phase 2: Sequential baseline ---
    print(f"\n--- Phase 2: Sequential baseline (1 minnow, {n_mbls} MBLs) ---\n")

    lease = shoal("POST", "/lease", json={
        "consumer": "scale-seq", "domain": "hapag-lloyd.com", "class": "light",
    })
    seq_fish = lease["agent_id"]
    print(f"  minnow: {seq_fish}")

    t0 = time.perf_counter()
    seq_results = []
    for mbl in targets:
        url = f"{HLCU_TRACK}?blno={mbl}"
        t1 = time.perf_counter()
        r = shoal("POST", "/request", json={
            "lease_id": lease["lease_id"], "url": url,
        })
        dt = time.perf_counter() - t1
        ok = "Just a moment" not in r.get("html", "Just a moment")
        seq_results.append((mbl, ok, dt, len(r.get("html", ""))))
        status = "OK" if ok else "BLOCKED"
        print(f"  {mbl} -> {status:7s} | {len(r.get('html','')):>7,}b | {dt:.2f}s")

    seq_total = time.perf_counter() - t0
    seq_ok = sum(1 for _, ok, *_ in seq_results if ok)
    print(f"\n  sequential: {seq_ok}/{n_mbls} OK in {seq_total:.2f}s ({seq_total/n_mbls:.2f}s/req)")

    shoal("POST", "/release", json={"lease_id": lease["lease_id"]})

    # --- Phase 3: Parallel with N minnows ---
    n_parallel = min(n_minnows, n_mbls)
    print(f"\n--- Phase 3: Parallel ({n_parallel} minnows, {n_mbls} MBLs) ---\n")

    # Acquire leases for all minnows
    leases = []
    for i in range(n_parallel):
        l = shoal("POST", "/lease", json={
            "consumer": "scale-par", "domain": "hapag-lloyd.com", "class": "light",
        })
        leases.append(l)
        print(f"  {l['agent_id']}")

    # Distribute MBLs round-robin across leases
    assignments = [(targets[i], leases[i % len(leases)]) for i in range(n_mbls)]

    def scrape(mbl_lease):
        mbl, lease = mbl_lease
        url = f"{HLCU_TRACK}?blno={mbl}"
        t1 = time.perf_counter()
        r = shoal("POST", "/request", json={
            "lease_id": lease["lease_id"], "url": url,
        })
        dt = time.perf_counter() - t1
        ok = "Just a moment" not in r.get("html", "Just a moment")
        return mbl, lease["agent_id"], ok, dt, len(r.get("html", ""))

    print()
    t0 = time.perf_counter()
    par_results = []
    with ThreadPoolExecutor(max_workers=n_parallel) as executor:
        futures = {executor.submit(scrape, a): a for a in assignments}
        for future in as_completed(futures):
            mbl, fish, ok, dt, size = future.result()
            par_results.append((mbl, fish, ok, dt, size))
            status = "OK" if ok else "BLOCKED"
            print(f"  {fish:20s} | {mbl} -> {status:7s} | {size:>7,}b | {dt:.2f}s")

    par_total = time.perf_counter() - t0
    par_ok = sum(1 for *_, ok, _, _ in par_results if ok)

    # Release all leases
    for l in leases:
        shoal("POST", "/release", json={"lease_id": l["lease_id"]})

    # --- Summary ---
    print(f"\n--- Results ---\n")
    print(f"  MBLs scraped:     {n_mbls}")
    print(f"  Minnows used:     {n_parallel}")
    print(f"  Sequential:       {seq_total:.2f}s total ({seq_total/n_mbls:.2f}s/req) — {seq_ok}/{n_mbls} OK")
    print(f"  Parallel:         {par_total:.2f}s total ({par_total/n_mbls:.2f}s/req) — {par_ok}/{n_mbls} OK")
    if seq_total > 0:
        speedup = seq_total / par_total
        print(f"  Speedup:          {speedup:.1f}x")

    # Fish usage
    print(f"\n--- Agent Usage ---\n")
    agents = shoal("GET", "/pool/agents")
    for a in agents:
        hl = a.get("domains", {}).get("hapag-lloyd.com", {})
        if hl:
            cf = " CF!" if hl.get("has_cf_clearance") else ""
            print(f"  {a['id']:20s} ({a['class']:5s} {a['backend']:12s}) | {hl['visit_count']}v {len(hl.get('cookies',[]))}c{cf}")

    print(f"\n=== done ===")


if __name__ == "__main__":
    main()
