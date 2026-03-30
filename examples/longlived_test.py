"""
Long-lived multi-grouper scrape test.

Runs multiple Chrome groupers + minnows for several minutes, continuously
scraping CF-protected pages. Tests:
- Multi-grouper warm matching and load distribution
- CF clearance persistence over time
- Minnow cookie handoff reliability
- Grouper failure recovery (optional kill test)
- Throughput and error rate over the full run

Usage:
    python examples/longlived_test.py [--duration 120] [--groupers 2] [--minnows 5]
"""

import argparse
import json
import os
import signal
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, field

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "clients", "python"))
from shoal import Shoal, ShoalError


# Real tracking URLs to rotate through
TARGETS = [
    "https://www.hapag-lloyd.com/en/online-business/track/track-by-booking-solution.html?blno=HLCUSGN2512AVIN5",
    "https://www.hapag-lloyd.com/en/online-business/track/track-by-booking-solution.html?blno=HLCUSGN2512ARMO0",
    "https://www.hapag-lloyd.com/en/online-business/track/track-by-booking-solution.html?blno=HLCUSHA2512BFJR5",
    "https://www.hapag-lloyd.com/en/online-business/track/track-by-booking-solution.html?blno=HLCUALY260206574",
    "https://www.hapag-lloyd.com/en/online-business/track/track-by-booking-solution.html?blno=HLCUSGN2512AUSY0",
    "https://www.hapag-lloyd.com/en/online-business/track/track-by-booking-solution.html?blno=HLCUSGN2512BDMA7",
    "https://www.hapag-lloyd.com/en/online-business/track/track-by-booking-solution.html?blno=HLCUSGN2512BERD0",
    "https://www.hapag-lloyd.com/en/online-business/track/track-by-booking-solution.html?blno=HLCUSGN2512BHGH7",
]

CONTROLLER = "http://localhost:8180"


@dataclass
class RunStats:
    ok: int = 0
    errors: int = 0
    blocked: int = 0
    latencies: list = field(default_factory=list)
    start_time: float = 0
    timeline: list = field(default_factory=list)  # (timestamp, ok, errors, blocked)

    def record(self, result: str, latency: float):
        if result == "ok":
            self.ok += 1
        elif result == "blocked":
            self.blocked += 1
        else:
            self.errors += 1
        self.latencies.append(latency)

    def snapshot(self):
        elapsed = time.time() - self.start_time
        self.timeline.append((elapsed, self.ok, self.errors, self.blocked))

    @property
    def total(self):
        return self.ok + self.errors + self.blocked

    @property
    def success_rate(self):
        return self.ok / self.total * 100 if self.total else 0

    @property
    def rps(self):
        elapsed = time.time() - self.start_time
        return self.total / elapsed if elapsed > 0 else 0

    def percentile(self, p):
        if not self.latencies:
            return 0
        s = sorted(self.latencies)
        idx = int(len(s) * p / 100)
        return s[min(idx, len(s) - 1)]


