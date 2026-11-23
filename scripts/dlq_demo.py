"""End-to-end demo of retry -> DLQ -> replay for Notification Service.

    python scripts/dlq_demo.py        (needs the stack up and loadtest/setup.py run once)

1. simulate an email-provider outage
2. make a real payment; its PaymentCompleted event fails delivery, is retried
   with backoff, then dead-lettered (payment itself still succeeds: NFR-AVAIL-2)
3. inject a malformed event; it is dead-lettered immediately (poison message)
4. end the outage, replay the dead letter, watch it get delivered
"""
import json, re, subprocess, sys, time, urllib.request, urllib.error, uuid, pathlib

ROOT = pathlib.Path(__file__).resolve().parent.parent
AUTH, PAY, NOTIF = "http://localhost:8081", "http://localhost:8085", "http://localhost:8086"
COMPOSE = ["docker", "compose", "-f", str(ROOT / "docker/docker-compose.yml")]
PW = "Adm!nPassw0rd-9x"


def call(method, url, body=None, token=None, headers=None):
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
        raise SystemExit(f"{method} {url} -> {e.code}: {e.read().decode()[:300]}")


def sh(*args):
    return subprocess.run(args, capture_output=True, text=True).stdout


def make_admin():
    email = f"admin{int(time.time())}@example.com"
    call("POST", f"{AUTH}/api/v1/auth/register", {"email": email, "password": PW})
    tok = None
    for _ in range(15):
        found = re.findall(re.escape(email) + r".*?token=([A-Za-z0-9_\-]+)", sh(*COMPOSE, "logs", "auth-service"))
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


def stats(admin):
    return call("GET", f"{NOTIF}/api/v1/notifications/admin/stats", token=admin)["data"]


def wait(pred, what, secs=40):
    end = time.time() + secs
    while time.time() < end:
        v = pred()
        if v:
            return v
        time.sleep(1)
    raise SystemExit(f"timed out waiting for {what}")


def main():
    fx = json.loads((ROOT / "loadtest/fixtures.json").read_text())["users"]
    admin = make_admin()
    user_tok = call("POST", f"{AUTH}/api/v1/auth/login", {"email": fx[0]["email"], "password": fx[0]["password"]})["data"]["accessToken"]
    base = stats(admin)["streams"]["PaymentCompleted"]["dlqDepth"]

    print("1. provider outage ON")
    call("PUT", f"{NOTIF}/api/v1/notifications/admin/provider-outage", {"failing": True}, admin)

    print("2. paying while notifications are down")
    t0 = time.time()
    p = call("POST", f"{PAY}/api/v1/payments",
             {"fromAccountId": fx[0]["accountId"], "toAccountNumber": fx[1]["accountNumber"], "amountMinor": 500},
             user_tok, {"Idempotency-Key": str(uuid.uuid4())})["data"]
    print(f"   payment {p['id']} status={p['status']} in {(time.time()-t0)*1000:.0f} ms  <- unaffected by the outage")

    wait(lambda: stats(admin)["streams"]["PaymentCompleted"]["dlqDepth"] > base, "dead-letter")
    s = stats(admin)["streams"]["PaymentCompleted"]
    print(f"   dead-lettered after {time.time()-t0:.1f}s; stats: {s}")

    print("3. injecting a malformed (poison) event")
    sh(*COMPOSE, "exec", "-T", "redis", "redis-cli", "XADD", "PaymentFailed", "*", "envelope",
       json.dumps({"eventId": str(uuid.uuid4()), "eventType": "PaymentFailed", "data": "garbage"}))
    wait(lambda: stats(admin)["streams"]["PaymentFailed"]["dlqDepth"] >= 1, "poison dead-letter", 15)
    print("   poison message dead-lettered on first attempt (no retries)")

    dlq = call("GET", f"{NOTIF}/api/v1/notifications/admin/dlq", token=admin)["data"]
    for e in dlq:
        print(f"   DLQ {e['stream']} id={e['id']} attempts={e['attempts']} reason={e['reason']!r}")

    print("4. provider outage OFF; replaying the dead-lettered PaymentCompleted")
    call("PUT", f"{NOTIF}/api/v1/notifications/admin/provider-outage", {"failing": False}, admin)
    target = [e for e in dlq if e["stream"] == "PaymentCompleted"][-1]
    delivered0 = stats(admin)["delivered"]
    call("POST", f"{NOTIF}/api/v1/notifications/admin/dlq/PaymentCompleted/{target['id']}/replay", token=admin)
    wait(lambda: stats(admin)["delivered"] > delivered0, "delivery after replay")
    print(f"   delivered after replay; final stats: {json.dumps(stats(admin))}")
    print("OK")


main()
