"""
Hapag-Lloyd tracking test — realistic CF bypass + scraping through Shoal.

Uses the grouper (Chrome) to bust through Cloudflare on hapag-lloyd.com,
then hands off cookies to the minnow (tls-client) for fast bulk requests.

Usage:
    make run-cf    # start grouper + minnow
    python examples/hlcu_test.py [MBL_NUMBER]
"""

import json
import sys
import time
import requests
from bs4 import BeautifulSoup

CONTROLLER = "http://localhost:8180"
HLCU_TRACK = "https://www.hapag-lloyd.com/en/online-business/track/track-by-booking-solution.html"


def shoal(method, path, **kwargs):
    resp = requests.request(method, f"{CONTROLLER}{path}", **kwargs)
    resp.raise_for_status()
    return resp.json()


def main():
    mbl = sys.argv[1] if len(sys.argv) > 1 else None

    print("=== Hapag-Lloyd Tracking via Shoal ===\n")

    pool = shoal("GET", "/pool/status")
    print(f"pool: {pool['total']} agents, {pool['available']} available")

    agents = shoal("GET", "/pool/agents")
    heavy = [a for a in agents if a["class"] == "heavy"]
    light = [a for a in agents if a["class"] == "light"]
    print(f"  groupers: {len(heavy)} ({', '.join(a['backend'] for a in heavy)})")
    print(f"  minnows:  {len(light)} ({', '.join(a['backend'] for a in light)})")

    if not heavy:
        print("\nno grouper available — need a Chrome agent")
        return

    # --- Phase 1: Grouper busts through CF ---
    print("\n--- Phase 1: Grouper vs Hapag-Lloyd Cloudflare ---\n")

    lease = shoal("POST", "/lease", json={
        "consumer": "hlcu-test",
        "domain": "hapag-lloyd.com",
        "class": "heavy",
    })
    grouper = lease["agent_id"]
    glid = lease["lease_id"]
    print(f"  grouper: {grouper}")

    url = HLCU_TRACK
    if mbl:
        url += f"?blno={mbl}"

    print(f"  navigating to: {url}")
    print(f"  (waiting up to 60s for CF challenge...)")

    t0 = time.perf_counter()
    r = shoal("POST", "/request", json={
        "lease_id": glid,
        "url": url,
        "max_timeout": 60000,
    }, timeout=90)
    elapsed = time.perf_counter() - t0

    cookies = r.get("cookies", [])
    cf = [c for c in cookies if c["name"] == "cf_clearance"]
    cookie_names = [c["name"] for c in cookies]

    print(f"  elapsed: {elapsed:.1f}s")
    print(f"  status: {r['status']}")
    print(f"  html: {len(r['html']):,} bytes")
    print(f"  cookies: {cookie_names}")

    if cf:
        print(f"  CF CLEARANCE: {cf[0]['value'][:30]}...")
    else:
        print(f"  no cf_clearance (site may not have challenged us)")

    html = r["html"]
    if "Just a moment" in html:
        print(f"  BLOCKED — still on challenge page")
        shoal("POST", "/release", json={"lease_id": glid})
        return

    # Quick parse to see what we got
    soup = BeautifulSoup(html, "html.parser")
    title = soup.title.string if soup.title else "?"
    print(f"  page title: {title}")

    # Look for tracking data
    tracking_table = soup.find("table", id=lambda x: x and "tracing_by_booking" in str(x))
    if tracking_table:
        rows = tracking_table.find_all("tr")
        print(f"  tracking table: {len(rows)} rows")
    elif mbl:
        print(f"  no tracking table found (MBL may not exist or page structure changed)")

    shoal("POST", "/release", json={"lease_id": glid})
    print(f"  released grouper")

    # --- Phase 2: Minnow with handed-off cookies ---
    if not light:
        print("\n  no minnows available — skipping handoff test")
        return

    print("\n--- Phase 2: Minnow with CF cookies ---\n")

    # Give controller a moment to propagate cookies
    time.sleep(1)

    lease2 = shoal("POST", "/lease", json={
        "consumer": "hlcu-test",
        "domain": "hapag-lloyd.com",
        "class": "light",
    })
    minnow = lease2["agent_id"]
    mlid = lease2["lease_id"]
    print(f"  minnow: {minnow}")

    # Hit the same page with the minnow
    minnow_url = HLCU_TRACK
    if mbl:
        minnow_url += f"?blno={mbl}"

    t0 = time.perf_counter()
    r2 = shoal("POST", "/request", json={
        "lease_id": mlid,
        "url": minnow_url,
    })
    elapsed2 = time.perf_counter() - t0

    print(f"  elapsed: {elapsed2:.1f}s")
    print(f"  status: {r2['status']}")
    print(f"  html: {len(r2['html']):,} bytes")

    html2 = r2["html"]
    if "Just a moment" in html2:
        print(f"  BLOCKED — minnow couldn't get through")
    else:
        soup2 = BeautifulSoup(html2, "html.parser")
        title2 = soup2.title.string if soup2.title else "?"
        print(f"  page title: {title2}")
        print(f"  MINNOW GOT THROUGH!")

        if mbl:
            tracking_table2 = soup2.find("table", id=lambda x: x and "tracing_by_booking" in str(x))
            if tracking_table2:
                rows2 = tracking_table2.find_all("tr")
                print(f"  tracking table: {len(rows2)} rows")

    shoal("POST", "/release", json={"lease_id": mlid})

    # --- Summary ---
    print("\n--- Agent Identities ---\n")
    agents = shoal("GET", "/pool/agents")
    for a in agents:
        hl = a.get("domains", {}).get("hapag-lloyd.com", {})
        if hl:
            cookies = [c["name"] for c in hl.get("cookies", [])]
            cf_flag = " CF!" if hl.get("has_cf_clearance") else ""
            print(f"  {a['id']:20s} ({a['class']:5s}) | hapag: {hl['visit_count']}v, {len(cookies)}c{cf_flag}")
        else:
            print(f"  {a['id']:20s} ({a['class']:5s}) | no hapag visits")

    print("\n=== done ===")


if __name__ == "__main__":
    main()
