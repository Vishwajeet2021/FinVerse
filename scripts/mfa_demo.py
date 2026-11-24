"""Live checks for administrator MFA (docs/24-admin-mfa.md).

    python scripts/mfa_demo.py        (stack up; loadtest/setup.py run once)

Takes about two minutes: the server accepts each 30-second TOTP step once, so a script that signs
the same user in repeatedly has to wait for new steps. That wait is itself the replay protection.

  1. customers are unaffected; internal roles get no session from a password alone
  2. enrolment: wrong code refused, right code opens the session with mfa=true; secrets at rest
  3. sign-in: wrong code refused, a code works once (replay refused), refresh keeps mfa
  4. a legacy internal session without MFA is not renewed
  5. recovery codes work exactly once
  6. wrong codes exhaust the challenge and lock the account; logging in again does not reset it
  7. a customer can opt in (password required); secrets never reach the logs
"""
import hashlib
import json
import pathlib
import re
import subprocess
import time
import urllib.error
import urllib.request
import uuid
import base64

from mfa_helper import (COOKIE, enroll_and_login, fresh_code, jwt_claims, login_with_totp, post, refresh_cookie)

ROOT = pathlib.Path(__file__).resolve().parent.parent
AUTH, ADMIN, REPORT = "http://localhost:8081", "http://localhost:8089", "http://localhost:8090"
COMPOSE = ["docker", "compose", "-f", str(ROOT / "docker/docker-compose.yml")]
PW = "Adm!nPassw0rd-9x"
failures = []


def check(ok, what):
    print(f"   {'PASS' if ok else 'FAIL'}  {what}")
    if not ok:
        failures.append(what)


def sh(*args):
    return subprocess.run(args, capture_output=True, text=True)


def psql(sql):
    return sh(*COMPOSE, "exec", "-T", "postgres", "psql", "-U", "finverse", "-d", "auth_db", "-tA", "-c", sql).stdout.strip()


def get(url, token=None):
    req = urllib.request.Request(url, method="GET")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req) as r:
            return r.status
    except urllib.error.HTTPError as e:
        return e.code


def code_of(body):
    return body.get("error", {}).get("code") if isinstance(body, dict) else None


def make_user(role):
    email = f"mfa{uuid.uuid4().hex[:10]}@example.com"
    post(f"{AUTH}/api/v1/auth/register", {"email": email, "password": PW})
    tok = None
    for _ in range(20):
        found = re.findall(re.escape(email) + r".*?token=([A-Za-z0-9_\-]+)", sh(*COMPOSE, "logs", "auth-service").stdout)
        if found:
            tok = found[-1]
            break
        time.sleep(1)
    assert tok, "no verification token in the auth-service logs"
    post(f"{AUTH}/api/v1/auth/verify-email", {"token": tok})
    if role != "customer":
        psql(f"UPDATE users SET role='{role}' WHERE email='{email}'")
    return email


