"""Latency percentiles for one span operation, straight from Jaeger.

    python scripts/span_latency.py <service> "<operation>" [lookback=15m] [limit=1500]
    python scripts/span_latency.py fraud-service "fraud.v1.FraudService/CheckTransaction"

Used to check NFR-PERF-4 (Fraud CheckTransaction P99 < 80 ms), which the k6
HTTP numbers cannot show on their own.
"""
import json, sys, urllib.parse, urllib.request

service, operation = sys.argv[1], sys.argv[2]
lookback = sys.argv[3] if len(sys.argv) > 3 else "15m"
limit = sys.argv[4] if len(sys.argv) > 4 else "1500"
url = "http://localhost:16686/api/traces?" + urllib.parse.urlencode(
    {"service": service, "operation": operation, "lookback": lookback, "limit": limit})
traces = json.load(urllib.request.urlopen(url))["data"]

durations = []
for t in traces:
    procs = {k: v["serviceName"] for k, v in t["processes"].items()}
    for s in t["spans"]:
        if s["operationName"] == operation and procs[s["processID"]] == service:
            durations.append(s["duration"] / 1000.0)  # microseconds -> ms

if not durations:
    sys.exit("no matching spans found")
durations.sort()


def pct(p):
    return durations[min(len(durations) - 1, int(len(durations) * p / 100))]


print(f"{service} {operation}: n={len(durations)}  "
      f"p50={pct(50):.2f}ms p95={pct(95):.2f}ms p99={pct(99):.2f}ms max={durations[-1]:.2f}ms")
