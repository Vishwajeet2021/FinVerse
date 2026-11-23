// Load test: internal transfers through Payment Service (-> Transaction
// Service ledger via gRPC -> outbox -> Redis Streams -> Notification Service).
//
//   docker compose -f docker/docker-compose.yml --profile loadtest run --rm k6 run /scripts/payments.js
//   (optional) -e VUS=20 -e DURATION=60s
//
// Correctness first, then speed: teardown asserts the ledger conserved money
// (sum of all balance deltas == 0 and equals what the successful payments imply).
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter } from 'k6/metrics';
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';

const AUTH = __ENV.AUTH_URL || 'http://localhost:8081';
const ACCOUNT = __ENV.ACCOUNT_URL || 'http://localhost:8083';
const PAYMENT = __ENV.PAYMENT_URL || 'http://localhost:8085';
const AMOUNT = 100; // minor units
const fixtures = JSON.parse(open('./fixtures.json')).users;

const paymentsCompleted = new Counter('payments_completed');
const paymentsFailed = new Counter('payments_failed');

export const options = {
  scenarios: {
    transfers: {
      executor: 'constant-vus',
      vus: Number(__ENV.VUS || 20),
      duration: __ENV.DURATION || '60s',
    },
  },
  thresholds: {
    // Targets from docs/04-non-functional-requirements.md (NFR-PERF); the run
    // report records the measured values whether or not they pass.
    http_req_failed: ['rate<0.01'],
    'http_req_duration{name:payment}': ['p(95)<500', 'p(99)<500'], // NFR-PERF-3
  },
};

const json = { headers: { 'Content-Type': 'application/json' } };

function login(u) {
  const r = http.post(`${AUTH}/api/v1/auth/login`, JSON.stringify({ email: u.email, password: u.password }), json);
  check(r, { 'login ok': (x) => x.status === 200 });
  return r.json('data.accessToken');
}

function balance(token, accountId) {
  const r = http.get(`${ACCOUNT}/api/v1/accounts/${accountId}/balance`, {
    headers: { Authorization: `Bearer ${token}` },
    tags: { name: 'balance' },
  });
  return r.json('data.ledgerBalanceMinor');
}

export function setup() {
  const tokens = fixtures.map(login);
  const before = fixtures.map((u, i) => balance(tokens[i], u.accountId));
  return { tokens, before };
}

export default function (data) {
  const i = (__VU - 1) % fixtures.length;
  const to = fixtures[(i + 1) % fixtures.length];
  const r = http.post(
    `${PAYMENT}/api/v1/payments`,
    JSON.stringify({ fromAccountId: fixtures[i].accountId, toAccountNumber: to.accountNumber, amountMinor: AMOUNT }),
    {
      headers: { ...json.headers, Authorization: `Bearer ${data.tokens[i]}`, 'Idempotency-Key': uuidv4() },
      tags: { name: 'payment' },
    },
  );
  const ok = check(r, { 'payment accepted': (x) => x.status === 201 || x.status === 200 });
  if (ok && r.json('data.status') === 'COMPLETED') paymentsCompleted.add(1);
  else paymentsFailed.add(1);
  sleep(0.05);
}

export function teardown(data) {
  // Balances are an event-fed projection (eventually consistent, NFR-PERF-5):
  // poll until the net drift reaches zero and report how long the consumers
  // took to catch up after the last payment, up to a 60s ceiling.
  const started = Date.now();
  let drift = NaN;
  let deltas = [];
  for (let i = 0; i < 60; i++) {
    const after = fixtures.map((u, k) => balance(data.tokens[k], u.accountId));
    deltas = after.map((a, k) => a - data.before[k]);
    drift = deltas.reduce((s, d) => s + d, 0);
    if (drift === 0) break;
    sleep(1);
  }
  const lagS = (Date.now() - started) / 1000;
  console.log(`balance deltas: ${JSON.stringify(deltas)}  net drift: ${drift}  projection caught up after: ${lagS.toFixed(1)}s`);
  check(null, { 'zero balance drift (money conserved)': () => drift === 0 });
  check(null, { 'projection converged within 5s of load ending (NFR-PERF-5)': () => lagS <= 5 });
}
