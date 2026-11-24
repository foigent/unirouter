"""Test the example as a customer runs it, using a synthetic local gateway."""
from contextlib import contextmanager
import http.server
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import threading
import unittest

KEY = "synthetic-first-use-key-do-not-print"
PREVIEW_ID = "op_" + "1" * 32
CHAT_ID = "op_" + "2" * 32
ATTEMPTS = "X-UniRouter-Max-Upstream-Attempts"
SCRIPT = Path(__file__).with_name("first_request.py")


class FirstRequestTest(unittest.TestCase):
    def run_example(self, args=(), **settings):
        allowed = {k: v for k, v in os.environ.items() if k in ("PATH", "SYSTEMROOT", "TMPDIR")}
        result = subprocess.run([sys.executable, "-I", "-B", SCRIPT, *args],
                                env={**allowed, **settings}, capture_output=True, text=True, timeout=10)
        self.assertNotIn(KEY, result.stdout + result.stderr)
        self.assertEqual(result.stderr, "")
        return result

    @contextmanager
    def gateway(self, *, preview_status=200, admissible=True, supported=True,
                chat_status=200, disconnect=False, chat_id=CHAT_ID, chat_body=None):
        received = []

        class Gateway(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                body = self.rfile.read(int(self.headers["Content-Length"]))
                received.append((self.path, dict(self.headers), json.loads(body)))
                preview = self.path.endswith("/preflight")
                if disconnect and not preview:
                    self.connection.shutdown(socket.SHUT_RDWR)
                    return
                status = preview_status if preview else chat_status
                self.send_response(status)
                if status == 307:
                    self.send_header("Location", "/redirect-must-not-run")
                if preview or chat_id is not None:
                    self.send_header("X-Request-ID", PREVIEW_ID if preview else chat_id)
                if supported or not preview:
                    self.send_header(ATTEMPTS, "1")
                self.end_headers()
                if preview:
                    data = {"locally_admissible": admissible}
                elif chat_body is None:
                    data = {"choices": [{"message": {"content": KEY}}]}
                else:
                    data = chat_body
                self.wfile.write(json.dumps(data if status == 200 else {"error": KEY}).encode())

            def log_message(self, *args):
                pass

        with http.server.HTTPServer(("127.0.0.1", 0), Gateway) as server:
            worker = threading.Thread(target=server.serve_forever)
            worker.start()
            try:
                settings = dict(UR_BASE_URL=f"http://127.0.0.1:{server.server_port}/v1",
                                UR_API_KEY=KEY, UR_MODEL="qualified-fixture-model")
                yield received, settings
            finally:
                server.shutdown()
                worker.join()

    def test_missing_configuration_does_not_contact_gateway(self):
        with self.gateway() as (calls, config):
            del config["UR_MODEL"]
            result = self.run_example(("--send",), **config)
            self.assertEqual(result.returncode, 2)
            self.assertIn("UR_MODEL", result.stdout)
            self.assertEqual(calls, [])

    def test_preview_only_by_default(self):
        with self.gateway() as (calls, config):
            result = self.run_example(**config)
            self.assertEqual(result.returncode, 0)
            self.assertIn("No Chat request was sent", result.stdout)
            self.assertEqual([call[0] for call in calls], ["/v1/chat/completions/preflight"])

    def test_live_once_uses_identical_body_and_correlation(self):
        with self.gateway() as (calls, config):
            result = self.run_example(("--send",), **config)
            self.assertEqual(result.returncode, 0)
            self.assertIn(f"Preflight request ID: {PREVIEW_ID}", result.stdout)
            self.assertIn(f"Chat request ID: {CHAT_ID}", result.stdout)
            self.assertEqual([call[0] for call in calls], [
                "/v1/chat/completions/preflight", "/v1/chat/completions"])
            self.assertEqual(calls[0][2], calls[1][2])
            headers = {k.lower(): v for k, v in calls[1][1].items()}
            self.assertEqual(headers["authorization"], "Bearer " + KEY)
            self.assertEqual(headers[ATTEMPTS.lower()], "1")
            intent = headers["x-unirouter-client-request-id"]
            self.assertEqual(headers["idempotency-key"], intent)
            self.assertIn(intent, result.stdout)

    def test_unsafe_preflight_results_never_send_chat(self):
        for options in ({"supported": False}, {"admissible": False}, {"preview_status": 404},
                        {"preview_status": 401}, {"preview_status": 307}):
            with self.subTest(options=options):
                with self.gateway(**options) as (calls, config):
                    result = self.run_example(("--send",), **config)
                    self.assertEqual(result.returncode, 1)
                    self.assertIn("No Chat request was sent", result.stdout)
                    self.assertEqual([call[0] for call in calls], ["/v1/chat/completions/preflight"])

    def test_chat_errors_and_disconnect_keep_the_original_reference(self):
        for options in ({"chat_status": 503}, {"chat_status": 307}, {"disconnect": True},
                        {"chat_id": None}, {"chat_id": KEY}, {"chat_body": {}}):
            with self.subTest(options=options):
                with self.gateway(**options) as (calls, config):
                    result = self.run_example(("--send",), **config)
                    self.assertEqual(result.returncode, 1)
                    self.assertIn("Check the original request in Logs", result.stdout)
                    self.assertIn("Chat client request ID: first-use-", result.stdout)
                    self.assertEqual([call[0] for call in calls], [
                        "/v1/chat/completions/preflight", "/v1/chat/completions"])
                    if not options.get("disconnect") and options.get("chat_id", CHAT_ID) == CHAT_ID:
                        self.assertIn(CHAT_ID, result.stdout)

if __name__ == "__main__":
    unittest.main()
