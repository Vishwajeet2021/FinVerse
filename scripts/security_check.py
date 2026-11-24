"""Live security regression checks against the running stack (docs/22 section 5).

    python scripts/security_check.py      (stack up; loadtest/setup.py run once)

Every check states the property it proves. A failure exits non-zero, so this can gate CI.
It sends deliberately hostile input, so run it only against a local or test stack.
"""
import base64
import json
import os
import pathlib
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid

ROOT = pathlib.Path(__file__).resolve().parent.parent
AUTH, CUST, ACCT, PAY, TXN = ("http://localhost:8081", "http://localhost:8082", "http://localhost:8083",
                              "http://localhost:8085", "http://localhost:8091")
COMPOSE = ["docker", "compose", "-f", str(ROOT / "docker/docker-compose.yml")]
results = []


def call(method, url, body=None, token=None, headers=None, raw=None):
    data = raw if raw is not None else (json.dumps(body).encode() if body is not None else None)
    req = urllib.request.Request(url, method=method, data=data)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            return r.status, dict(r.headers), r.read()
    except urllib.error.HTTPError as e:
        return e.code, dict(e.headers), e.read()


def check(name, ok, detail=""):
    results.append(ok)
    print(f"  [{'PASS' if ok else 'FAIL'}] {name}{(' - ' + detail) if detail and not ok else ''}")


def login(u):
    for _ in range(4):
        s, h, b = call("POST", f"{AUTH}/api/v1/auth/login", {"email": u["email"], "password": u["password"]})
        if s != 429:  # a previous run of this script trips the login limit on purpose; wait it out
            return json.loads(b)["data"]["accessToken"]
        time.sleep(int({k.lower(): v for k, v in h.items()}.get("retry-after", "30")) + 1)
    raise SystemExit("login stayed rate limited")


