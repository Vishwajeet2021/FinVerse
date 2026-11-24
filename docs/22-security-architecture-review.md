# 22 — Security and architecture review, and hardening plan

Status: Inception (2026-11-24). Findings come from reading the code and grepping the repo; each
one cites what I looked at. "Verified" below means I reproduced it against the running stack or
confirmed it in code, not just suspected it. Results of the fixes are added in section 5 as they
land.

## 1. Scope and approach (AI-DLC)

Intent: move the platform from "works end to end" to "defensible": every trust boundary
authenticated, every input bounded and validated, secrets out of source, least privilege between
components, and each claim backed by a test or a live check.

Out of scope here (needs your AWS account and product decisions): deployment (Phase 4), and new
product services (Card, Loan, Admin, Reporting, Search). Those are listed in section 6.

Method: threat-model the trust boundaries (browser to API, service to service, service to
datastore, operator to platform), then check each against the code.

## 2. Trust boundaries and what protects them today

| Boundary | Protection today | Verdict |
|---|---|---|
| Browser to public APIs | RS256 JWT, scopes per route, CORS allow-list, HttpOnly refresh cookie, login lockout (FR-AUTH-9), Argon2id password hashing | Sound core; gaps below |
| Service to service (gRPC) | Plaintext, no authentication | **Gap F-3** |
| Service to service (internal HTTP) | Unauthenticated `/internal/*` routes | **Gap F-4** |
| Service to datastore | One shared superuser (`finverse`) for every database | **Gap F-7** |
| Operator to platform | Grafana admin/admin, Prometheus and metrics open, all ports on all interfaces | **Gap F-8** |
| Secrets | Dev keys and passwords committed in `docker-compose.yml` | **Gap F-7** |

## 3. Findings

Severity is my judgement of impact if exploited on a networked deployment.

| ID | Severity | Finding | Evidence |
|---|---|---|---|
| F-1 | High | No HTTP security headers on any API or on the frontend (no `X-Content-Type-Options`, `Referrer-Policy`, CSP, HSTS-ready setup). | grep for the headers across services, shared and frontend: no hits. |
| F-2 | Medium | No request body size limit on any service; a large JSON or base64 body is read in full. Server timeouts exist but `ReadHeaderTimeout` is not set everywhere. | grep for `MaxBytesReader`: no hits. |
| F-3 | Critical | The ledger gRPC API (`PostTransaction`) and the fraud API accept any caller: plaintext transport, no credential. Anything that can reach port 9091 can post money movements. | `insecure.NewCredentials()` in four clients; `grpc.NewServer` with no auth in two servers. |
| F-4 | High | `POST /internal/daily-closing/run` is unauthenticated and state-changing. `GET /internal/accounts/by-number/:n` lets anyone who can reach the port test whether an account number exists. | Routes in transaction and account services have no middleware. |
| F-5 | High | **IDOR:** `GET /api/v1/payments/:id` returns any payment to any caller holding `payments:read`; it never checks the caller holds the payment's source account. | `GetPayment` handler and service. |
| F-6 | Medium | No general rate limiting. Only login has lockout. Registration, password reset, email verification and payment initiation are unthrottled (abuse, enumeration, cost). | grep: lockout only in auth login. |
| F-7 | Medium | Secrets and a shared database superuser committed for local use. The PAN encryption key is in source. | `docker-compose.yml` lines for `POSTGRES_PASSWORD`, `CUSTOMER_PAN_ENCRYPTION_KEY`. |
| F-8 | Medium | Every published port binds all interfaces, so on a laptop on a shared network the databases, Redis and admin tools are reachable by others. Grafana ships with `admin/admin`. | `ports:` entries in compose. |
| F-9 | Medium | Input validation is mostly `required`. No maximum lengths, no UUID format checks on identifiers, no upper bound on amounts, no PAN format check, free-form filenames on document upload. | `dto.go` files in each service. |
| F-10 | Low | `/metrics` is open on the same port as the API. Fine on a private network, wrong if that port is ever exposed. | Route registration. |
| F-11 | Low | Account Service trusts the JWT `kyc_tier` claim nowhere (good) but Auth always issues 0 (documented stand-in); the KYC gate calls Customer Service. Correct, but a latent trap if someone starts trusting the claim. | `OpenAccount` comment. |

