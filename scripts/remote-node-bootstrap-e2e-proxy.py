#!/usr/bin/env python3

import argparse
import http.client
import mimetypes
import os
import ssl
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit


HOP_BY_HOP = {
    "connection",
    "keep-alive",
    "proxy-authenticate",
    "proxy-authorization",
    "te",
    "trailers",
    "transfer-encoding",
    "upgrade",
}


class Handler(BaseHTTPRequestHandler):
    server_version = "RouteGateBootstrapE2E/1"

    def do_GET(self):
        self._handle()

    def do_HEAD(self):
        self._handle(head_only=True)

    def do_POST(self):
        self._handle()

    def do_PUT(self):
        self._handle()

    def do_PATCH(self):
        self._handle()

    def do_DELETE(self):
        self._handle()

    def log_message(self, fmt, *args):
        print("[bootstrap-e2e-proxy] " + fmt % args, flush=True)

    def _handle(self, head_only=False):
        parsed = urlsplit(self.path)
        if parsed.path.startswith("/bootstrap/"):
            self._serve_static(parsed.path, head_only)
            return
        if parsed.path.startswith("/api/"):
            self._proxy(parsed, head_only)
            return
        self.send_error(404)

    def _serve_static(self, request_path, head_only):
        relative = request_path.removeprefix("/").lstrip("/")
        candidate = (self.server.static_root / relative).resolve()
        root = self.server.static_root.resolve()
        try:
            candidate.relative_to(root)
        except ValueError:
            self.send_error(403)
            return
        if not candidate.is_file() or candidate.is_symlink():
            self.send_error(404)
            return

        payload = candidate.read_bytes()
        content_type = mimetypes.guess_type(candidate.name)[0] or "application/octet-stream"
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(payload)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        if not head_only:
            self.wfile.write(payload)

    def _proxy(self, parsed, head_only):
        body_length = int(self.headers.get("Content-Length", "0") or "0")
        body = self.rfile.read(body_length) if body_length else None

        connection = http.client.HTTPConnection(
            self.server.manager_host,
            self.server.manager_port,
            timeout=30,
        )
        headers = {
            key: value
            for key, value in self.headers.items()
            if key.lower() not in HOP_BY_HOP and key.lower() != "host"
        }
        target = parsed.path
        if parsed.query:
            target += "?" + parsed.query

        try:
            connection.request(self.command, target, body=body, headers=headers)
            response = connection.getresponse()
            payload = response.read()
        except Exception as exc:
            self.send_error(502, explain=str(exc))
            return
        finally:
            connection.close()

        self.send_response(response.status, response.reason)
        for key, value in response.getheaders():
            lowered = key.lower()
            if lowered in HOP_BY_HOP or lowered == "content-length":
                continue
            self.send_header(key, value)
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        if not head_only:
            self.wfile.write(payload)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--listen-host", default="127.0.0.1")
    parser.add_argument("--listen-port", type=int, required=True)
    parser.add_argument("--manager-host", default="127.0.0.1")
    parser.add_argument("--manager-port", type=int, required=True)
    parser.add_argument("--static-root", type=Path, required=True)
    parser.add_argument("--cert", required=True)
    parser.add_argument("--key", required=True)
    args = parser.parse_args()

    server = ThreadingHTTPServer((args.listen_host, args.listen_port), Handler)
    server.manager_host = args.manager_host
    server.manager_port = args.manager_port
    server.static_root = args.static_root

    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.load_cert_chain(certfile=args.cert, keyfile=args.key)
    server.socket = context.wrap_socket(server.socket, server_side=True)

    print(
        f"[bootstrap-e2e-proxy] https://{args.listen_host}:{args.listen_port} "
        f"-> http://{args.manager_host}:{args.manager_port}",
        flush=True,
    )
    server.serve_forever()


if __name__ == "__main__":
    main()
