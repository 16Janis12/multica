#!/usr/bin/env python3
"""
Direct HTTP PoC script to query Gemini (Antigravity) usage and quota endpoints.
Queries the CloudCode / Antigravity internal backend API directly via HTTP.

Features:
- Gemini Weekly & 5-hour limits
- Claude & GPT (3P) Weekly & 5-hour limits
- Formatted human output, statusline (--short), or structured JSON (--json)
"""

import argparse
import datetime
import json
import os
import platform
import sys
import urllib.parse
import urllib.request
from typing import Any, Dict, Optional

ENDPOINTS = [
    "https://daily-cloudcode-pa.googleapis.com",
    "https://cloudcode-pa.googleapis.com",
]

# The API filters the "Gemini Models" group based on the client User-Agent.
# Using the desktop client format returns BOTH Gemini and Claude/GPT quota buckets.
USER_AGENTS = [
    f"antigravity/2.23.0 ({platform.system().lower()}; {platform.machine().lower()})",
    f"antigravity/hub/1.0.0 (aidev_client; os_type={platform.system().lower()}; arch={platform.machine().lower()}; cl=963137146)",
]


def find_token_file() -> Optional[str]:
    """Finds acp_token.json across standard Gemini / T3 provider paths."""
    candidates = [
        os.path.expanduser("~/.gemini/antigravity-acp/acp_token.json"),
        os.path.expanduser("~/.gemini/antigravity/acp_token.json"),
    ]
    t3_root = os.path.expanduser("~/.t3/userdata/providers/antigravity")
    if os.path.exists(t3_root):
        matches = []
        for root, _, files in os.walk(t3_root):
            if "acp_token.json" in files:
                matches.append(os.path.join(root, "acp_token.json"))
        # Sort by modification time descending to pick freshest token
        matches.sort(key=lambda p: os.path.getmtime(p), reverse=True)
        candidates.extend(matches)

    for p in candidates:
        if os.path.isfile(p):
            return p
    return None


def get_token_from_keyring() -> Optional[Dict[str, Any]]:
    """Attempts to retrieve token from Linux Secret Service if acp_token.json is missing."""
    import shutil
    import subprocess
    if shutil.which("secret-tool"):
        try:
            raw = subprocess.check_output(
                ["secret-tool", "lookup", "service", "gemini", "username", "antigravity"],
                stderr=subprocess.DEVNULL,
                text=True,
            ).strip()
            if raw:
                data = json.loads(raw)
                token_info = data.get("token", {})
                return {
                    "client_id": "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com",
                    "client_secret": None,
                    "access_token": token_info.get("access_token"),
                    "refresh_token": token_info.get("refresh_token"),
                }
        except Exception:
            pass
    return None


