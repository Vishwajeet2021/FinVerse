"""End-to-end demo of payment rails, scheduled payments and standing instructions.

    python scripts/rails_demo.py     (stack up; loadtest/setup.py run once)

1. INTERNAL completes synchronously; UPI/IMPS/NEFT/RTGS return PROCESSING then settle
2. rail limits are enforced (UPI cap, RTGS minimum)
3. the same Idempotency-Key never moves money twice
4. ownership: paying from an account you do not hold is refused
5. a scheduled payment executes at its time; a cancelled one never does
6. a standing instruction fires repeatedly, can be paused/resumed
Money conservation is checked at the end (balance deltas across users sum to 0).
"""
import json, time, urllib.request, urllib.error, uuid, pathlib
from datetime import datetime, timezone, timedelta

ROOT = pathlib.Path(__file__).resolve().parent.parent
AUTH, PAY, ACCT = "http://localhost:8081", "http://localhost:8085", "http://localhost:8083"


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
        body_txt = e.read().decode()
        if expect_error:
            return {"_status": e.code, "_body": body_txt}
        raise SystemExit(f"{method} {url} -> {e.code}: {body_txt[:300]}")


def login(u):
    return call("POST", f"{AUTH}/api/v1/auth/login", {"email": u["email"], "password": u["password"]})["data"]["accessToken"]


def balance(tok, u):
    return call("GET", f"{ACCT}/api/v1/accounts/{u['accountId']}/balance", token=tok)["data"]["ledgerBalanceMinor"]


def pay(tok, src, dst, amount, rail=None, key=None, expect_error=False):
    body = {"fromAccountId": src["accountId"], "toAccountNumber": dst["accountNumber"], "amountMinor": amount}
    if rail:
        body["rail"] = rail
    r = call("POST", f"{PAY}/api/v1/payments", body, tok, {"Idempotency-Key": key or str(uuid.uuid4())}, expect_error)
    return r if expect_error else r["data"]


def wait(pred, what, secs=60):
    end = time.time() + secs
    while time.time() < end:
        v = pred()
        if v:
            return v
        time.sleep(0.5)
    raise SystemExit(f"timed out waiting for {what}")


