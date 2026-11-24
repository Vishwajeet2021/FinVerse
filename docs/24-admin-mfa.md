# 24 - Administrator MFA

Status: built and verified on the local stack (`scripts/mfa_demo.py`). Closes the top open item of
`docs/23` (D-A6) and `docs/22`. Follows `docs/10 §5-§6`, which require TOTP and make it mandatory
for internal roles. Gaps are listed honestly in section 6.

## 1. Inception

**Problem.** An administrator can freeze accounts, resolve fraud cases, approve KYC and export
data (`docs/23`). Until now an admin session was one password away: Auth had no second factor, the
`mfa` claim in the JWT was hard-wired to `false`, and nothing downstream looked at it.

**Requirement.** Every internal role (`support`, `fraud_analyst`, `auditor`, `admin`,
`super_admin`) must hold TOTP, and no such role may ever hold a session that did not pass it.
Customers may opt in. The first sign-in of an existing internal account must lead to enrolment, not
to a session.

**Out of scope.** WebAuthn / hardware keys, SMS or email OTP as a second factor, step-up OTP for
payments (`docs/10 §5`, a separate mechanism), an admin-initiated MFA reset, UI for customer
opt-in.

## 2. Decisions

- **D-M1 TOTP (RFC 6238), written against the standard library.** HMAC-SHA1, 6 digits, 30 s, the
  values every authenticator app assumes. It is about 40 lines, and the test suite checks it
  against the RFC's published vectors (so does the Python helper in `scripts/`). A credential
  primitive is a poor place to take on a dependency. SHA-1 here is the RFC's HMAC construction, not
  a collision-resistance use.
- **D-M2 The TOTP secret is encrypted at rest, not hashed.** The server must read it back to check
  codes, so hashing is impossible. AES-256-GCM, key in `AUTH_MFA_ENCRYPTION_KEY` (outside the
  database), the user id bound in as additional authenticated data so a ciphertext copied onto
  another user's row will not open, a `v1:` prefix to allow rotation later. The service **refuses
  to start** without the key: silently falling back to "no MFA" would remove the control.
- **D-M3 A code is accepted once.** `users.mfa_last_step` holds the newest accepted 30-second step;
  a code is accepted only for a greater step, in one conditional `UPDATE`. A code someone watched
  being typed would otherwise stay valid for up to 90 seconds (the previous, current and next step
  are accepted for clock drift).
- **D-M4 Sign-in is two steps joined by a challenge.** A correct password returns a one-time
  `challengeToken`, not tokens. Only its hash is stored; it lives 5 minutes, is single use, and
  allows 5 guesses. Every wrong code also counts as a failed login against the account
  (FR-AUTH-9).
- **D-M5 A correct password does not reset the failure count for an MFA account.** Otherwise
  knowing the password would allow unlimited code guessing: five wrong codes, log in again (count
  cleared), five more. The count is cleared only when the second step succeeds. Proven live.
- **D-M6 Internal roles cannot get a token without MFA, including the first time.** If the role
  requires MFA and none is enrolled, login returns an `ENROLL` challenge. That challenge can do one
  thing, enrol, and confirming enrolment finishes the sign-in (the user has just proved the factor).
  There is no path from a password to a token for these roles.
- **D-M7 The session remembers it passed MFA.** `refresh_tokens.mfa_verified` is copied down the
  rotation family, so the access token minted every 15 minutes carries the same `mfa` claim.
  A refresh of an internal-role session with `mfa_verified = false` (any session opened before this
  change) is refused with `MFA_REQUIRED` and the family is revoked: existing admin sessions end at
  their next refresh and sign in again through enrolment.
- **D-M8 Privileged routes check the claim themselves.** `jwtauth.RequireMFA()` (shared) is applied
  to every Admin Service route, Reporting's fraud summary, Fraud case list/resolve, Customer KYC
  review, Notification admin routes and Audit records. Auth already never issues such a token
  without MFA; this keeps the routes closed if one were ever minted by a path that skipped it, and
  states the rule where the privileged action is.