Things I checked and found sound: JWT algorithm is pinned to RS256 (`WithValidMethods`), refresh
tokens travel in an HttpOnly cookie, the ledger is append-only with balanced-leg validation,
audit records are protected by database triggers, payment idempotency keys are enforced, and the
card-style PAN is encrypted at rest with AES-256-GCM.

## 4. Decisions

- **D-S1 Service-to-service auth: signed service tokens now, mTLS when deployed.** Each internal
  call carries `x-service-token`: `service-name.unix-seconds.HMAC-SHA256(key, name|seconds)`,
  accepted for 60 seconds, compared in constant time. Chosen because it works in local compose
  without a certificate authority, closes F-3/F-4 completely for the threat of "an unauthorised
  process on the network", and has the same call sites that mTLS will use later. *Rejected:*
  static shared secret (replayable forever, no service identity); full mTLS now (needs a CA and
  rotation tooling that belongs with the AWS phase, where ACM Private CA or a service mesh
  provides it). *Limit:* a single shared key means a compromised service can impersonate another;
  per-service keys or mTLS are the next step and are listed as a gap.
- **D-S2 Fail closed on missing key.** Services refuse to start without `SERVICE_AUTH_KEY`
  outside `ENVIRONMENT=development`, and in development compose supplies one from `.env`.
- **D-S3 Payment lookups return 404, not 403, to non-holders** so payment ids cannot be probed.
- **D-S4 Rate limits live in Redis (fixed window, per IP for anonymous routes, per user for
  authenticated ones)** because Redis is already in the stack and shared across replicas. Limits
  fail open if Redis is down for read paths but fail closed for registration and password
  reset, where abuse costs more than a brief outage.
- **D-S5 Per-service database roles.** Each service gets its own role owning only its database.
  A compromised service can no longer read another service's data.
- **D-S6 Secrets come from `docker/.env` (git-ignored) with a committed `.env.example`**; compose
  uses `${VAR:?message}` so a missing secret is a clear startup error, not a silent default.

## 5. Plan and results

Each unit was built domain-first with tests, then verified against a rebuilt stack (a fresh
Postgres volume, so the per-service roles were created from scratch).

| Unit | Closes | Status | Evidence |
|---|---|---|---|
| S1 Security headers, body limits, `ReadHeaderTimeout` (APIs) and CSP plus headers (frontend) | F-1, F-2 | done | `httpsec` unit tests; live: headers present, 2 MiB body gets 413; frontend dev server returns the CSP with per-request nonces |
| S2 Service tokens on gRPC and internal HTTP | F-3, F-4 | done | `svcauth` unit tests (replay window, wrong key, name swap, allow-lists); live: daily-closing, account lookup and KYC lookup return 401 without a token, a forged token gets 401, and a `grpcurl` call to `PostTransaction` returns `Unauthenticated` |
| S3 Payment ownership | F-5 | done | live: holder reads own payment (200), another user gets 404 |
| S4 Input validation | F-9 | done | customer rules unit-tested (PAN format, name, age, document type/size/magic bytes/extension); live: oversize amount, malformed id, injection-shaped account number, negative amount, 5,000-character password all return 400 |
| S5 Rate limiting | F-6 | done | `ratelimit` tests against real Redis (limit, independence, window reset, fail-open vs fail-closed); live: 40 login attempts produce 429 |
| S6 Per-service DB roles, secrets from env, loopback ports, Grafana credential | F-7, F-8 | done | live: payment role uses its own database and is refused `auth_db`; stack unreachable on the LAN address |
| S7 Live security check and dependency scans | verification | script done; scans below | `scripts/security_check.py`: 27 of 27 checks pass |

