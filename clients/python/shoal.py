"""
Shoal Python client — talk to a Shoal controller.

    from shoal import Shoal

    s = Shoal("http://localhost:8180")

    # One-liner
    resp = s.fetch("https://example.com")
    print(resp.html)

    # With lease control
    with s.session("my-scraper", "example.com") as session:
        resp = session.get("https://example.com/page1")
        resp = session.get("https://example.com/page2", actions=[
            {"type": "click", "selector": "#load-more"}
        ])

    # XHR capture
    resp = s.fetch("https://site.com/tracking?id=123",
                    capture_xhr=True, xhr_filter="/api/events")
    for xhr in resp.xhr_responses:
        data = json.loads(xhr["body"])
"""

from __future__ import annotations

import json
from contextlib import contextmanager
from dataclasses import dataclass, field
from typing import Any, Optional

import requests


@dataclass
class ShoalResponse:
    """Response from a Shoal navigation."""
    url: str = ""
    status: int = 0
    html: str = ""
    content_size: int = 0
    title: str = ""
    redirected: bool = False
    quality: str = ""           # "good", "partial", "blocked", "empty"
    quality_hints: list = field(default_factory=list)
    block_system: str = ""     # "cloudflare", "akamai", "datadome", etc.
    block_suggest: str = ""    # "retry_heavy", "retry_proxy", "wait", "skip"
    cookies: list[dict] = field(default_factory=list)
    headers: dict[str, str] = field(default_factory=dict)
    user_agent: str = ""
    xhr_responses: list[dict] = field(default_factory=list)

    @classmethod
    def from_dict(cls, d: dict) -> ShoalResponse:
        return cls(
            url=d.get("url", ""),
            status=d.get("status", 0),
            html=d.get("html", ""),
            content_size=d.get("content_size", 0),
            title=d.get("title", ""),
            redirected=d.get("redirected", False),
            quality=d.get("quality", ""),
            quality_hints=d.get("quality_hints", []),
            block_system=d.get("block_system", ""),
            block_suggest=d.get("block_suggest", ""),
            cookies=d.get("cookies", []),
            headers=d.get("headers", {}),
            user_agent=d.get("user_agent", ""),
            xhr_responses=d.get("xhr_responses", []),
        )

    def cookies_dict(self) -> dict[str, str]:
        """Cookies as a simple name→value dict."""
        return {c["name"]: c["value"] for c in self.cookies}

    def json(self) -> Any:
        """Parse HTML as JSON (for API responses)."""
        return json.loads(self.html)


@dataclass
class PoolStatus:
    total: int = 0
    available: int = 0
    leased: int = 0


class ShoalError(Exception):
    """Error from the Shoal controller."""
    def __init__(self, error: str, detail: str = ""):
        self.error = error
        self.detail = detail
        super().__init__(f"{error}: {detail}" if detail else error)


class Session:
    """A leased session — holds a single agent for multiple requests."""

    def __init__(self, client: Shoal, lease_id: str, agent_id: str):
        self.client = client
        self.lease_id = lease_id
        self.agent_id = agent_id

    def get(
        self,
        url: str = "",
        actions: list[dict] | None = None,
        max_timeout: int = 0,
        capture_xhr: bool = False,
        xhr_filter: str = "",
    ) -> ShoalResponse:
        """Navigate (or run actions on current page if url is empty)."""
        return self.client._request(
            self.lease_id, url, actions, max_timeout, capture_xhr, xhr_filter,
        )

    def release(self):
        """Release the lease."""
        self.client.release(self.lease_id)


