"""
Lowcountry tides through a school of fish.

Pulls high/low tide predictions for Charleston-area NOAA stations
through Shoal. Because what else would a shoal do?

Usage:
    make run               # lightpanda or tls-client cluster
    python examples/tides.py
"""

import requests
import sys
from datetime import datetime, timedelta

CONTROLLER = "http://localhost:8180"

# Lowcountry NOAA tide stations
STATIONS = {
    "8665530": "Charleston Harbor, SC",
    "8661070": "Myrtle Beach, SC",
    "8670870": "Fort Pulaski, GA",
    "8662245": "Oyster Landing, SC",
    "8665494": "Citadel Beach, SC",
}

NOAA_API = "https://api.tidesandcurrents.noaa.gov/api/prod/datagetter"


def shoal_fetch(url):
    """One-shot fetch through Shoal."""
    resp = requests.post(
        f"{CONTROLLER}/fetch",
        json={"url": url, "consumer": "tides"},
        timeout=30,
    )
    resp.raise_for_status()
    return resp.json()


def get_tides(station_id, days=2):
    """Pull tide predictions for a station via Shoal."""
    begin = datetime.now().strftime("%Y%m%d")
    end = (datetime.now() + timedelta(days=days)).strftime("%Y%m%d")

    url = (
        f"{NOAA_API}?station={station_id}"
        f"&product=predictions&datum=MLLW&time_zone=lst_ldt"
        f"&units=english&interval=hilo&format=json"
        f"&begin_date={begin}&end_date={end}"
    )

    result = shoal_fetch(url)
    html = result.get("html", "")

    # NOAA API returns JSON wrapped in Shoal's HTML response
    import json
    try:
        data = json.loads(html)
    except json.JSONDecodeError:
        return None

    return data.get("predictions", [])


def format_tide(pred):
    """Format a tide prediction."""
    t = pred["t"]  # "2026-03-29 06:12"
    v = float(pred["v"])  # height in feet
    typ = pred.get("type", "?")
    arrow = "\u2191" if typ == "H" else "\u2193"  # up/down arrow
    label = "HIGH" if typ == "H" else "LOW "
    return f"  {arrow} {label}  {t}  {v:+.1f} ft"


def main():
    stations = sys.argv[1:] if len(sys.argv) > 1 else list(STATIONS.keys())[:3]

    # Check Shoal is running
    try:
        pool = requests.get(f"{CONTROLLER}/pool/status").json()
    except requests.ConnectionError:
        print("shoal not running — start with: make run")
        return

    print(f"pool: {pool['total']} agents\n")
    print("--- lowcountry tides ---\n")

    for sid in stations:
        name = STATIONS.get(sid, f"Station {sid}")
        tides = get_tides(sid)

        if not tides:
            print(f"{name}: no data")
            continue

        print(f"{name} ({sid})")
        for pred in tides:
            print(format_tide(pred))
        print()


if __name__ == "__main__":
    main()
