"""End-to-end demo of Fraud (sync check) and Audit (append-only) behaviour.

    python scripts/fraud_audit_demo.py     (stack up; loadtest/setup.py run once)

1. a payment above the single-transfer limit is denied before any money moves
2. a large-but-allowed payment completes and opens a REVIEW case; an analyst resolves it
3. every event lands in Audit; the database rejects UPDATE/DELETE on audit records
4. with Fraud Service stopped, payments fail closed (and recover when it returns)
"""
import json, re, subprocess, sys, time, urllib.request, urllib.error, uuid, pathlib

ROOT = pathlib.Path(__file__).resolve().parent.parent
AUTH, PAY, FRAUD, AUDIT = "http://localhost:8081", "http://localhost:8085", "http://localhost:8088", "http://localhost:8087"
COMPOSE = ["docker", "compose", "-f", str(ROOT / "docker/docker-compose.yml")]
PW = "Adm!nPassw0rd-9x"


def call(method, url, body=None, token=None, headers=None, expect_error=False):
    req = urllib.request.Request(url, method=method, data=json.dumps(body).encode() if body is not None else None)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    try:
        with urllib.request.urlopen(req) as r:
            raw = r.read()
            return json.loads(raw) if raw else {}
    except urllib.error.HTTPError as e:
        if expect_error:
            return {"_status": e.code, "_body": e.read().decode()}
        raise SystemExit(f"{method} {url} -> {e.code}: {e.read().decode()[:300]}")


def sh(*args):
    return subprocess.run(args, capture_output=True, text=True)


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
    if not tok:
        raise SystemExit("no verification token in auth-service logs for " + email)
    call("POST", f"{AUTH}/api/v1/auth/verify-email", {"token": tok})
    sh(*COMPOSE, "exec", "-T", "postgres", "psql", "-U", "finverse", "-d", "auth_db", "-c",
       f"UPDATE users SET role='admin' WHERE email='{email}'")
    return call("POST", f"{AUTH}/api/v1/auth/login", {"email": email, "password": PW})["data"]["accessToken"]


def pay(token, src, dst, amount):
    return call("POST", f"{PAY}/api/v1/payments",
                {"fromAccountId": src["accountId"], "toAccountNumber": dst["accountNumber"], "amountMinor": amount},
                token, {"Idempotency-Key": str(uuid.uuid4())})["data"]


def wait(pred, what, secs=30):
    end = time.time() + secs
    while time.time() < end:
        v = pred()
        if v:
            return v
        time.sleep(1)
    raise SystemExit(f"timed out waiting for {what}")


def main():
    fx = json.loads((ROOT / "loadtest/fixtures.json").read_text())["users"]
    a, b = fx[0], fx[1]
    tok = call("POST", f"{AUTH}/api/v1/auth/login", {"email": a["email"], "password": a["password"]})["data"]["accessToken"]
    admin = make_admin()

    print("1. payment above the single-transfer limit (INR 60,000)")
    denied = pay(tok, a, b, 6_000_000)
    p = denied
    print(f"   status={p['status']} reason={p.get('failureReason')!r}")
    assert p["status"] == "FAILED" and p["failureReason"].startswith("FRAUD_DENIED"), p

    print("2. large-but-allowed payment (INR 30,000) -> completes, opens a REVIEW case")
    p = pay(tok, a, b, 3_000_000)
    print(f"   status={p['status']}")
    assert p["status"] == "COMPLETED", p
    case = wait(lambda: next((c for c in call("GET", f"{FRAUD}/api/v1/fraud/cases?status=OPEN", token=admin)["data"]
                              if c["paymentId"] == p["id"]), None), "fraud case for the review payment")
    print(f"   case {case['id'][:8]} verdict={case['verdict']} rule={case['rule']} riskScore={case['riskScore']}")
    res = call("POST", f"{FRAUD}/api/v1/fraud/cases/{case['id']}/resolve", {"outcome": "DISMISSED"}, admin)["data"]
    print(f"   analyst resolved: {res['status']} / {res['resolution']}")

    print("3. audit trail")
    # Look up each specific event by type + payload id (the newest-N window is
    # unreliable while Audit is still draining a backlog of older events).
    def audit_has(event_type, field, value):
        recs = call("GET", f"{AUDIT}/api/v1/audit/records?eventType={event_type}&limit=200", token=admin)["data"]
        return any(isinstance(r["payload"], dict) and r["payload"].get(field) == value for r in recs)
    for et, field, value in (("PaymentFailed", "paymentId", denied["id"]), ("PaymentCompleted", "paymentId", p["id"]),
                             ("FraudCaseOpened", "caseId", case["id"]), ("FraudCaseResolved", "caseId", case["id"])):
        wait(lambda: audit_has(et, field, value), f"audit record {et}", 120)
        print(f"   audited: {et} ({field}={value[:8]})")
    for stmt in ("UPDATE audit_records SET event_type='X' WHERE id=(SELECT min(id) FROM audit_records)",
                 "DELETE FROM audit_records WHERE id=(SELECT min(id) FROM audit_records)",
                 "TRUNCATE audit_records"):
        r = sh(*COMPOSE, "exec", "-T", "postgres", "psql", "-U", "finverse", "-d", "audit_db", "-c", stmt)
        assert "append-only" in r.stderr, f"{stmt!r} was NOT rejected: {r.stdout} {r.stderr}"
        print(f"   {stmt.split()[0]:8} -> rejected by the database")

    print("4. Fraud Service down -> fail closed")
    sh(*COMPOSE, "stop", "fraud-service")
    t0 = time.time()
    p = pay(tok, a, b, 100)
    print(f"   status={p['status']} reason={p.get('failureReason')!r} (answered in {(time.time()-t0)*1000:.0f} ms)")
    assert p["status"] == "FAILED" and "FRAUD_CHECK_UNAVAILABLE" in p["failureReason"], p
    sh(*COMPOSE, "start", "fraud-service")
    wait(lambda: pay(tok, a, b, 100)["status"] == "COMPLETED", "payments to recover after Fraud Service restart", 60)
    print("   Fraud Service back -> payments succeed again")
    print("OK")


main()