def refresh_access_token(token_data: Dict[str, Any]) -> str:
    """Exchanges refresh_token for a fresh access_token via Google OAuth endpoint."""
    refresh_token = token_data.get("refresh_token")
    client_id = token_data.get("client_id")
    client_secret = token_data.get("client_secret")

    if not refresh_token:
        if token_data.get("access_token"):
            return token_data["access_token"]
        raise ValueError("No refresh_token or access_token found in token file.")

    body = {
        "client_id": client_id,
        "refresh_token": refresh_token,
        "grant_type": "refresh_token",
    }
    if client_secret:
        body["client_secret"] = client_secret

    req = urllib.request.Request(
        "https://oauth2.googleapis.com/token",
        data=urllib.parse.urlencode(body).encode("utf-8"),
        headers={"Content-Type": "application/x-www-form-urlencoded"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=10) as resp:
        res = json.loads(resp.read().decode("utf-8"))
        return res["access_token"]


def api_post(endpoint: str, operation: str, access_token: str, payload: Dict[str, Any], ua: str) -> Optional[Dict[str, Any]]:
    url = f"{endpoint}/v1internal:{operation}"
    req = urllib.request.Request(
        url,
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {access_token}",
            "User-Agent": ua,
            "Content-Type": "application/json",
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            if resp.status == 200:
                return json.loads(resp.read().decode("utf-8"))
    except Exception:
        pass
    return None


def fetch_quota_summary(access_token: str, project_id: Optional[str]) -> Optional[Dict[str, Any]]:
    payload = {"project": project_id} if project_id else {}
    for ua in USER_AGENTS:
        for ep in ENDPOINTS:
            data = api_post(ep, "retrieveUserQuotaSummary", access_token, payload, ua)
            if data and data.get("groups"):
                has_gemini = any("Gemini" in g.get("displayName", "") for g in data["groups"])
                if has_gemini:
                    return data
    return None


def format_delta(target_iso: str) -> str:
    try:
        clean_iso = target_iso.replace("Z", "+00:00")
        target_dt = datetime.datetime.fromisoformat(clean_iso)
        diff = target_dt - datetime.datetime.now(datetime.timezone.utc)
        if diff.total_seconds() <= 0:
            return "ready to reset"
        total_secs = int(diff.total_seconds())
        days, rem = divmod(total_secs, 86400)
        hours, rem = divmod(rem, 3600)
        minutes, _ = divmod(rem, 60)
        parts = []
        if days > 0:
            parts.append(f"{days}d")
        if hours > 0 or days > 0:
            parts.append(f"{hours}h")
        parts.append(f"{minutes}m")
        return "in " + " ".join(parts)
    except Exception:
        return target_iso


def render_bar(fraction: float, length: int = 18) -> str:
    fraction = max(0.0, min(1.0, fraction))
    filled = int(round(fraction * length))
    return f"[{'█' * filled}{'░' * (length - filled)}]"


def print_report(summary: Optional[Dict[str, Any]]):
    print("=" * 66)
    print("      Antigravity / Gemini Quota & Usage (Direct HTTP API)      ")
    print("=" * 66)

    if not summary or not summary.get("groups"):
        print("\n  No active quota limits returned.")
        print("\n" + "=" * 66)
        return

    for group in summary["groups"]:
        name = group.get("displayName", "Limits")
        desc = group.get("description", "")
        print(f"\n▶ {name}")
        if desc:
            print(f"  {desc}")
        print("  " + "-" * 60)

        for b in group.get("buckets", []):
            b_name = b.get("displayName") or b.get("bucketId", "Bucket")
            frac = float(b.get("remainingFraction", 0.0))
            reset = b.get("resetTime", "")
            countdown = f"resets {format_delta(reset)}" if reset else "no reset time"
            print(f"  • {b_name:<26} {render_bar(frac)} {frac * 100:5.1f}% ({countdown})")

    print("\n" + "=" * 66)


def print_short_summary(summary: Optional[Dict[str, Any]]):
    if not summary or not summary.get("groups"):
        print("No quota data")
        return
    parts = []
    for group in summary["groups"]:
        g_name = group.get("displayName", "")
        label = "Gemini" if "Gemini" in g_name else "3P"
        bucket_strs = []
        for b in group.get("buckets", []):
            window = b.get("window", "")
            pct = int(round(float(b.get("remainingFraction", 0.0)) * 100))
            bucket_strs.append(f"{window}:{pct}%")
        if bucket_strs:
            parts.append(f"{label}[{', '.join(bucket_strs)}]")
    print(" | ".join(parts))


def main():
    parser = argparse.ArgumentParser(description="Query Antigravity usage API directly via HTTP.")
    parser.add_argument("--json", action="store_true", help="Print combined JSON payload")
    parser.add_argument("--short", action="store_true", help="Print compact one-line status")
    parser.add_argument("--token-file", help="Explicit path to acp_token.json")
    args = parser.parse_args()

    token_file = args.token_file or find_token_file()
    token_data = None
    if token_file and os.path.isfile(token_file):
        with open(token_file) as f:
            token_data = json.load(f)
    else:
        token_data = get_token_from_keyring()

    if not token_data:
        sys.stderr.write("Error: Could not locate acp_token.json or keyring credentials.\n")
        sys.exit(1)

    try:
        access_token = refresh_access_token(token_data)
    except Exception as e:
        sys.stderr.write(f"Error refreshing access token: {e}\n")
        sys.exit(1)

    project_id = token_data.get("project_id")
    summary = fetch_quota_summary(access_token, project_id)

    if args.json:
        print(json.dumps(summary, indent=2))
        return

    if args.short:
        print_short_summary(summary)
        return

    print_report(summary)


if __name__ == "__main__":
    main()