- **D-M9 Recovery codes.** Ten, 50 bits each, shown once at enrolment, stored as SHA-256 hashes,
  each usable once (one conditional `UPDATE`), accepted wherever a code is. Using one is logged at
  WARN.
- **D-M10 The MFA columns are read-only to the general user repository.** `UserRepository.Update`
  saves the whole row; a stale copy saved by the lockout code would silently roll back
  `mfa_last_step` and reopen the replay window. The model tags the columns `->` (read only) and
  only `MFARepository` writes them, with conditional statements. A unit test and the live replay
  check cover it.
- **D-M11 Customer opt-in re-asks the password.** A stolen access token alone must not be enough to
  bind someone's account to an attacker's phone (which would also lock the owner out).

## 3. Flows

```
password ok ─┬─ user has MFA ───────────────► LOGIN challenge  ─► /mfa/verify (code or recovery) ─► session (mfa=true)
             ├─ role requires it, none yet ─► ENROLL challenge ─► /mfa/enroll/begin ─► QR + key
             │                                                   /mfa/enroll/confirm (code) ─► session + recovery codes
             └─ otherwise (customer) ────────► session (mfa=false), exactly as before
```

| Endpoint | Auth | Purpose |
|---|---|---|
| `POST /auth/login` | none | `{accessToken}` or `{mfaRequired \| mfaEnrollmentRequired, challengeToken}` |
| `POST /auth/mfa/verify` | challenge | finish a LOGIN challenge |
| `POST /auth/mfa/enroll/begin`, `/confirm` | challenge | first-login enrolment |
| `POST /auth/mfa/setup/begin`, `/confirm` | bearer (+password) | opt-in for a signed-in user |

All are rate limited per IP and fail closed; responses that carry a secret are `no-store`.

## 4. Threats considered

| Threat | Mitigation |
|---|---|
| Stolen or guessed admin password | second factor required; no token without it |
| Brute-forcing the 6-digit code | 5 guesses per challenge, each also a failed login, lockout after 5, count not reset by a correct password (D-M5), per-IP limit |
| Replaying an observed code | one accepted use per step (D-M3) |
| Database copy (backup, replica, SQL read) | secrets encrypted (D-M2); recovery codes hashed; challenge tokens hashed |
| Swapping a known secret into a user's row | ciphertext bound to the user id |
| An unenrolled admin session slipping through | no token for the role without MFA; refresh refuses `mfa_verified = false` (D-M7); downstream `RequireMFA` (D-M8) |
| Using the challenge token as a credential | it opens no API (checked live: 401), expires in 5 minutes, single use |
| Secrets in logs | none logged; checked live that secret, recovery code, challenge and access token are absent |
| Race: two parallel requests with one code or one recovery code | single conditional `UPDATE`, one wins |
| Stale write rolling back replay protection | D-M10 |
| Attacker with a stolen customer token enrols their own phone | password re-asked (D-M11) |

## 5. Construction

| Unit | What | Where |
|---|---|---|
| A | TOTP, AES-GCM cipher, recovery codes (+ RFC 6238 vector tests) | `services/auth-service/internal/mfa` |
| B | migration `0002_mfa` (user columns, recovery codes, challenges, `mfa_verified`) | `services/auth-service/db` |
| C | challenge, verify, enrolment logic; refresh propagation; injected clock | `internal/service/{login,mfa}.go` |
| D | MFA repository (conditional writes), read-only MFA columns | `internal/repository/postgres` |
| E | endpoints, DTOs, error codes (`MFA_CODE_INVALID`, `MFA_CHALLENGE_INVALID`, `MFA_REQUIRED`, `MFA_ALREADY_ENABLED`, `MFA_NOT_STARTED`) | `internal/transport/http` |
| F | `RequireMFA` + test; applied to privileged routes in Admin, Reporting, Fraud, Customer, Notification, Audit | `shared/go/jwtauth`, services |
| G | `AUTH_MFA_ENCRYPTION_KEY` in compose, `gen_env.py`, `.env.example` | `docker/`, `scripts/` |
| H | login page: code step, enrolment step (QR + manual key), recovery-code step | `frontend/src/routes/login` |
| I | `scripts/mfa_helper.py` (stdlib TOTP and enrol/login helpers); the three demos that create admins now enrol; `scripts/mfa_demo.py` | `scripts/` |