Regression after all changes: unit tests pass in every module; `rails_demo.py`,
`fraud_audit_demo.py` and `dlq_demo.py` all pass; the 20-VU, 60 s k6 run completed 5,224
payments, 0 % errors, zero balance drift, projection caught up in 2.0 s, at 83 req/s with p95
297 ms. The earlier unhardened runs were 90 to 106 req/s and p95 228 to 287 ms, so throughput is
at the low end of, or slightly below, the earlier range; I did not isolate how much comes from the
extra Redis rate-limit call, run-to-run noise, or a fresh database.

Two things the verification caught that tests had not:
- Binding published ports to `127.0.0.1` alone made every API call take 2.0 s on this machine,
  because `localhost` resolves to `::1` first and nothing listened there. Ports are now bound on
  both `127.0.0.1` and `[::1]`; latency is back to 3 ms.
- My first database-isolation check passed for the wrong reason (it used a wrong password). It
  now uses the role's real password, with a control proving the role works on its own database.

## 5a. Remaining gaps in this area (honest list)

- **Shared service key.** Any service holding `SERVICE_AUTH_KEY` can impersonate another. Per-service
  keys or mTLS close this (Phase 4). The allow-lists limit blast radius only for callers without the key.
- **gRPC is still plaintext** inside the Docker network; the token authenticates but does not encrypt.
- **Redis has no password.** It is loopback-only on the host and not reachable from outside, but any
  container on the network can use it (including the event bus and rate-limit counters).
- **Database roles own their databases.** A service can therefore alter its own schema, including
  the Audit service dropping its append-only triggers. Separating a migration role from the runtime
  role (runtime gets INSERT and SELECT only) is the proper fix for Audit.
- **Existing local volumes** are not migrated: roles are created on first start only. Developers
  must recreate the volume (documented in the README).
- **Rate limits** are per client address; behind a load balancer the trusted-proxy list must be set
  (Phase 4) or all users share one address. Fixed windows allow a short burst at a window boundary.
- **Frontend CSP** was verified by inspecting the header the dev server sends and by a clean build,
  not in a real browser; the policy permits inline styles (`unsafe-inline` for styles only).
- **`/metrics` and Prometheus** are unauthenticated on the internal network.
- **Dependency scans** (`govulncheck`, `npm audit`, run 2026-11-24): fixed by upgrading `golang.org/x/net`, `pgx` and `klauspost/compress` in all modules. `govulncheck` still lists 25 to 34 findings per module, **all in the Go standard library of my local toolchain (1.26.1)**; every one is fixed in 1.26.11 or earlier, and the service images build with `golang:1.26-alpine`, which currently resolves to Go 1.26.11 (checked). The local toolchain should be updated; the images are not affected, but I did not scan the built binaries. `npm audit --omit=dev` reports 0 production vulnerabilities; 3 development-only findings (`@sveltejs/kit`, `cookie`) need a breaking upgrade and were left alone.
- No penetration test, no WAF, no token revocation list.

## 6. Not in this change (needs your decision)

- Deployment (Phase 4): Terraform, CI/CD to AWS, API Gateway, Secrets Manager, mTLS.
- Product services from Phase 6: Card, Loan, Admin, Reporting, Search, Analytics. Each needs
  functional requirements confirmed in docs 03/07 before I build it.
- Per-service signing keys, token revocation lists, WAF, and a formal penetration test.

## 7. Addendum (docs/23): admin surface and freeze enforcement

- A review of the account freeze (FR-ACC-6) found that no service enforced it: a frozen account could still pay and fund wallet top-ups. Closed in `docs/23` D-A7 (Payment at initiation and settlement, Wallet at top-up, fail closed, 3 s cache bounding the latency).
- New privileged surface: Admin Service and its scopes. The top open item for it, **MFA for administrators**, is closed in `docs/24`; the new top item is an MFA reset path for a lost device (`docs/24` section 6).
- New internal routes are protected by the service-token allow-lists from D-S1: `/internal/admin/accounts/*` (admin-service only), `/internal/account-status/:id` (payment-service, wallet-service only). `reporting-service` was added to Transaction's gRPC allow-list.
- Reports are downloaded only through the authenticated API, with a stored SHA-256 verified on every download and storage keys restricted to `<uuid>.csv`.