def main():
    fx = json.loads((ROOT / "loadtest/fixtures.json").read_text())["users"]

    print("1. customers are unaffected; internal roles get no session from a password alone")
    s, body, headers = post(f"{AUTH}/api/v1/auth/login", {"email": fx[0]["email"], "password": fx[0]["password"]})
    check(s == 200 and body["data"].get("accessToken") and not body["data"].get("mfaRequired"),
          "a customer without MFA still gets tokens directly")
    check(jwt_claims(body["data"]["accessToken"]).get("mfa") is False, "...and their token carries mfa=false")
    check(refresh_cookie(headers) is not None, "...and the refresh cookie is set")

    admin_a = make_user("admin")
    s, body, headers = post(f"{AUTH}/api/v1/auth/login", {"email": admin_a, "password": PW})
    d = body.get("data", {})
    check(s == 200 and d.get("mfaEnrollmentRequired") is True and "accessToken" not in d,
          "an admin's password alone yields an enrolment challenge, no access token")
    check(refresh_cookie(headers) is None, "...and no refresh cookie")
    check("no-store" in (headers.get("Cache-Control") or ""), "...and the response is marked no-store")
    challenge = d.get("challengeToken", "")
    check(get(f"{ADMIN}/api/v1/admin/dashboard", challenge) == 401, "the challenge token opens no API (401)")
    for role in ("support", "fraud_analyst", "auditor", "super_admin"):
        email = make_user(role)
        s, body, _ = post(f"{AUTH}/api/v1/auth/login", {"email": email, "password": PW})
        check(body.get("data", {}).get("mfaEnrollmentRequired") is True and "accessToken" not in body.get("data", {}),
              f"role {role}: no session without enrolling")

    print("2. enrolment")
    s, body, _ = post(f"{AUTH}/api/v1/auth/mfa/enroll/begin", {"challengeToken": "not-a-real-token"})
    check(s == 401 and code_of(body) == "MFA_CHALLENGE_INVALID", "an unknown challenge is refused")
    s, body, headers = post(f"{AUTH}/api/v1/auth/mfa/enroll/begin", {"challengeToken": challenge})
    check(s == 200 and "otpauth://totp/FinVerse:" in body["data"]["otpauthUri"], "begin returns a provisioning URI")
    check("no-store" in (headers.get("Cache-Control") or ""), "...marked no-store")
    secret_b32 = body["data"]["secret"]
    secret = base64.b32decode(secret_b32 + "=" * (-len(secret_b32) % 8))
    s, body, _ = post(f"{AUTH}/api/v1/auth/mfa/enroll/confirm", {"challengeToken": challenge, "code": "000000"})
    check(s == 401 and code_of(body) == "MFA_CODE_INVALID", "a wrong confirmation code is refused")
    code, step = fresh_code(secret)
    s, body, headers = post(f"{AUTH}/api/v1/auth/mfa/enroll/confirm", {"challengeToken": challenge, "code": code})
    check(s == 200, "the right code completes enrolment and sign-in")
    token = body["data"]["accessToken"]
    recovery = body["data"]["recoveryCodes"]
    check(jwt_claims(token).get("mfa") is True, "the session carries mfa=true")
    check(len(recovery) == 10 and all(re.fullmatch(r"[A-Z2-7]{5}-[A-Z2-7]{5}", c) for c in recovery),
          "ten recovery codes are issued, once")
    check(get(f"{ADMIN}/api/v1/admin/dashboard", token) == 200, "the admin console accepts the session")
    s, body, _ = post(f"{AUTH}/api/v1/auth/mfa/enroll/confirm", {"challengeToken": challenge, "code": code})
    check(s == 401 and code_of(body) == "MFA_CHALLENGE_INVALID", "the challenge cannot be used a second time")

    row = psql(f"SELECT mfa_enabled, left(mfa_secret_enc, 3), mfa_secret_enc LIKE '%{secret_b32}%' "
               f"FROM users WHERE email='{admin_a}'")
    check(row == "t|v1:|f", f"the TOTP secret is stored encrypted, not in clear ({row})")
    hashes = psql(f"SELECT code_hash FROM mfa_recovery_codes WHERE user_id=(SELECT id FROM users WHERE email='{admin_a}')").split()
    check(len(hashes) == 10 and all(len(h) == 64 for h in hashes) and not set(hashes) & {c.replace('-', '') for c in recovery}
          and hashes.count(hashlib.sha256(recovery[0].replace('-', '').encode()).hexdigest()) == 1,
          "recovery codes are stored only as SHA-256 hashes")

    print("3. sign-in with a code")
    s, body, _ = post(f"{AUTH}/api/v1/auth/login", {"email": admin_a, "password": PW})
    check(body["data"].get("mfaRequired") is True and "accessToken" not in body["data"], "login now asks for a code")
    ch = body["data"]["challengeToken"]
    s, body, _ = post(f"{AUTH}/api/v1/auth/mfa/verify", {"challengeToken": ch, "code": "000000"})
    check(s == 401 and code_of(body) == "MFA_CODE_INVALID", "a wrong code is refused")
    s, body, _ = post(f"{AUTH}/api/v1/auth/mfa/verify", {"challengeToken": ch, "code": code})
    check(s == 401, "the code used at enrolment cannot be reused (replay)")
    code2, step2 = fresh_code(secret, step)
    s, body, headers = post(f"{AUTH}/api/v1/auth/mfa/verify", {"challengeToken": ch, "code": code2})
    check(s == 200 and jwt_claims(body["data"]["accessToken"]).get("mfa") is True, "a fresh code signs in with mfa=true")
    cookie = refresh_cookie(headers)

    s, body, _ = post(f"{AUTH}/api/v1/auth/login", {"email": admin_a, "password": PW})
    s, body, _ = post(f"{AUTH}/api/v1/auth/mfa/verify", {"challengeToken": body["data"]["challengeToken"], "code": code2})
    check(s == 401 and code_of(body) == "MFA_CODE_INVALID", "the same code within its window is refused the second time")

    s, body, headers = post(f"{AUTH}/api/v1/auth/refresh", cookie=cookie)
    check(s == 200 and jwt_claims(body["data"]["accessToken"]).get("mfa") is True, "refresh keeps mfa=true")
    cookie = refresh_cookie(headers)

    print("4. a legacy internal session without MFA is not renewed")
    uid = jwt_claims(token)["sub"]
    legacy = "legacy-" + uuid.uuid4().hex
    psql("INSERT INTO refresh_tokens (user_id, token_hash, family_id, expires_at, mfa_verified) VALUES "
         f"('{uid}', '{hashlib.sha256(legacy.encode()).hexdigest()}', '{uuid.uuid4()}', now() + interval '1 hour', FALSE)")
    s, body, _ = post(f"{AUTH}/api/v1/auth/refresh", cookie=legacy)
    check(s == 401 and code_of(body) == "MFA_REQUIRED", "refresh of an unverified internal session -> 401 MFA_REQUIRED")
    revoked = psql(f"SELECT revoked FROM refresh_tokens WHERE token_hash='{hashlib.sha256(legacy.encode()).hexdigest()}'")
    check(revoked == "t", "...and that session is revoked")

    print("5. recovery codes")
    s, body, _ = post(f"{AUTH}/api/v1/auth/login", {"email": admin_a, "password": PW})
    s, body, _ = post(f"{AUTH}/api/v1/auth/mfa/verify", {"challengeToken": body["data"]["challengeToken"], "code": recovery[0].lower()})
    check(s == 200 and jwt_claims(body["data"]["accessToken"]).get("mfa") is True, "a recovery code signs in (typed in lower case)")
    s, body, _ = post(f"{AUTH}/api/v1/auth/login", {"email": admin_a, "password": PW})
    s, body, _ = post(f"{AUTH}/api/v1/auth/mfa/verify", {"challengeToken": body["data"]["challengeToken"], "code": recovery[0]})
    check(s == 401 and code_of(body) == "MFA_CODE_INVALID", "the same recovery code is refused the second time")
    s, body, _ = post(f"{AUTH}/api/v1/auth/login", {"email": admin_a, "password": PW})
    s, body, _ = post(f"{AUTH}/api/v1/auth/mfa/verify", {"challengeToken": body["data"]["challengeToken"], "code": recovery[1]})
    check(s == 200, "a different recovery code still works")

    print("6. guessing is bounded, and a correct password does not reset the count")
    admin_b = make_user("admin")
    sess_b = enroll_and_login(AUTH, admin_b, PW)
    s, body, _ = post(f"{AUTH}/api/v1/auth/login", {"email": admin_b, "password": PW})
    ch = body["data"]["challengeToken"]
    statuses = []
    for _ in range(5):
        s, body, _ = post(f"{AUTH}/api/v1/auth/mfa/verify", {"challengeToken": ch, "code": "000000"})
        statuses.append(s)
    check(statuses == [401] * 5, f"five wrong codes are refused ({statuses})")
    good, _ = fresh_code(sess_b["secret"], sess_b["last_step"])
    s, body, _ = post(f"{AUTH}/api/v1/auth/mfa/verify", {"challengeToken": ch, "code": good})
    check(s == 401, f"...and then even the right code is refused on that challenge ({code_of(body)})")
    s, body, _ = post(f"{AUTH}/api/v1/auth/login", {"email": admin_b, "password": PW})
    check(s == 401 and code_of(body) == "ACCOUNT_LOCKED",
          "logging in again with the right password does not reset the count: the account is locked")
    failed = psql(f"SELECT failed_login_count FROM users WHERE email='{admin_b}'")
    check(failed.isdigit() and int(failed) >= 5, f"failed_login_count persisted ({failed})")

    print("7. a customer can opt in; secrets stay out of the logs")
    cust = make_user("customer")
    s, body, _ = post(f"{AUTH}/api/v1/auth/login", {"email": cust, "password": PW})
    ctoken = body["data"]["accessToken"]
    s, body, _ = post(f"{AUTH}/api/v1/auth/mfa/setup/begin", {"password": PW})
    check(s == 401, "set-up without a session is refused")
    s, body, _ = post(f"{AUTH}/api/v1/auth/mfa/setup/begin", {"password": "wrong-password-123"}, token=ctoken)
    check(s == 401 and code_of(body) == "INVALID_CREDENTIALS", "set-up needs the current password")
    s, body, _ = post(f"{AUTH}/api/v1/auth/mfa/setup/begin", {"password": PW}, token=ctoken)
    check(s == 200, "set-up begins with the right password")
    csecret = base64.b32decode(body["data"]["secret"] + "=" * (-len(body["data"]["secret"]) % 8))
    s, body, _ = post(f"{AUTH}/api/v1/auth/mfa/setup/confirm", {"code": "000000"}, token=ctoken)
    check(s == 401 and code_of(body) == "MFA_CODE_INVALID", "a wrong confirmation code is refused")
    ccode, cstep = fresh_code(csecret)
    s, body, _ = post(f"{AUTH}/api/v1/auth/mfa/setup/confirm", {"code": ccode}, token=ctoken)
    check(s == 200 and len(body["data"]["recoveryCodes"]) == 10, "the right code enables MFA and returns recovery codes")
    s, body, _ = post(f"{AUTH}/api/v1/auth/mfa/setup/begin", {"password": PW}, token=ctoken)
    check(s == 409 and code_of(body) == "MFA_ALREADY_ENABLED", "a live secret cannot be replaced")
    s, body, _ = post(f"{AUTH}/api/v1/auth/login", {"email": cust, "password": PW})
    check(body["data"].get("mfaRequired") is True, "that customer is now challenged at login")

    logs = sh(*COMPOSE, "logs", "auth-service").stdout
    check("mfa_login_succeeded" in logs and "mfa_code_rejected" in logs and "mfa_recovery_code_used" in logs,
          "security events are logged (success, rejection, recovery-code use)")
    leaked = [name for name, value in (("TOTP secret", secret_b32), ("recovery code", recovery[0]),
                                       ("challenge token", challenge), ("access token", token)) if value in logs]
    check(not leaked, f"no secret material in the logs ({leaked or 'clean'})")

    print()
    if failures:
        print(f"{len(failures)} CHECK(S) FAILED:")
        for f in failures:
            print("  -", f)
        raise SystemExit(1)
    print("ALL MFA CHECKS PASSED")


main()
