"""Regenerates docker/observability/grafana/dashboards/*.json.

    python scripts/gen_dashboards.py

Dashboards are generated so the three of them stay consistent; the JSON is
committed because Grafana provisions it from disk.
"""
import json
import pathlib

OUT = pathlib.Path(__file__).resolve().parent.parent / "docker/observability/grafana/dashboards"
OUT.mkdir(parents=True, exist_ok=True)
DS = {"type": "prometheus", "uid": "prometheus"}


def panel(i, title, exprs, x, y, w=12, h=8, unit=None, kind="timeseries", desc=None):
    p = {
        "id": i, "title": title, "type": kind, "datasource": DS,
        "gridPos": {"x": x, "y": y, "w": w, "h": h},
        "targets": [{"datasource": DS, "expr": e, "legendFormat": l, "refId": chr(65 + n)} for n, (e, l) in enumerate(exprs)],
        "fieldConfig": {"defaults": {"unit": unit} if unit else {}, "overrides": []},
    }
    if desc:
        p["description"] = desc
    return p


def write(uid, title, panels, name):
    d = {"uid": uid, "title": title, "schemaVersion": 39, "version": 1, "refresh": "5s",
         "time": {"from": "now-15m", "to": "now"}, "panels": panels, "templating": {"list": []}, "tags": ["finverse"]}
    (OUT / f"{name}.json").write_text(json.dumps(d, indent=1))


def pay_q(q):
    return ('histogram_quantile(%s, sum by (le) (rate(finverse_http_request_duration_seconds_bucket'
            '{service="payment-service",method="POST",route="/api/v1/payments"}[1m])))' % q)


write("fv-platform", "Platform overview", [
    panel(1, "Request rate by service", [('sum by (service) (rate(finverse_http_requests_total[1m]))', "{{service}}")], 0, 0, unit="reqps"),
    panel(2, "5xx error ratio by service", [('sum by (service) (rate(finverse_http_requests_total{status=~"5.."}[1m])) / sum by (service) (rate(finverse_http_requests_total[1m]))', "{{service}}")], 12, 0, unit="percentunit"),
    panel(3, "p95 latency by service", [('histogram_quantile(0.95, sum by (le, service) (rate(finverse_http_request_duration_seconds_bucket[1m])))', "{{service}}")], 0, 8, unit="s"),
    panel(4, "Services up", [('up{job="finverse"}', "{{service}}")], 12, 8, kind="stat", desc="1 = scraped OK, 0 = down"),
], "platform-overview")

write("fv-payments", "Payment funnel and ledger health", [
    panel(1, "Payments entering each state (per second)", [('sum by (status) (rate(finverse_payments_total[1m]))', "{{status}}")], 0, 0, unit="ops",
          desc="INITIATED -> (PROCESSING) -> COMPLETED or FAILED. A gap between INITIATED and the outcomes is a drop-off."),
    panel(2, "Totals since service start", [('sum by (status) (finverse_payments_total)', "{{status}}")], 12, 0, kind="stat"),
    panel(3, "Ledger transactions posted per second", [('rate(finverse_ledger_transactions_posted_total[1m])', "posted")], 0, 8, unit="ops"),
    panel(4, "Ledger balance-check failures (must be 0)", [('finverse_ledger_balance_check_failures_total', "failures")], 12, 8, kind="stat",
          desc="NFR-DATA-3. Any non-zero value raises the LedgerImbalance alert."),
    panel(5, "Payment POST latency p50 / p95 / p99", [(pay_q("0.50"), "p50"), (pay_q("0.95"), "p95"), (pay_q("0.99"), "p99")], 0, 16, 24, unit="s"),
], "payment-funnel")

write("fv-fraud", "Fraud, messaging and DLQ", [
    panel(1, "Fraud verdicts per second", [('sum by (verdict) (rate(finverse_fraud_checks_total[1m]))', "{{verdict}}")], 0, 0, unit="ops"),
    panel(2, "CheckTransaction latency vs 80 ms budget", [
        ('histogram_quantile(0.50, sum by (le) (rate(finverse_fraud_check_duration_seconds_bucket[1m])))', "p50"),
        ('histogram_quantile(0.99, sum by (le) (rate(finverse_fraud_check_duration_seconds_bucket[1m])))', "p99"),
        ('vector(0.08)', "budget")], 12, 0, unit="s"),
    panel(3, "Event consumer outcomes per second", [('sum by (group, outcome) (rate(finverse_eventbus_messages_total[1m]))', "{{group}} {{outcome}}")], 0, 8, unit="ops"),
    panel(4, "Dead-letter queue depth", [('max by (stream, group) (finverse_eventbus_dlq_depth)', "{{stream}} / {{group}}")], 12, 8, kind="stat",
          desc="Any value above 0 raises DeadLetterQueueNotEmpty."),
], "fraud-messaging")
