#!/usr/bin/env python3
"""
Local Phone Simulator Proxy

Serves the phone simulator UI locally and proxies API requests with proper auth.
This allows you to view the phone simulator in your browser without manually
adding JWT headers.

Usage:
    python scripts/phone_simulator_local.py

Then open: http://localhost:8090/?orgID=...&phone=...&clinic=...
"""

import http.server
import json
import os
import socketserver
import sys
import time
import urllib.request
import urllib.error
from urllib.parse import urlparse, parse_qs

# Try to import jwt, fall back to manual token if not available
try:
    import jwt
    HAS_JWT = True
except ImportError:
    HAS_JWT = False

API_URL = os.environ.get("API_URL", "http://localhost:8082")
JWT_SECRET = os.environ.get("JWT_SECRET", "test-jwt-secret")
ORG_ID = os.environ.get("TEST_ORG_ID", "11111111-1111-1111-1111-111111111111")
PORT = int(os.environ.get("SIMULATOR_PORT", "8090"))


def generate_admin_jwt(org_id: str, secret: str) -> str:
    """Generate a valid admin JWT for testing."""
    if not HAS_JWT:
        # Fallback: use a pre-generated token (valid for 1 hour from generation)
        print("WARNING: PyJWT not installed. Using fallback token generation.")
        print("Install with: pip install PyJWT")
        # This won't work without the library, but we'll try anyway
        return ""

    now = int(time.time())
    payload = {
        "sub": "test-admin",
        "org_id": org_id,
        "role": "admin",
        "iat": now,
        "exp": now + 3600,
    }
    return jwt.encode(payload, secret, algorithm="HS256")


# Generate token once at startup
AUTH_TOKEN = generate_admin_jwt(ORG_ID, JWT_SECRET) if HAS_JWT else ""


class PhoneSimulatorHandler(http.server.BaseHTTPRequestHandler):
    """HTTP handler that serves phone simulator and proxies API calls."""

    def log_message(self, format, *args):
        """Custom log format."""
        print(f"[{self.log_date_time_string()}] {format % args}")

    def do_GET(self):
        parsed = urlparse(self.path)
        path = parsed.path
        query = parsed.query

        # Serve the phone simulator HTML at root
        if path == "/" or path == "":
            self.serve_simulator(query)
        # Proxy API requests to the backend
        elif path.startswith("/admin/"):
            self.proxy_request(path, query)
        else:
            self.send_error(404, "Not Found")

    def serve_simulator(self, query: str):
        """Fetch and serve the phone simulator HTML."""
        try:
            url = f"{API_URL}/admin/e2e/phone-simulator"
            if query:
                url += f"?{query}"

            req = urllib.request.Request(url)
            req.add_header("Authorization", f"Bearer {AUTH_TOKEN}")

            with urllib.request.urlopen(req, timeout=10) as resp:
                html = resp.read()

                # Modify the HTML to use our local proxy for API calls
                html_str = html.decode("utf-8")
                # The phone simulator already uses relative URLs, so it should work

                self.send_response(200)
                self.send_header("Content-Type", "text/html; charset=utf-8")
                self.send_header("Content-Length", len(html_str))
                self.end_headers()
                self.wfile.write(html_str.encode("utf-8"))

        except urllib.error.HTTPError as e:
            self.send_error(e.code, f"API Error: {e.reason}")
        except Exception as e:
            self.send_error(500, f"Error: {e}")

    def proxy_request(self, path: str, query: str):
        """Proxy API requests to the backend with auth."""
        try:
            url = f"{API_URL}{path}"
            if query:
                url += f"?{query}"

            req = urllib.request.Request(url)
            req.add_header("Authorization", f"Bearer {AUTH_TOKEN}")
            req.add_header("Accept", "application/json")

            with urllib.request.urlopen(req, timeout=30) as resp:
                data = resp.read()
                content_type = resp.headers.get("Content-Type", "application/json")

                self.send_response(200)
                self.send_header("Content-Type", content_type)
                self.send_header("Content-Length", len(data))
                # Allow CORS for local development
                self.send_header("Access-Control-Allow-Origin", "*")
                self.end_headers()
                self.wfile.write(data)

        except urllib.error.HTTPError as e:
            error_body = e.read().decode("utf-8") if e.fp else ""
            self.send_response(e.code)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"error": e.reason, "details": error_body}).encode())
        except Exception as e:
            self.send_error(500, f"Proxy Error: {e}")

    def do_OPTIONS(self):
        """Handle CORS preflight."""
        self.send_response(200)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Authorization, Content-Type")
        self.end_headers()


def main():
    if not HAS_JWT:
        print("=" * 60)
        print("ERROR: PyJWT library not installed!")
        print("Install it with: pip install PyJWT")
        print("=" * 60)
        sys.exit(1)

    if not AUTH_TOKEN:
        print("ERROR: Failed to generate JWT token")
        sys.exit(1)

    print("=" * 60)
    print("Phone Simulator Local Proxy")
    print("=" * 60)
    print(f"API Backend:    {API_URL}")
    print(f"Local Port:     {PORT}")
    print(f"Org ID:         {ORG_ID}")
    print(f"JWT Token:      {AUTH_TOKEN[:20]}...")
    print("=" * 60)
    print()
    print("Open in your browser:")
    print()
    print(f"  http://localhost:{PORT}/?orgID={ORG_ID}&phone=+15005550001&clinic=+13304600937")
    print()
    print("Or with your test phone numbers:")
    print(f"  http://localhost:{PORT}/?orgID=<org>&phone=<customer>&clinic=<clinic>")
    print()
    print("Press Ctrl+C to stop")
    print("=" * 60)

    with socketserver.TCPServer(("", PORT), PhoneSimulatorHandler) as httpd:
        try:
            httpd.serve_forever()
        except KeyboardInterrupt:
            print("\nShutting down...")


if __name__ == "__main__":
    main()
