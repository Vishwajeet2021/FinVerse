"""Provision N verified customers (profile + KYC + one account each) and write
loadtest/fixtures.json for the k6 scripts. Stdlib only.

    python loadtest/setup.py [N=4]

Needs the compose stack up. The email-verification token is read from
auth-service's dev-mailer log line (Phase 1's ConsoleMailer).
"""
import json, re, subprocess, sys, time, urllib.request, urllib.error, pathlib

AUTH, CUST, ACCT = "http://localhost:8081", "http://localhost:8082", "http://localhost:8083"
ROOT = pathlib.Path(__file__).resolve().parent.parent
PASSWORD = "Lt!Passw0rd-9x"


def call(method, url, body=None, token=None):
    req = urllib.request.Request(url, method=method, data=json.dumps(body).encode() if body is not None else None)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req) as r:
            return json.loads(r.read() or b"{}")
    except urllib.error.HTTPError as e:
        raise SystemExit(f"{method} {url} -> {e.code}: {e.read().decode()[:300]}")


def verification_token(email):
    for _ in range(10):
        logs = subprocess.run(["docker", "compose", "-f", str(ROOT / "docker/docker-compose.yml"), "logs", "auth-service"],
                              capture_output=True, text=True).stdout
        m = re.findall(re.escape(email) + r'.*?token=([A-Za-z0-9_\-]+)', logs)
        if m:
            return m[-1]
        time.sleep(1)
    raise SystemExit(f"no verification token for {email}")


def main():
    n = int(sys.argv[1]) if len(sys.argv) > 1 else 4
    run = int(time.time())
    users = []
    for i in range(1, n + 1):
        email = f"lt{run}u{i}@example.com"
        call("POST", f"{AUTH}/api/v1/auth/register", {"email": email, "password": PASSWORD})
        call("POST", f"{AUTH}/api/v1/auth/verify-email", {"token": verification_token(email)})
        tok = call("POST", f"{AUTH}/api/v1/auth/login", {"email": email, "password": PASSWORD})["data"]["accessToken"]
        pan = f"ABCDE{(run + i) % 10000:04d}F"
        call("POST", f"{CUST}/api/v1/customers", {"fullName": f"Load Tester {i}", "dob": "1990-01-01", "pan": pan}, tok)
        try:
            call("POST", f"{CUST}/api/v1/customers/me/kyc/submit", {}, tok)
        except SystemExit as e:
            print("kyc submit:", e)
        time.sleep(1)
        acct = call("POST", f"{ACCT}/api/v1/accounts", {"productType": "SAVINGS"}, tok)["data"]
        users.append({"email": email, "password": PASSWORD, "accountId": acct["id"], "accountNumber": acct["accountNumber"]})
        print("provisioned", email)
    (ROOT / "loadtest/fixtures.json").write_text(json.dumps({"users": users}, indent=2))
    print(f"wrote loadtest/fixtures.json with {n} users")


main()
