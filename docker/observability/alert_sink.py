"""Local stand-in for a pager: logs each alert Alertmanager delivers, one line each.

Stdout is tailed by Promtail, so delivered alerts are searchable in Loki
({service="alert-sink"}). Replace with SNS/email/Slack outside local development.
"""
import json
from http.server import BaseHTTPRequestHandler, HTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or b"{}")
        for a in body.get("alerts", []):
            print(json.dumps({"level": "alert", "status": a.get("status"), "alertname": a["labels"].get("alertname"),
                              "severity": a["labels"].get("severity"), "service": a["labels"].get("service"),
                              "summary": a.get("annotations", {}).get("summary")}), flush=True)
        self.send_response(200)
        self.end_headers()

    def log_message(self, *args):
        pass


HTTPServer(("", 8099), Handler).serve_forever()
