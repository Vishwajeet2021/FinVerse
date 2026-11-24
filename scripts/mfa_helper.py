"""Second-factor helpers for the demo scripts (docs/24-admin-mfa.md).

Every internal role must pass MFA now, so a script that promotes a user to admin can no longer
just log in: the first sign-in is an enrolment. This module does that with nothing but the
standard library, including its own TOTP (RFC 6238), so the scripts stay dependency free.

    from mfa_helper import enroll_and_login, login_with_totp
    session = enroll_and_login(AUTH, email, password)   # first sign-in of a staff user
    token = session["token"]
    later = login_with_totp(AUTH, email, password, session["secret"])   # later sign-ins
"""
import base64
import hashlib
import hmac
import json
import struct
import time
import urllib.error
import urllib.request

COOKIE = "finverse_refresh_token"


def totp(secret: bytes, at: float | None = None, digits: int = 6) -> str:
    counter = int((time.time() if at is None else at) // 30)
    mac = hmac.new(secret, struct.pack(">Q", counter), hashlib.sha1).digest()
    off = mac[-1] & 0x0F
    value = struct.unpack(">I", mac[off:off + 4])[0] & 0x7FFFFFFF
    return str(value % 10**digits).zfill(digits)


def step_now() -> int:
    return int(time.time() // 30)


def fresh_code(secret: bytes, after_step: int = 0) -> tuple[str, int]:
    """A code for a step strictly newer than after_step, waiting for the next window if need be.

    The server accepts each 30-second step once, so a script that signs in twice with the same
    secret must not reuse a step. Waiting is the honest way to produce a second valid code.
    """
    while step_now() <= after_step:
        time.sleep(1)
    return totp(secret), step_now()


def post(url: str, body: dict | None = None, token: str | None = None, cookie: str | None = None):
    """POST JSON. Returns (status, parsed body, response headers). Never raises on an HTTP error."""
    req = urllib.request.Request(url, method="POST", data=json.dumps(body or {}).encode())
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    if cookie:
        req.add_header("Cookie", f"{COOKIE}={cookie}")
    try:
        with urllib.request.urlopen(req) as r:
            raw = r.read()
            return r.status, (json.loads(raw) if raw else {}), r.headers
    except urllib.error.HTTPError as e:
        raw = e.read()
        try:
            return e.code, json.loads(raw), e.headers
        except ValueError:
            return e.code, {"raw": raw.decode(errors="replace")}, e.headers


def refresh_cookie(headers) -> str | None:
    for value in headers.get_all("Set-Cookie") or []:
        if value.startswith(COOKIE + "="):
            return value.split(";", 1)[0].split("=", 1)[1]
    return None


def jwt_claims(token: str) -> dict:
    payload = token.split(".")[1]
    return json.loads(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)))


def enroll_and_login(auth: str, email: str, password: str) -> dict:
    """First sign-in of a user whose role requires MFA: log in, enrol, finish signing in."""
    s, body, _ = post(f"{auth}/api/v1/auth/login", {"email": email, "password": password})
    if s != 200 or not body.get("data", {}).get("mfaEnrollmentRequired"):
        raise SystemExit(f"expected an enrolment challenge for {email}, got {s}: {str(body)[:200]}")
    challenge = body["data"]["challengeToken"]

    s, body, _ = post(f"{auth}/api/v1/auth/mfa/enroll/begin", {"challengeToken": challenge})
    if s != 200:
        raise SystemExit(f"enrol begin failed: {s} {str(body)[:200]}")
    secret = base64.b32decode(body["data"]["secret"] + "=" * (-len(body["data"]["secret"]) % 8))

    code, step = fresh_code(secret)
    s, body, headers = post(f"{auth}/api/v1/auth/mfa/enroll/confirm", {"challengeToken": challenge, "code": code})
    if s != 200:
        raise SystemExit(f"enrol confirm failed: {s} {str(body)[:200]}")
    return {
        "token": body["data"]["accessToken"],
        "secret": secret,
        "recovery": body["data"]["recoveryCodes"],
        "last_step": step,
        "cookie": refresh_cookie(headers),
    }


def login_with_totp(auth: str, email: str, password: str, secret: bytes, last_step: int = 0) -> dict:
    """Sign in as a user who already has MFA."""
    s, body, _ = post(f"{auth}/api/v1/auth/login", {"email": email, "password": password})
    if s != 200 or not body.get("data", {}).get("mfaRequired"):
        raise SystemExit(f"expected an MFA challenge for {email}, got {s}: {str(body)[:200]}")
    code, step = fresh_code(secret, last_step)
    s, body, headers = post(
        f"{auth}/api/v1/auth/mfa/verify", {"challengeToken": body["data"]["challengeToken"], "code": code})
    if s != 200:
        raise SystemExit(f"MFA verify failed: {s} {str(body)[:200]}")
    return {"token": body["data"]["accessToken"], "last_step": step, "cookie": refresh_cookie(headers)}