class Shoal:
    """Client for the Shoal controller API."""

    def __init__(self, base_url: str = "http://localhost:8180", timeout: int = 120):
        self.base_url = base_url.rstrip("/")
        self.http = requests.Session()
        self.http.timeout = timeout

    # --- Simple API ---

    def fetch(
        self,
        url: str,
        consumer: str = "python-client",
        agent_class: str = "",
        actions: list[dict] | None = None,
        max_timeout: int = 0,
        capture_xhr: bool = False,
        xhr_filter: str = "",
        output_format: str = "",
        auto_retry: bool = True,
    ) -> ShoalResponse:
        """One-shot: fetch a URL through the pool (auto lease/release).

        When auto_retry is True (default), blocked responses are automatically
        retried with a heavier agent class: light → medium → heavy.
        """
        payload: dict[str, Any] = {"url": url, "consumer": consumer}
        if agent_class:
            payload["class"] = agent_class
        if actions:
            payload["actions"] = actions
        if max_timeout:
            payload["max_timeout"] = max_timeout
        if capture_xhr:
            payload["capture_xhr"] = True
        if xhr_filter:
            payload["capture_xhr_filter"] = xhr_filter
        if output_format:
            payload["output_format"] = output_format

        data = self._post("/fetch", payload)
        resp = ShoalResponse.from_dict(data)

        # Auto-retry based on remora's block_suggest
        if auto_retry and resp.quality == "blocked" and resp.block_suggest:
            if resp.block_suggest == "retry_heavy":
                upgrade = _next_class(agent_class or "light")
                if upgrade:
                    payload["class"] = upgrade
                    data = self._post("/fetch", payload)
                    resp = ShoalResponse.from_dict(data)
            # "retry_proxy" and "wait" can't be handled client-side —
            # need different proxy or backoff, which the controller manages

        return resp

    # --- Lease API ---

    def lease(self, consumer: str, domain: str, agent_class: str = "") -> Session:
        """Acquire a lease and return a Session."""
        payload: dict[str, Any] = {"consumer": consumer, "domain": domain}
        if agent_class:
            payload["class"] = agent_class
        data = self._post("/lease", payload)
        return Session(self, data["lease_id"], data["agent_id"])

    def release(self, lease_id: str):
        """Release a lease."""
        self._post("/release", {"lease_id": lease_id})

    @contextmanager
    def session(self, consumer: str, domain: str, agent_class: str = ""):
        """Context manager for a leased session — auto-releases on exit."""
        s = self.lease(consumer, domain, agent_class)
        try:
            yield s
        finally:
            s.release()

    # --- Tides ---

    def tides(self) -> dict:
        """Get current scraping cadence: interval, phase, boosts."""
        return self._get("/tides/status")

    def set_boost(self, name: str, factor: float):
        """Set a tides boost. Use 0 to clear."""
        self._post("/tides/boost", {"name": name, "factor": factor})

    # --- Status ---

    def status(self) -> PoolStatus:
        data = self._get("/pool/status")
        return PoolStatus(**data)

    def agents(self) -> list[dict]:
        return self._get("/pool/agents")

    def health(self) -> dict:
        return self._get("/health")

    def renew(self, domain: str):
        """Force CF clearance renewal for a domain."""
        self._post("/renew", {"domain": domain})

    # --- Internal ---

    def _request(
        self,
        lease_id: str,
        url: str,
        actions: list[dict] | None,
        max_timeout: int,
        capture_xhr: bool,
        xhr_filter: str,
    ) -> ShoalResponse:
        payload: dict[str, Any] = {"lease_id": lease_id}
        if url:
            payload["url"] = url
        if actions:
            payload["actions"] = actions
        if max_timeout:
            payload["max_timeout"] = max_timeout
        if capture_xhr:
            payload["capture_xhr"] = True
        if xhr_filter:
            payload["capture_xhr_filter"] = xhr_filter

        data = self._post("/request", payload)
        return ShoalResponse.from_dict(data)

    def _post(self, path: str, payload: dict) -> dict:
        try:
            resp = self.http.post(f"{self.base_url}{path}", json=payload)
        except requests.ConnectionError:
            raise ShoalError("connection_refused", f"cannot reach Shoal at {self.base_url}")
        except requests.Timeout:
            raise ShoalError("timeout", f"request to {self.base_url}{path} timed out")
        try:
            data = resp.json()
        except ValueError:
            raise ShoalError("invalid_response", f"non-JSON response from {path} (status {resp.status_code})")
        if resp.status_code >= 400:
            raise ShoalError(data.get("error", "unknown"), data.get("detail", ""))
        return data

    def _get(self, path: str) -> Any:
        try:
            resp = self.http.get(f"{self.base_url}{path}")
            return resp.json()
        except requests.ConnectionError:
            raise ShoalError("connection_refused", f"cannot reach Shoal at {self.base_url}")
        except requests.Timeout:
            raise ShoalError("timeout", f"request to {self.base_url}{path} timed out")
        except ValueError:
            raise ShoalError("invalid_response", f"non-JSON response from {path}")

    def is_available(self) -> bool:
        """Check if the Shoal controller is reachable."""
        try:
            self.health()
            return True
        except ShoalError:
            return False


def _next_class(current: str) -> str | None:
    """Return the next heavier agent class, or None if already heaviest."""
    upgrade = {"light": "medium", "medium": "heavy", "": "medium"}
    return upgrade.get(current)
