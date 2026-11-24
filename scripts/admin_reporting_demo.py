"""Live checks for Admin Service and Reporting Service (docs/23).

    python scripts/admin_reporting_demo.py     (stack up; loadtest/setup.py run once)

Admin
  1. privileged routes refuse anonymous callers and customers
  2. dashboard answers, with each panel reporting whether its source was reachable
  3. an admin freeze stops payments, a customer cannot lift it, an admin can
  4. every action is audited (Requested + Resolved events) and the record is immutable
  5. audit-then-act: if the action cannot be recorded, it is NOT performed
  6. fraud case resolution through Admin forwards the admin's identity; downstream errors are recorded
Reporting
  7. a statement's closing balance equals the account's live balance
  8. other customers cannot request, see or download it (404, not 403)
  9. input validation, scope checks, and an admin-only fraud summary
"""
import csv, io, json, pathlib, re, subprocess, sys, time, urllib.error, urllib.request, uuid
import datetime as dt

ROOT = pathlib.Path(__file__).resolve().parent.parent
H = "http://localhost"
AUTH, ACCOUNT, FRAUD, AUDIT = f"{H}:8081", f"{H}:8083", f"{H}:8088", f"{H}:8087"
PAY, ADMIN, REPORT, WALLET = f"{H}:8085", f"{H}:8089", f"{H}:8090", f"{H}:8084"
COMPOSE = ["docker", "compose", "-f", str(ROOT / "docker/docker-compose.yml")]
PW = "Adm!nPassw0rd-9x"
failures = []


def call(method, url, body=None, token=None, headers=None, raw=False):
    """Returns (status, parsed-json-or-bytes). Never raises on an HTTP error status."""
    req = urllib.request.Request(url, method=method, data=json.dumps(body).encode() if body is not None else None)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    try:
        with urllib.request.urlopen(req) as r:
            data = r.read()
            return r.status, (data if raw else (json.loads(data) if data else {}))
    except urllib.error.HTTPError as e:
        data = e.read()
        try:
            return e.code, json.loads(data)
        except ValueError:
            return e.code, data


def check(ok, what):
    print(f"   {'PASS' if ok else 'FAIL'}  {what}")
    if not ok:
        failures.append(what)


def sh(*args):
    return subprocess.run(args, capture_output=True, text=True)


def psql(db, sql):
    return sh(*COMPOSE, "exec", "-T", "postgres", "psql", "-U", "finverse", "-d", db, "-tA", "-c", sql)


def wait(pred, what, secs=40):
    end = time.time() + secs
    while time.time() < end:
        v = pred()
        if v:
            return v
        time.sleep(1)
    failures.append(f"timed out waiting for {what}")
    print(f"   FAIL  timed out waiting for {what}")
    return None


def login(email, password):
    s, b = call("POST", f"{AUTH}/api/v1/auth/login", {"email": email, "password": password})
    assert s == 200, (s, b)
    return b["data"]["accessToken"]


def make_admin():
    email = f"admin{int(time.time())}@example.com"
    call("POST", f"{AUTH}/api/v1/auth/register", {"email": email, "password": PW})
    tok = None
    for _ in range(15):
        found = re.findall(re.escape(email) + r".*?token=([A-Za-z0-9_\-]+)", sh(*COMPOSE, "logs", "auth-service").stdout)
        if found:
            tok = found[-1]
            break
        time.sleep(1)
    assert tok, "no verification token in auth-service logs"
    call("POST", f"{AUTH}/api/v1/auth/verify-email", {"token": tok})
    psql("auth_db", f"UPDATE users SET role='admin' WHERE email='{email}'")
    return login(email, PW)


def pay(token, src, dst, amount):
    s, b = call("POST", f"{PAY}/api/v1/payments",
                {"fromAccountId": src["accountId"], "toAccountNumber": dst["accountNumber"], "amountMinor": amount},
                token, {"Idempotency-Key": str(uuid.uuid4())})
    return b["data"] if s in (200, 201, 202) else {"status": f"HTTP {s}", "failureReason": str(b)[:120]}