Unit tests: `internal/mfa` 7 (RFC vectors, window, malformed input, cipher binding/tamper, recovery
codes), Auth service 14 new (internal roles never get tokens, customers unchanged, enrolment,
replay, attempt exhaustion, failure count not reset, recovery single use, challenge single
use/expiry/purpose, refresh keeps mfa, legacy session refused, self-enrolment needs password,
stale write cannot roll back replay protection), shared `RequireMFA` 1. The service now uses an
injected clock (`Login` and `Refresh` previously read the wall clock directly).

## 6. Operations: live verification and gaps

`python scripts/mfa_demo.py` on the Compose stack: **46 checks, all pass**. Takes about two
minutes, because the server accepts each 30-second step once and the script has to wait for fresh
steps to sign the same user in twice. Covers: customers unchanged; all five internal roles get no
session from a password; challenge opens no API; wrong/unknown/reused challenge; wrong code;
secret stored as `v1:` ciphertext and recovery codes as 64-hex hashes; replay refused (the
enrolment code, and a sign-in code within its window); refresh keeps `mfa=true`; a legacy
unverified session is refused and revoked; recovery code works once (typed lower case);
five wrong codes exhaust the challenge, the right code is then refused, logging in again returns
`ACCOUNT_LOCKED`, and `failed_login_count` persisted; customer opt-in needs the password and
cannot replace a live secret; security events are logged and no secret material is.

**Regression** on the same stack after the change: `admin_reporting_demo.py`, `fraud_audit_demo.py`,
`dlq_demo.py` and `rails_demo.py` all pass (the first three now create their admin through
enrolment), `security_check.py` 27/27, and unit tests pass in every touched module (`shared/go`,
Auth, Admin, Reporting, Fraud, Customer, Notification, Audit). Frontend `svelte-check`: 0 errors,
0 warnings; eslint clean.

### Honest gaps

1. **No MFA reset.** An admin who loses both the phone and the recovery codes is locked out, and
   there is no support or super-admin reset flow. The only recovery is direct database work
   (clear `mfa_enabled`, `mfa_secret_enc`, delete the user's `mfa_recovery_codes`), which the
   application does not audit. This is the most important thing still missing.
2. **First-login enrolment is trust on first use.** Whoever first presents the initial password
   binds their authenticator. The system does not deliver that password; the safe practice is to
   provision and enrol immediately. An invitation token would remove the window.
3. **TOTP is phishable.** A real-time relay of a code defeats it. WebAuthn would not be.
4. **Events are logged, not audited.** `docs/10 §6` wants admin logins written to Audit. Auth has
   no outbox, so enrolment, sign-in, rejection and recovery-code use go to structured logs only.
5. **Customer opt-in has no UI** (the API exists and is tested), no way to turn MFA off or
   regenerate recovery codes, and no "codes remaining" count. Enabling MFA does not end a
   customer's existing sessions.
6. **No key rotation tooling.** The `v1:` prefix leaves room for it, but nothing re-encrypts.
   Losing `AUTH_MFA_ENCRYPTION_KEY` means every enrolled user must be reset (gap 1).
7. **Lockout can be used against an admin.** Someone who knows an admin's password can lock the
   account for 15 minutes by failing the second step. The same is true of the password step; the
   trade-off favours not letting guesses through.
8. **Behaviour change:** existing internal sessions end at their next refresh (at most 15
   minutes) and must sign in through enrolment (D-M7). Intended; worth announcing.
9. **Downstream enforcement is proved by a unit test of the middleware**, plus every demo using an
   MFA session successfully. No live request with `mfa=false` exists to try, because Auth never
   issues one.
10. **The new login steps have not been run in a real browser.** Type-check and lint are clean; the
    API behind them is proved by the script, but QR rendering (an inline `data:` image, allowed by
    the CSP `img-src`) and the clipboard copy are unobserved.
11. The gaps in `docs/22` section 5a still stand.
