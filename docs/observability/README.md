# basement-server observability (v1.11.0f)

basement exposes a Prometheus scrape endpoint, a drop-in Grafana
dashboard and a starter alert ruleset. This directory is the operator
quick-start.

## What's here

| File | Purpose |
|------|---------|
| `grafana-dashboard.json` | Drop-in Grafana dashboard. Import via `+ -> Import`. Bind to your Prometheus datasource at import time (the dashboard uses a `${DS_PROMETHEUS}` variable). |
| `prometheus-alerts.yml` | Starter alert rules — five core alerts plus an optional `BasementBackupOverdue`. |
| `README.md` | This file. |

## Scrape config

basement-server exposes `/metrics` on the same port as the rest of
the HTTP server (default `:8080`). Add it to your Prometheus config:

```yaml
scrape_configs:
  - job_name: basement
    scrape_interval: 30s
    metrics_path: /metrics
    static_configs:
      - targets: ["basement.example.com:8080"]
```

If you've set `BASEMENT_METRICS_TOKEN`, add the bearer header:

```yaml
scrape_configs:
  - job_name: basement
    scrape_interval: 30s
    metrics_path: /metrics
    authorization:
      type: Bearer
      credentials: scrape-s3cr3t
    static_configs:
      - targets: ["basement.example.com:8080"]
```

By default `/metrics` is unauthenticated — the standard Prometheus
convention is to front it with a network allowlist (firewall, VPN,
service-mesh policy). Use the token gate when a shared ingress makes
the network gate impractical (e.g. multi-tenant k8s).

## Importing the dashboard

1. In Grafana: `Dashboards -> + -> Import`.
2. Upload `grafana-dashboard.json` (or paste the contents).
3. Select your Prometheus datasource when prompted.
4. The dashboard renders ten panels: build info, HTTP request rate +
   latency percentiles, auth success rate, federation lag per replica,
   backup runs, webhook success rate, audit-event volume, active
   sessions, service-account count.

The dashboard refreshes every 30s by default; tune to your scrape
interval.

## Applying the alert rules

Save `prometheus-alerts.yml` somewhere Prometheus's `rule_files` glob
picks up (e.g. `/etc/prometheus/rules/basement.yml`) and reload:

```bash
curl -X POST http://prometheus:9090/-/reload
```

(Requires Prometheus started with `--web.enable-lifecycle`; otherwise
restart the process.) Verify under Prometheus `-> Alerts` that all six
rules are loaded green.

### Alert reference

| Alert | Trigger | Severity |
|-------|---------|----------|
| `BasementDown` | `basement_build_info` absent for 5min | critical |
| `BasementHighAuthFailureRate` | > 1 failed login/sec sustained for 5min | warning |
| `BasementFederationLagHigh` | per-replica lag > 600s for 5min | warning |
| `BasementBackupFailed` | any backup failure in last 15min | warning |
| `BasementBackupOverdue` | no successful run for a backup in 26h | warning |
| `BasementWebhookFailureRate` | > 50% delivery failures over 10min | warning |

Each rule ships with a `runbook` annotation in the YAML pointing at
the first three steps to investigate. Wire these into Alertmanager
templates to surface them in pages.

## Exposed metrics

The /metrics endpoint emits exactly the metric families below. The
set is fixed at v1.11.0f — adding new families is a server-side
change, never a config knob, so dashboards and alerts stay stable
across releases.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `basement_http_requests_total` | counter | method, path, status | HTTP requests handled. `path` is the chi route template, not the raw URL. |
| `basement_http_request_duration_seconds` | histogram | method, path | Per-request latency, bucketed. |
| `basement_auth_attempts_total` | counter | result | Login attempts (success or failure). |
| `basement_audit_events_total` | counter | action | One per audit event written. |
| `basement_federation_replicate_total` | counter | result | Federation replicate outcomes. |
| `basement_federation_lag_seconds` | gauge | federation_id, replica | Seconds since the replica's last successful sync. |
| `basement_backup_runs_total` | counter | result | Scheduled backup runs. |
| `basement_backup_last_success_timestamp_seconds` | gauge | backup_id | Unix timestamp of the last successful run. |
| `basement_webhook_deliveries_total` | counter | result | Webhook delivery outcomes. |
| `basement_active_sessions` | gauge |  | Approximate authenticated session count. |
| `basement_service_accounts_total` | gauge |  | Service accounts across every owner. |
| `basement_buckets_total` | gauge | driver, cluster | Buckets present (populated on snapshot tick). |
| `basement_objects_total` | gauge | driver, cluster, bucket | Object count per bucket; may be sampled when expensive. |
| `basement_build_info` | gauge | version, commit | Always 1; labels carry build metadata. |

## Structured logs

basement-server emits one structured log line per server event (request,
audit, federation tick, webhook delivery, etc.) via Go's `log/slog`.

Format is controlled by `BASEMENT_LOG_FORMAT`:

- `json` (default) — one JSON object per line, ready for filebeat / loki
  / stackdriver / cloudwatch ingest.
- `text` — `key=value` lines for local development.

Log level is `BASEMENT_LOG_LEVEL` (debug | info | warn | error;
default info).

Example JSON line:

```json
{"time":"2026-05-22T19:04:11Z","level":"INFO","msg":"request","method":"GET","url":"/api/v1/buckets","status":200,"duration_ms":12}
```