def main():
    fx = json.loads((ROOT / "loadtest/fixtures.json").read_text())["users"]
    a, b = fx[0], fx[1]
    ta, tb = login(a), login(b)

    print("1. Response headers")
    s, h, _ = call("GET", f"{PAY}/health")
    h = {k.lower(): v for k, v in h.items()}
    check("nosniff", h.get("x-content-type-options") == "nosniff")
    check("not frameable", h.get("x-frame-options") == "DENY")
    check("CSP present", "default-src 'none'" in h.get("content-security-policy", ""))
    check("API responses are not cacheable", h.get("cache-control") == "no-store")

    print("2. Authentication required")
    s, _, _ = call("GET", f"{ACCT}/api/v1/accounts")
    check("no token -> 401", s == 401, str(s))
    s, _, _ = call("GET", f"{ACCT}/api/v1/accounts", token="not.a.jwt")
    check("garbage token -> 401", s == 401, str(s))
    none_alg = (base64.urlsafe_b64encode(b'{"alg":"none","typ":"JWT"}').rstrip(b"=").decode() + "." +
                base64.urlsafe_b64encode(json.dumps({"sub": a["email"], "scope": "payments:write"}).encode()).rstrip(b"=").decode() + ".")
    s, _, _ = call("GET", f"{ACCT}/api/v1/accounts", token=none_alg)
    check("alg=none token -> 401", s == 401, str(s))

    print("3. Internal endpoints need a service token")
    s, _, _ = call("POST", f"{TXN}/internal/daily-closing/run")
    check("daily closing without token -> 401", s == 401, str(s))
    s, _, _ = call("GET", f"{ACCT}/internal/accounts/by-number/{a['accountNumber']}")
    check("account lookup without token -> 401", s == 401, str(s))
    s, _, _ = call("GET", f"{CUST}/internal/customers/by-user/{uuid.uuid4()}/kyc-tier")
    check("kyc-tier lookup without token -> 401", s == 401, str(s))
    s, _, _ = call("POST", f"{TXN}/internal/daily-closing/run", headers={"X-Service-Token": "payment-service.1.deadbeef"})
    check("forged service token -> 401", s == 401, str(s))

    # The ledger gRPC port is not published to the host, so call it from inside the Docker network.
    project = os.environ.get("COMPOSE_PROJECT_NAME", "docker")
    g = subprocess.run(["docker", "run", "--rm", "--network", f"{project}_default", "-v", f"{ROOT / 'shared/proto'}:/protos:ro",
                        "fullstorydev/grpcurl", "-plaintext", "-import-path", "/protos", "-proto", "ledger/v1/ledger.proto",
                        "-d", "{}", "transaction-service:9091", "ledger.v1.LedgerService/PostTransaction"],
                       capture_output=True, text=True, env={**os.environ, "MSYS_NO_PATHCONV": "1"})
    check("ledger gRPC call without a service token -> Unauthenticated", "Unauthenticated" in g.stderr + g.stdout, (g.stderr + g.stdout)[-120:])

    print("4. Object-level authorisation (IDOR)")
    pay = {"fromAccountId": a["accountId"], "toAccountNumber": b["accountNumber"], "amountMinor": 100}
    s, _, body = call("POST", f"{PAY}/api/v1/payments", pay, ta, {"Idempotency-Key": str(uuid.uuid4())})
    pid = json.loads(body)["data"]["id"]
    s_owner, _, _ = call("GET", f"{PAY}/api/v1/payments/{pid}", token=ta)
    s_other, _, body_other = call("GET", f"{PAY}/api/v1/payments/{pid}", token=tb)
    check("holder can read own payment", s_owner == 200, str(s_owner))
    check("other user gets 404, not the payment", s_other == 404 and pid not in body_other.decode(), str(s_other))
    s, _, _ = call("POST", f"{PAY}/api/v1/payments", {**pay, "fromAccountId": b["accountId"], "toAccountNumber": a["accountNumber"]},
                   ta, {"Idempotency-Key": str(uuid.uuid4())})
    check("cannot pay from someone else's account -> 403", s == 403, str(s))
    s, _, _ = call("GET", f"{ACCT}/api/v1/accounts/{b['accountId']}/balance", token=ta)
    check("cannot read someone else's balance", s in (403, 404), str(s))

    print("5. Input validation")
    idem = lambda: {"Idempotency-Key": str(uuid.uuid4())}
    s, _, _ = call("POST", f"{PAY}/api/v1/payments", {**pay, "amountMinor": 2_000_000_000}, ta, idem())
    check("amount above the maximum -> 400", s == 400, str(s))
    s, _, _ = call("POST", f"{PAY}/api/v1/payments", {**pay, "fromAccountId": "not-a-uuid"}, ta, idem())
    check("malformed account id -> 400", s == 400, str(s))
    s, _, _ = call("POST", f"{PAY}/api/v1/payments", {**pay, "toAccountNumber": "1' OR '1'='1"}, ta, idem())
    check("injection-shaped account number -> 400", s == 400, str(s))
    s, _, _ = call("POST", f"{PAY}/api/v1/payments", {**pay, "amountMinor": -5}, ta, idem())
    check("negative amount -> 400", s == 400, str(s))
    s, _, _ = call("POST", f"{PAY}/api/v1/payments", pay, ta)
    check("missing Idempotency-Key -> 400", s == 400, str(s))
    s, _, _ = call("POST", f"{AUTH}/api/v1/auth/login", {"email": a["email"], "password": "x" * 5000})
    check("5,000-character password rejected -> 400", s == 400, str(s))

    print("6. Request size limits")
    s, _, _ = call("POST", f"{PAY}/api/v1/payments", raw=b'{"x":"' + b"a" * (2 << 20) + b'"}', token=ta, headers=idem())
    check("2 MiB body -> 413", s == 413, str(s))

    print("7. Data stores are isolated and not exposed")
    env = dict(line.split("=", 1) for line in (ROOT / "docker/.env").read_text().splitlines() if "=" in line and not line.startswith("#"))

    def psql(user, password_key, database):
        # -h postgres goes over the Docker network, where password authentication is enforced.
        return subprocess.run(COMPOSE + ["exec", "-T", "-e", f"PGPASSWORD={env[password_key]}", "postgres", "psql", "-h", "postgres",
                                         "-U", user, "-d", database, "-tAc", "select 1"], capture_output=True, text=True)

    own = psql("payment_svc", "PAYMENT_DB_PASSWORD", "payment_db")
    check("control: payment role can use its own database", own.stdout.strip() == "1", own.stderr[-120:])
    other = psql("payment_svc", "PAYMENT_DB_PASSWORD", "auth_db")
    check("payment role (right password) cannot connect to the auth database",
          other.returncode != 0 and "permission denied" in other.stderr, other.stderr[-120:])
    ip = socket.gethostbyname(socket.gethostname())
    reachable = []
    for port in (5432, 6379, 8085):
        try:
            socket.create_connection((ip, port), timeout=1).close()
            reachable.append(port)
        except OSError:
            pass
    check(f"stack not reachable on the LAN address {ip}", not reachable, f"open ports: {reachable}")

    print("8. Rate limiting (runs last: it deliberately trips the login limit)")
    codes = []
    for _ in range(40):
        s, _, _ = call("POST", f"{AUTH}/api/v1/auth/login", {"email": f"nobody{uuid.uuid4().hex[:6]}@example.com", "password": "wrong-password-123"})
        codes.append(s)
    check("login floods are throttled with 429", 429 in codes, str(sorted(set(codes))))

    failed = results.count(False)
    print(f"\n{len(results) - failed}/{len(results)} checks passed")
    sys.exit(1 if failed else 0)


main()