def iso(dt):
    return dt.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def main():
    fx = json.loads((ROOT / "loadtest/fixtures.json").read_text())["users"]
    a, b, c = fx[0], fx[1], fx[2]
    ta, tb, tc = login(a), login(b), login(c)
    start = [balance(ta, a), balance(tb, b), balance(tc, c)]

    print("1. rail lifecycles (INR 100 each unless noted)")
    for rail in ("INTERNAL", "UPI", "IMPS", "NEFT"):
        t0 = time.time()
        p = pay(ta, a, b, 10_000, rail)
        first = p["status"]
        if rail == "INTERNAL":
            assert first == "COMPLETED", p
        else:
            assert first == "PROCESSING" and p.get("settleAt"), p
            p = wait(lambda: (lambda g: g if g["status"] != "PROCESSING" else None)(
                call("GET", f"{PAY}/api/v1/payments/{p['id']}", token=ta)["data"]), f"{rail} to settle")
        print(f"   {rail:8} {first:10} -> {p['status']:9} after {time.time()-t0:4.1f}s")
        assert p["status"] == "COMPLETED", p

    print("2. rail limits")
    r = pay(ta, a, b, 10_000_001, "UPI", expect_error=True)
    print(f"   UPI > 1 lakh        -> {r['_status']} {json.loads(r['_body'])['error']['code']}")
    assert r["_status"] == 400
    r = pay(ta, a, b, 10_000, "RTGS", expect_error=True)
    print(f"   RTGS < 2 lakh       -> {r['_status']} {json.loads(r['_body'])['error']['code']}")
    assert r["_status"] == 400
    r = pay(ta, a, b, 10_000, "CARRIER_PIGEON", expect_error=True)
    print(f"   unknown rail        -> {r['_status']} {json.loads(r['_body'])['error']['code']}")
    assert r["_status"] == 400

    print("3. idempotency on an async rail")
    k = str(uuid.uuid4())
    p1 = pay(ta, a, b, 5_000, "IMPS", key=k)
    p2 = pay(ta, a, b, 5_000, "IMPS", key=k)
    print(f"   same key twice -> same payment: {p1['id'] == p2['id']}")
    assert p1["id"] == p2["id"]
    wait(lambda: call("GET", f"{PAY}/api/v1/payments/{p1['id']}", token=ta)["data"]["status"] == "COMPLETED", "IMPS settle")

    print("4. ownership")
    r = call("POST", f"{PAY}/api/v1/payments",
             {"fromAccountId": b["accountId"], "toAccountNumber": c["accountNumber"], "amountMinor": 100},
             ta, {"Idempotency-Key": str(uuid.uuid4())}, expect_error=True)
    print(f"   user A paying from user B's account -> {r['_status']} {json.loads(r['_body'])['error']['code']}")
    assert r["_status"] == 403

    print("5. scheduled payments")
    when = datetime.now(timezone.utc) + timedelta(seconds=4)
    s_run = call("POST", f"{PAY}/api/v1/payments/scheduled", {"fromAccountId": a["accountId"], "toAccountNumber": b["accountNumber"],
                 "amountMinor": 7_000, "rail": "INTERNAL", "runAt": iso(when)}, ta)["data"]
    s_cancel = call("POST", f"{PAY}/api/v1/payments/scheduled", {"fromAccountId": a["accountId"], "toAccountNumber": b["accountNumber"],
                    "amountMinor": 9_000, "runAt": iso(when)}, ta)["data"]
    call("DELETE", f"{PAY}/api/v1/payments/scheduled/{s_cancel['id']}", token=ta)
    past = call("POST", f"{PAY}/api/v1/payments/scheduled", {"fromAccountId": a["accountId"], "toAccountNumber": b["accountNumber"],
                "amountMinor": 100, "runAt": iso(datetime.now(timezone.utc) - timedelta(hours=1))}, ta, expect_error=True)
    print(f"   scheduled in the past -> {past['_status']}")
    assert past["_status"] == 400
    done = wait(lambda: next((s for s in call("GET", f"{PAY}/api/v1/payments/scheduled", token=ta)["data"]
                              if s["id"] == s_run["id"] and s["status"] == "EXECUTED"), None), "scheduled payment to execute")
    pay_row = call("GET", f"{PAY}/api/v1/payments/{done['paymentId']}", token=ta)["data"]
    print(f"   executed -> payment {pay_row['id'][:8]} {pay_row['status']}")
    assert pay_row["status"] == "COMPLETED"
    cancelled = next(s for s in call("GET", f"{PAY}/api/v1/payments/scheduled", token=ta)["data"] if s["id"] == s_cancel["id"])
    print(f"   cancelled one stayed {cancelled['status']} (paymentId={cancelled.get('paymentId')})")
    assert cancelled["status"] == "CANCELLED" and not cancelled.get("paymentId")

    print("6. standing instruction (DAILY, first run in 3s)")
    si = call("POST", f"{PAY}/api/v1/payments/standing-instructions", {"fromAccountId": a["accountId"], "toAccountNumber": c["accountNumber"],
              "amountMinor": 1_000, "frequency": "DAILY", "firstRunAt": iso(datetime.now(timezone.utc) + timedelta(seconds=3))}, ta)["data"]
    got = wait(lambda: next((s for s in call("GET", f"{PAY}/api/v1/payments/standing-instructions", token=ta)["data"]
                             if s["id"] == si["id"] and s.get("lastPaymentId")), None), "first standing run")
    nxt = datetime.fromisoformat(got["nextRunAt"].replace("Z", "+00:00"))
    gap = (nxt - datetime.fromisoformat(si["nextRunAt"].replace("Z", "+00:00"))).total_seconds()
    print(f"   fired once; next run advanced by {gap/3600:.1f}h")
    assert abs(gap - 86400) < 5
    call("POST", f"{PAY}/api/v1/payments/standing-instructions/{si['id']}/pause", token=ta)
    paused = next(s for s in call("GET", f"{PAY}/api/v1/payments/standing-instructions", token=ta)["data"] if s["id"] == si["id"])
    print(f"   paused -> {paused['status']}")
    assert paused["status"] == "PAUSED"
    call("DELETE", f"{PAY}/api/v1/payments/standing-instructions/{si['id']}", token=ta)
    r = call("POST", f"{PAY}/api/v1/payments/standing-instructions/{si['id']}/resume", token=ta, expect_error=True)
    print(f"   resume after cancel -> {r.get('_status')} (cancel is terminal)")
    assert r.get("_status") in (409, 404)

    print("7. money conservation")
    time.sleep(3)
    end = [balance(ta, a), balance(tb, b), balance(tc, c)]
    deltas = [e - s for e, s in zip(end, start)]
    print(f"   balance deltas (minor): {deltas}  sum={sum(deltas)}")
    assert sum(deltas) == 0, deltas
    print("OK")


main()
