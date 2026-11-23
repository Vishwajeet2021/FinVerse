# Load tests

Measured, repeatable numbers for the payment path (NFR-PERF-3, NFR-PERF-5,
NFR-SCALE-2, NFR-DATA-3). Results are recorded in `docs/20-ai-dlc-phase3.md`.

```bash
docker compose -f docker/docker-compose.yml up -d --build
python loadtest/setup.py 4            # 4 verified customers + accounts -> fixtures.json
# Git Bash on Windows: export MSYS_NO_PATHCONV=1 first
docker compose -f docker/docker-compose.yml --profile loadtest run --rm k6 run \
    -e VUS=20 -e DURATION=60s /scripts/payments.js
python scripts/dlq_demo.py            # retry -> DLQ -> replay demo
```

`payments.js` ends with a conservation check: the sum of all account balance
deltas must be zero, and the script reports how long the event-fed balance
projection took to converge after the last payment. Traces for any request are
in Jaeger at http://localhost:16686.