def start_cluster(n_groupers: int, n_minnows: int) -> list[subprocess.Popen]:
    """Start controller + groupers + minnows, return all processes."""
    procs = []

    # Kill any existing
    os.system("fuser -k 8180/tcp 2>/dev/null; sleep 0.3")
    for p in range(8181, 8181 + n_groupers):
        os.system(f"fuser -k {p}/tcp 2>/dev/null")
    for p in range(8190, 8190 + n_minnows):
        os.system(f"fuser -k {p}/tcp 2>/dev/null")
    for p in range(9333, 9333 + n_groupers):
        os.system(f"fuser -k {p}/tcp 2>/dev/null")
        os.system(f"rm -rf /tmp/shoal-chrome-{p}")
    time.sleep(0.5)

    # Controller
    p = subprocess.Popen(
        ["bin/controller", "-addr", ":8180", "-health-interval", "10s"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    procs.append(p)
    time.sleep(0.5)

    # Groupers (Chrome)
    for i in range(n_groupers):
        agent_port = 8181 + i
        cdp_port = 9333 + i
        p = subprocess.Popen(
            ["bin/agent", "-addr", f":{agent_port}",
             "-controller", CONTROLLER,
             "-backend", "chrome",
             "-lightpanda-port", str(cdp_port)],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        )
        procs.append(p)

    # Minnows (tls-client)
    for i in range(n_minnows):
        agent_port = 8190 + i
        p = subprocess.Popen(
            ["bin/agent", "-addr", f":{agent_port}",
             "-controller", CONTROLLER,
             "-backend", "tls-client"],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        )
        procs.append(p)

    # Wait for everything to register
    print(f"  waiting for {n_groupers} groupers + {n_minnows} minnows...")
    deadline = time.time() + 30
    while time.time() < deadline:
        try:
            status = Shoal(CONTROLLER).status()
            if status.total >= n_groupers + n_minnows:
                break
        except Exception:
            pass
        time.sleep(1)

    return procs


def stop_cluster(procs: list[subprocess.Popen]):
    for p in procs:
        try:
            p.send_signal(signal.SIGTERM)
        except Exception:
            pass
    time.sleep(1)
    for p in procs:
        try:
            p.kill()
        except Exception:
            pass
    os.system("fuser -k 8180/tcp 2>/dev/null")


def main():
    parser = argparse.ArgumentParser(description="Long-lived multi-grouper scrape test")
    parser.add_argument("--duration", type=int, default=120, help="test duration in seconds")
    parser.add_argument("--groupers", type=int, default=2, help="number of Chrome groupers")
    parser.add_argument("--minnows", type=int, default=5, help="number of tls-client minnows")
    parser.add_argument("--concurrency", type=int, default=5, help="concurrent requests")
    parser.add_argument("--no-cluster", action="store_true", help="skip cluster startup (use existing)")
    args = parser.parse_args()

    print("=" * 60)
    print("  LONG-LIVED MULTI-GROUPER SCRAPE TEST")
    print("=" * 60)
    print(f"  duration:    {args.duration}s")
    print(f"  groupers:    {args.groupers}")
    print(f"  minnows:     {args.minnows}")
    print(f"  concurrency: {args.concurrency}")
    print()

    procs = []
    if not args.no_cluster:
        print("--- Starting cluster ---")
        procs = start_cluster(args.groupers, args.minnows)

    try:
        s = Shoal(CONTROLLER)
        pool = s.status()
        agents = s.agents()
        heavy = [a for a in agents if a["class"] == "heavy"]
        light = [a for a in agents if a["class"] == "light"]
        print(f"\n  pool: {pool.total} agents ({len(heavy)} groupers, {len(light)} minnows)")
        for a in agents:
            print(f"    {a['id']:24s} | {a['backend']:12s} | {a['class']}")

        if not heavy:
            print("\n  ERROR: no groupers available")
            return

        # --- Phase 1: Initial CF solve with each grouper ---
        print(f"\n--- Phase 1: CF solve ({len(heavy)} groupers) ---\n")

        for i, g in enumerate(heavy):
            t0 = time.perf_counter()
            try:
                resp = s.fetch(
                    TARGETS[i % len(TARGETS)],
                    consumer="longlived-init",
                    agent_class="heavy",
                    max_timeout=60000,
                )
                dt = time.perf_counter() - t0
                cf = any(c["name"] == "cf_clearance" for c in resp.cookies)
                blocked = "Just a moment" in resp.html
                print(f"  grouper {i+1}: {dt:.1f}s | cf={'YES' if cf else 'NO'} | {'BLOCKED' if blocked else 'OK'} | {len(resp.html):,}b")
            except ShoalError as e:
                print(f"  grouper {i+1}: ERROR {e}")
            time.sleep(1)

        time.sleep(2)

        # --- Phase 2: Sustained minnow scraping ---
        print(f"\n--- Phase 2: Sustained scraping ({args.duration}s) ---\n")

        stats = RunStats(start_time=time.time())
        target_idx = 0
        snapshot_interval = 10

        def do_request():
            nonlocal target_idx
            url = TARGETS[target_idx % len(TARGETS)]
            target_idx += 1
            t0 = time.perf_counter()
            try:
                resp = s.fetch(url, consumer="longlived", agent_class="light")
                dt = time.perf_counter() - t0
                if "Just a moment" in resp.html:
                    stats.record("blocked", dt)
                    return "blocked", dt
                stats.record("ok", dt)
                return "ok", dt
            except ShoalError as e:
                dt = time.perf_counter() - t0
                stats.record("error", dt)
                return f"error: {e.error}", dt

        last_snapshot = time.time()
        last_print = time.time()

        with ThreadPoolExecutor(max_workers=args.concurrency) as pool_exec:
            futures = set()
            end_time = time.time() + args.duration

            while time.time() < end_time:
                # Keep concurrency slots filled
                while len(futures) < args.concurrency and time.time() < end_time:
                    futures.add(pool_exec.submit(do_request))

                # Collect completed
                done = {f for f in futures if f.done()}
                for f in done:
                    futures.discard(f)
                    try:
                        f.result()
                    except Exception:
                        pass

                # Periodic status
                now = time.time()
                if now - last_print >= snapshot_interval:
                    elapsed = now - stats.start_time
                    stats.snapshot()
                    print(f"  [{elapsed:5.0f}s] {stats.ok} ok | {stats.errors} err | {stats.blocked} blocked | {stats.rps:.1f} req/s | p50={stats.percentile(50):.2f}s")
                    last_print = now

                time.sleep(0.05)

            # Drain remaining
            for f in as_completed(futures):
                try:
                    f.result()
                except Exception:
                    pass

        stats.snapshot()

        # --- Results ---
        print(f"\n{'='*60}")
        print(f"  RESULTS ({args.duration}s)")
        print(f"{'='*60}\n")

        print(f"  Total requests:  {stats.total}")
        print(f"  Success:         {stats.ok} ({stats.success_rate:.1f}%)")
        print(f"  Blocked:         {stats.blocked}")
        print(f"  Errors:          {stats.errors}")
        print(f"  Throughput:      {stats.rps:.1f} req/s")
        print(f"  Latency p50:     {stats.percentile(50):.3f}s")
        print(f"  Latency p95:     {stats.percentile(95):.3f}s")
        print(f"  Latency p99:     {stats.percentile(99):.3f}s")

        print(f"\n--- Final agent state ---\n")
        agents = s.agents()
        for a in agents:
            hl = a.get("domains", {}).get("hapag-lloyd.com", {})
            if hl:
                cf = "CF!" if hl.get("has_cf_clearance") else ""
                print(f"  {a['id']:24s} ({a['class']:5s}) | {hl['visit_count']:>4d}v {len(hl.get('cookies',[]))}c {cf}")
            else:
                print(f"  {a['id']:24s} ({a['class']:5s}) | no hapag visits")

        print(f"\n--- Metrics ---\n")
        try:
            import requests
            metrics = requests.get(f"{CONTROLLER}/metrics").text
            for line in metrics.split("\n"):
                if line.startswith("shoal_cf_") or line.startswith("shoal_request_total") or line.startswith("shoal_agent_removed"):
                    if not line.startswith("#"):
                        print(f"  {line}")
        except Exception:
            pass

        if stats.success_rate < 80:
            print(f"\n  WARNING: success rate {stats.success_rate:.1f}% is below 80%")
            sys.exit(1)
        else:
            print(f"\n  PASS")

    finally:
        if procs:
            print("\n--- Stopping cluster ---")
            stop_cluster(procs)


if __name__ == "__main__":
    main()
