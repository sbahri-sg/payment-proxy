#!/usr/bin/env python3

import argparse
import hashlib
import hmac
import json
import os
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def main() -> None:
    parser = argparse.ArgumentParser(description="Conformance-only Emisell webhook receiver")
    parser.add_argument("--port", type=int, default=19090)
    args = parser.parse_args()
    secret = os.environ.get("EMISELL_BACKEND_WEBHOOK_SECRET", os.environ.get("EMISELL_WEBHOOK_SECRET", "")).encode()
    output = os.environ.get("EMISELL_WEBHOOK_OUTPUT", "")
    if not secret or not output:
        raise SystemExit("EMISELL_BACKEND_WEBHOOK_SECRET and EMISELL_WEBHOOK_OUTPUT are required")

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            if self.path != "/health":
                self.send_error(404)
                return
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b"ok")

        def do_POST(self) -> None:
            if self.path not in ("/webhooks/v1/payment-proxy", "/events"):
                self.send_error(404)
                return
            try:
                length = int(self.headers.get("Content-Length", "0"))
            except ValueError:
                self.send_error(400)
                return
            if length <= 0 or length > 1_048_576:
                self.send_error(413)
                return
            body = self.rfile.read(length)
            event_id = self.headers.get("X-Emisell-Webhook-ID", "")
            event_type = self.headers.get("X-Emisell-Event-Type", "")
            merchant_id = self.headers.get("X-Emisell-Merchant-ID", "")
            timestamp = self.headers.get("X-Emisell-Webhook-Timestamp", "")
            signature = self.headers.get("X-Emisell-Webhook-Signature", "")
            if self.headers.get("X-Emisell-Webhook-Version", "") != "1":
                self.send_error(400)
                return
            try:
                if abs(int(time.time()) - int(timestamp)) > 300:
                    raise ValueError("stale timestamp")
            except ValueError:
                self.send_error(401)
                return
            expected = "v1=" + hmac.new(secret, timestamp.encode() + b"." + body, hashlib.sha256).hexdigest()
            if not hmac.compare_digest(signature, expected):
                self.send_error(401)
                return
            try:
                payload = json.loads(body)
            except json.JSONDecodeError:
                self.send_error(400)
                return
            resource = payload.get("resource") or {}
            if (
                payload.get("id") != event_id
                or payload.get("type") != event_type
                or payload.get("merchant_id") != merchant_id
                or payload.get("object") != "event"
                or payload.get("api_version") != "2026-08-28"
                or not resource.get("type")
                or not resource.get("id")
            ):
                self.send_error(400)
                return
            record = {
                "event_id": event_id,
                "event_type": event_type,
                "merchant_id": merchant_id,
                "payload": payload,
            }
            with open(output, "a", encoding="utf-8") as destination:
                destination.write(json.dumps(record, separators=(",", ":")) + "\n")
            self.send_response(204)
            self.end_headers()

        def log_message(self, format: str, *args: object) -> None:
            return

    ThreadingHTTPServer(("0.0.0.0", args.port), Handler).serve_forever()


if __name__ == "__main__":
    main()