def balance(token, acct):
    return call("GET", f"{ACCOUNT}/api/v1/accounts/{acct['accountId']}/balance", token=token)[1]["data"]["ledgerBalanceMinor"]


def code_of(body):
    return body.get("error", {}).get("code") if isinstance(body, dict) else None


def main():
    fx = json.loads((ROOT / "loadtest/fixtures.json").read_text())["users"]
    a, b = fx[0], fx[1]
    tok_a = login(a["email"], a["password"])
    tok_b = login(b["email"], b["password"])
    admin = make_admin()
    acct_a = a["accountId"]

    print("1. privileged routes refuse anonymous callers and customers")
    for what, method, url, body in (
        ("dashboard", "GET", f"{ADMIN}/api/v1/admin/dashboard", None),
        ("freeze", "POST", f"{ADMIN}/api/v1/admin/accounts/{acct_a}/freeze", {"reason": "not allowed for me"}),
        ("action list", "GET", f"{ADMIN}/api/v1/admin/actions", None),
    ):
        s, _ = call(method, url, body)
        check(s == 401, f"{what}: anonymous -> 401 (got {s})")
        s, _ = call(method, url, body, tok_a)
        check(s == 403, f"{what}: customer -> 403 (got {s})")
    s, body = call("POST", f"{ACCOUNT}/internal/admin/accounts/{acct_a}/freeze", {})
    check(s == 401, f"Account's internal admin route needs a service token (got {s})")
    check(call("GET", f"{ACCOUNT}/api/v1/accounts/{acct_a}", token=tok_a)[1]["data"]["status"] == "ACTIVE", "account is untouched so far")

    print("2. dashboard")
    s, body = call("GET", f"{ADMIN}/api/v1/admin/dashboard", token=admin)
    d = body.get("data", {})
    check(s == 200, f"admin gets the dashboard (got {s})")
    check(d.get("fraud", {}).get("available") is True, "fraud panel available")
    check(d.get("payments", {}).get("available") is True and d.get("ops", {}).get("available") is True, "metrics panels available")
    print(f"   payments(1h)={d.get('payments', {}).get('data')} ops={d.get('ops', {}).get('data')}")

    print("3. admin freeze stops payments, customer cannot lift it, admin can")
    s, body = call("POST", f"{ADMIN}/api/v1/admin/accounts/{acct_a}/freeze", {"reason": "demo: suspected account takeover"}, admin)
    check(s == 200 and body["data"]["status"] == "SUCCEEDED", f"freeze -> SUCCEEDED (got {s} {body.get('data', {}).get('status')})")
    act_id = body.get("data", {}).get("id")
    check(call("GET", f"{ACCOUNT}/api/v1/accounts/{acct_a}", token=tok_a)[1]["data"]["status"] == "FROZEN", "account is FROZEN")
    s, body = call("POST", f"{PAY}/api/v1/payments",
                   {"fromAccountId": acct_a, "toAccountNumber": b["accountNumber"], "amountMinor": 100},
                   tok_a, {"Idempotency-Key": str(uuid.uuid4())})
    check(s == 422 and code_of(body) == "ACCOUNT_FROZEN", f"a payment from the frozen account is refused (got {s} {code_of(body)})")
    s, body = call("GET", f"{WALLET}/api/v1/wallets/me", token=tok_a)
    if s == 404:
        s, body = call("POST", f"{WALLET}/api/v1/wallets", {"fundingAccountId": acct_a}, tok_a)
    check(s in (200, 201), f"customer A has a wallet funded by the frozen account (got {s})")
    s, body = call("POST", f"{WALLET}/api/v1/wallets/me/topup", {"amountMinor": 100}, tok_a, {"Idempotency-Key": str(uuid.uuid4())})
    check(s == 422 and code_of(body) == "ACCOUNT_FROZEN", f"a wallet top-up from the frozen account is refused (got {s} {code_of(body)})")
    s, body = call("POST", f"{ACCOUNT}/api/v1/accounts/{acct_a}/unfreeze", {}, tok_a)
    check(s == 403 and code_of(body) == "FROZEN_BY_ADMIN", f"customer cannot lift an admin freeze (got {s} {code_of(body)})")
    s, body = call("POST", f"{ADMIN}/api/v1/admin/accounts/{acct_a}/freeze", {"reason": "demo: second freeze attempt"}, admin)
    check(s == 409 and code_of(body) == "DOWNSTREAM_ACCOUNT_ALREADY_FROZEN", f"double freeze -> 409 with downstream code (got {s} {code_of(body)})")
    s, body = call("POST", f"{ADMIN}/api/v1/admin/accounts/{acct_a}/unfreeze", {"reason": "demo: customer verified by phone"}, admin)
    check(s == 200 and body["data"]["status"] == "SUCCEEDED", "admin unfreeze -> SUCCEEDED")
    unfreeze_id = body.get("data", {}).get("id")
    p = wait(lambda: (lambda r: r if r["status"] == "COMPLETED" else None)(pay(tok_a, a, b, 100)), "payment to succeed after unfreeze")
    check(p is not None, "payments work again after the admin unfreeze")
    s, body = call("POST", f"{WALLET}/api/v1/wallets/me/topup", {"amountMinor": 100}, tok_a, {"Idempotency-Key": str(uuid.uuid4())})
    check(s == 200, f"wallet top-up works again after the unfreeze (got {s} {code_of(body)})")
    s, body = call("POST", f"{ADMIN}/api/v1/admin/accounts/{acct_a}/freeze", {"reason": "x"}, admin)
    check(s == 400 and code_of(body) == "INVALID_REASON", f"a 1-character reason is refused (got {s})")

    print("4. audit trail and immutability")
    def audited(event_type, action_id):
        s, b2 = call("GET", f"{AUDIT}/api/v1/audit/records?eventType={event_type}&limit=200", token=admin)
        return s == 200 and any(r["payload"].get("actionId") == action_id for r in b2["data"])
    for aid, label in ((act_id, "freeze"), (unfreeze_id, "unfreeze")):
        for et in ("AdminActionRequested", "AdminActionResolved"):
            check(bool(wait(lambda: audited(et, aid), f"{et} for {label}", 120)), f"{et} reached Audit ({label})")
    s, body = call("GET", f"{ADMIN}/api/v1/admin/actions?limit=20", token=admin)
    mine = [x for x in body.get("data", []) if x["id"] == act_id]
    check(len(mine) == 1 and mine[0]["status"] == "SUCCEEDED" and mine[0]["reason"].startswith("demo:"), "action is listed with its reason and outcome")
    for stmt in (f"UPDATE admin_actions SET reason='tampered reason' WHERE id='{act_id}'",
                 f"UPDATE admin_actions SET status='FAILED' WHERE id='{act_id}'",
                 f"DELETE FROM admin_actions WHERE id='{act_id}'"):
        r = psql("admin_db", stmt)
        check("admin action" in r.stderr or "never deleted" in r.stderr, f"{stmt.split()[0]} on a finished action is rejected by the database")

    print("5. audit-then-act: no record, no action")
    psql("admin_db", "ALTER TABLE admin_actions RENAME TO admin_actions_offline")
    try:
        s, body = call("POST", f"{ADMIN}/api/v1/admin/accounts/{acct_a}/freeze", {"reason": "demo: must not happen"}, admin)
        check(s == 503 and code_of(body) == "AUDIT_UNAVAILABLE", f"refused with AUDIT_UNAVAILABLE (got {s} {code_of(body)})")
    finally:
        psql("admin_db", "ALTER TABLE admin_actions_offline RENAME TO admin_actions")
    check(call("GET", f"{ACCOUNT}/api/v1/accounts/{acct_a}", token=tok_a)[1]["data"]["status"] == "ACTIVE", "the account was NOT frozen")

    print("6. fraud case resolution through Admin")
    p = pay(tok_a, a, b, 3_000_000)
    case = wait(lambda: next((c for c in call("GET", f"{FRAUD}/api/v1/fraud/cases?status=OPEN", token=admin)[1]["data"]
                              if c["paymentId"] == p.get("id")), None), "fraud case for a review payment")
    if case:
        s, body = call("POST", f"{ADMIN}/api/v1/admin/fraud/cases/{case['id']}/resolve", {"outcome": "DISMISSED", "reason": "demo: legitimate rent payment"}, admin)
        check(s == 200 and body["data"]["status"] == "SUCCEEDED", f"resolve through Admin (got {s})")
        resolved = next(c for c in call("GET", f"{FRAUD}/api/v1/fraud/cases?status=RESOLVED&limit=200", token=admin)[1]["data"] if c["id"] == case["id"])
        check(resolved["resolvedBy"] == admin_sub(admin), "Fraud recorded the administrator, not a service, as the resolver")
        s, body = call("POST", f"{ADMIN}/api/v1/admin/fraud/cases/{case['id']}/resolve", {"outcome": "DISMISSED", "reason": "demo: resolving twice"}, admin)
        check(s == 409 and code_of(body) == "DOWNSTREAM_CASE_ALREADY_RESOLVED", f"second resolve -> 409 passed through (got {s} {code_of(body)})")
    ghost = str(uuid.uuid4())
    s, body = call("POST", f"{ADMIN}/api/v1/admin/customers/{ghost}/kyc-review", {"approve": True, "tier": 2, "reason": "demo: nonexistent"}, admin)
    check(s == 404, f"KYC review of an unknown customer -> 404 passed through (got {s} {code_of(body)})")
    s, body = call("POST", f"{ADMIN}/api/v1/admin/customers/{ghost}/kyc-review", {"approve": True, "tier": 9, "reason": "demo: bad tier"}, admin)
    check(s == 400, "an approval with tier 9 is refused")
    failed = psql("admin_db", "SELECT count(*) FROM admin_actions WHERE status='FAILED' AND action_type='KYC_REVIEW'").stdout.strip()
    check(failed.isdigit() and int(failed) >= 1, "the failed KYC attempt is recorded as FAILED, not lost")

    print("7. statement: closing balance equals the live balance")
    today = dt.datetime.now(dt.timezone.utc).date()
    rng = {"accountId": acct_a, "from": str(today - dt.timedelta(days=30)), "to": str(today)}
    s, body = call("POST", f"{REPORT}/api/v1/reports/statements", rng, tok_a)
    check(s == 202 and body["data"]["status"] == "QUEUED", f"statement accepted and queued (got {s})")
    rid = body.get("data", {}).get("id")
    rep = wait(lambda: (lambda r: r if r["status"] in ("READY", "FAILED") else None)(call("GET", f"{REPORT}/api/v1/reports/{rid}", token=tok_a)[1]["data"]), "report to finish", 120)
    check(bool(rep) and rep["status"] == "READY", f"report is READY (got {rep and rep['status']} {rep and rep.get('failureCode')})")
    if rep and rep["status"] == "READY":
        s, data = call("GET", f"{REPORT}/api/v1/reports/{rid}/download", token=tok_a, raw=True)
        rows = list(csv.reader(io.StringIO(data.decode())))
        check(s == 200 and rows[0][:3] == ["date_utc", "transaction_id", "type"], "downloaded CSV has the expected header")
        closing = next(r for r in rows if r[2] == "CLOSING_BALANCE")
        opening = next(r for r in rows if r[2] == "OPENING_BALANCE")
        print(f"   {len(rows) - 3} transactions, opening {opening[5]}, closing {closing[5]}")
        live = wait(lambda: balance(tok_a, a) == round(float(closing[5]) * 100), "projection to match the statement", 30)
        check(bool(live), f"closing balance {closing[5]} equals the live ledger balance ({balance(tok_a, a) / 100:.2f})")
        run = int(round(float(opening[5]) * 100))
        ok = True
        for r in rows[2:-1]:
            run += int(round(float(r[4] or 0) * 100)) - int(round(float(r[3] or 0) * 100))
            ok &= run == int(round(float(r[5]) * 100))
        check(ok, "every row's running balance is consistent")
        s, hdrs = 0, None
        req = urllib.request.Request(f"{REPORT}/api/v1/reports/{rid}/download", headers={"Authorization": f"Bearer {tok_a}"})
        with urllib.request.urlopen(req) as r:
            check("attachment" in r.headers.get("Content-Disposition", "") and "text/csv" in r.headers.get("Content-Type", "")
                  and r.headers.get("Cache-Control") == "no-store", "download is an attachment, text/csv, no-store")

    print("8. other customers get 404, not 403")
    s, body = call("POST", f"{REPORT}/api/v1/reports/statements", rng, tok_b)
    check(s == 404, f"customer B cannot request a statement for A's account (got {s})")
    check(call("GET", f"{REPORT}/api/v1/reports/{rid}", token=tok_b)[0] == 404, "customer B cannot see A's report")
    check(call("GET", f"{REPORT}/api/v1/reports/{rid}/download", token=tok_b)[0] == 404, "customer B cannot download A's report")
    check(call("GET", f"{REPORT}/api/v1/reports/{rid}/download")[0] == 401, "anonymous download -> 401")
    check(call("GET", f"{REPORT}/api/v1/reports/..%2F..%2Fetc%2Fpasswd/download", token=tok_a)[0] == 404, "path-traversal id -> 404")

    print("9. validation, scopes, fraud summary")
    for label, patch in (("future end date", {"to": str(today + dt.timedelta(days=1))}),
                         ("range over 366 days", {"from": str(today - dt.timedelta(days=400))}),
                         ("reversed range", {"from": str(today), "to": str(today - dt.timedelta(days=2))}),
                         ("not a date", {"from": "yesterday"}),
                         ("bad account id", {"accountId": "../etc"})):
        s, _ = call("POST", f"{REPORT}/api/v1/reports/statements", {**rng, **patch}, tok_a)
        check(s == 400, f"{label} -> 400 (got {s})")
    check(call("POST", f"{REPORT}/api/v1/reports/fraud-summary", {}, tok_a)[0] == 403, "customer cannot request the fraud summary")
    s, body = call("POST", f"{REPORT}/api/v1/reports/fraud-summary", {}, admin)
    check(s == 202, f"admin can request it (got {s})")
    fid = body.get("data", {}).get("id")
    rep = wait(lambda: (lambda r: r if r["status"] in ("READY", "FAILED") else None)(call("GET", f"{REPORT}/api/v1/reports/{fid}", token=admin)[1]["data"]), "fraud summary", 60)
    if rep and rep["status"] == "READY":
        text = call("GET", f"{REPORT}/api/v1/reports/{fid}/download", token=admin, raw=True)[1].decode()
        check(text.startswith("case_id,account_id"), "fraud summary CSV downloaded")
        check(call("GET", f"{REPORT}/api/v1/reports/{fid}/download", token=tok_a)[0] == 404, "a customer cannot download the admin's report")
    else:
        check(False, "fraud summary became READY")

    print()
    if failures:
        print(f"{len(failures)} check(s) FAILED:")
        for f in failures:
            print("  -", f)
        sys.exit(1)
    print("OK: all admin and reporting checks passed")


def admin_sub(token):
    import base64
    payload = token.split(".")[1]
    return json.loads(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)))["sub"]


main()
