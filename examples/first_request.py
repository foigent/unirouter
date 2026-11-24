#!/usr/bin/env python3
"""Preflight a Chat request and, if requested, send it once to UniRouter."""
import argparse
import http.client
import ipaddress
import json
import os
import re
import urllib.error
import urllib.parse
import urllib.request
import uuid

ATTEMPTS_HEADER = "X-UniRouter-Max-Upstream-Attempts"
MAX_RESPONSE_BYTES = 1024 * 1024


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def chat_url(base_url):
    parsed = urllib.parse.urlsplit(base_url)
    if not parsed.hostname or parsed.username is not None or parsed.query or parsed.fragment:
        raise ValueError("invalid gateway URL")
    if parsed.scheme != "https":
        if parsed.scheme != "http":
            raise ValueError("invalid gateway scheme")
        try:
            local = ipaddress.ip_address(parsed.hostname).is_loopback
        except ValueError:
            local = parsed.hostname == "localhost"
        if not local:
            raise ValueError("cleartext gateway is not loopback")
    base = base_url.rstrip("/")
    return base + ("/chat/completions" if parsed.path.rstrip("/").endswith("/v1") else "/v1/chat/completions")


def report_request_id(headers, api_key, phase):
    for name in ("X-UniRouter-Original-Request-ID", "X-Request-ID"):
        value = headers.get(name, "")
        if re.fullmatch(r"op_[0-9a-f]{32}", value) and api_key not in value:
            print(f"{phase} request ID: {value}", flush=True)
            return value
    return None


def send(opener, url, body, headers, api_key, *, preflight):
    phase = "Preflight" if preflight else "Chat"
    request = urllib.request.Request(url, data=body, headers=headers, method="POST")
    try:
        with opener.open(request, timeout=30 if preflight else 60) as response:
            request_id = report_request_id(response.headers, api_key, phase)
            if response.status != 200:
                print(f"{phase}: HTTP {response.status}.")
                return False
            if response.headers.get_all(ATTEMPTS_HEADER) != ["1"]:
                print(f"{phase}: single-attempt support could not be confirmed.")
                return False
            raw = response.read(MAX_RESPONSE_BYTES + 1)
            if not raw or len(raw) > MAX_RESPONSE_BYTES:
                raise ValueError("response body unavailable")
            result = json.loads(raw)
            if not isinstance(result, dict):
                raise ValueError("expected JSON object")
            if preflight:
                if result.get("locally_admissible") is not True:
                    print("Preflight: the request is not locally admissible. Check your model and key access.")
                    return False
                print("Preflight: accepted locally, single upstream attempt supported.")
            else:
                if request_id is None:
                    print("Chat: a valid server request ID was not returned.")
                    return False
                choices = result.get("choices")
                if not isinstance(choices, list) or not choices or not isinstance(choices[0], dict) or not isinstance(choices[0].get("message"), dict):
                    raise ValueError("incomplete chat completion")
                print("Chat: HTTP 200. Check Logs for usage and billing status.")
            return True
    except urllib.error.HTTPError as error:
        with error:
            report_request_id(error.headers, api_key, phase)
            print(f"{phase}: HTTP {error.code}.")
        return False
    except (OSError, ValueError, http.client.HTTPException):
        print(f"{phase}: a complete response could not be confirmed.")
        return False


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--send", action="store_true", help="send one potentially billable Chat request after preflight")
    parser.add_argument("--disable-thinking", action="store_true",
                        help="set thinking.type=disabled on a compatible model profile")
    args = parser.parse_args()

    settings = {name: os.environ.get(name, "") for name in ("UR_BASE_URL", "UR_API_KEY", "UR_MODEL")}
    missing = [name for name, value in settings.items() if not value.strip()]
    if missing:
        print("Missing local configuration: " + ", ".join(missing))
        return 2
    api_key = settings["UR_API_KEY"]
    try:
        url = chat_url(settings["UR_BASE_URL"].strip())
        if not re.fullmatch(r"[!-~]+", api_key):
            raise ValueError("invalid API key syntax")
    except ValueError:
        print("Use an HTTPS gateway URL or a local HTTP tunnel and a valid application key.")
        return 2

    payload = {
        "model": settings["UR_MODEL"].strip(),
        "messages": [{"role": "user", "content": "Reply with a brief greeting."}],
        "stream": False,
        "max_tokens": 32,
    }
    if args.disable_thinking:
        payload["thinking"] = {"type": "disabled"}
    body = json.dumps(payload).encode()
    headers = {
        "Authorization": "Bearer " + api_key,
        "Content-Type": "application/json",
        ATTEMPTS_HEADER: "1",
    }
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    if not send(opener, url + "/preflight", body, headers, api_key, preflight=True):
        print("No Chat request was sent. Check the installation and preflight before continuing.")
        return 1
    if not args.send:
        print("No Chat request was sent. Use --send if you want to send one potentially billable request.")
        return 0

    intent = "first-use-" + uuid.uuid4().hex
    print(f"Chat client request ID: {intent}", flush=True)
    headers["X-UniRouter-Client-Request-ID"] = intent
    headers["Idempotency-Key"] = intent
    if not send(opener, url, body, headers, api_key, preflight=False):
        print("Check the original request in Logs by Request ID or Chat client request ID before sending another.")
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
