# Lumen Hosted Runbook

## Service map

- Platform ingress/TLS -> loopback Caddy -> private `code:8080` / `lab:4310`.
- Code and Lab -> Postgres, object/workspace volumes, Oasis control plane and
  the configured provider. No runtime service has a public container port.
- JWT is minted by Oasis and has a five-minute maximum operational lifetime.

## Observability

Structured platform logs must attach `request_id`, `run_id`, and a one-way
salted user hash. Never log Authorization/Cookie headers, JWTs, prompts, model
reasoning, tool arguments, provider keys, or file contents. Retain access logs
separately from runtime logs and restrict both by role.

Scrape the platform/Oasis Prometheus exporters and alert on:

| Signal | Warning | Critical / action |
| --- | --- | --- |
| HTTP 5xx ratio | >2% for 5m | >5% for 5m; stop rollout |
| Run p95 duration | >10m | near policy wall-time; inspect provider/queue |
| approval wait p95 | >15m | expiry growth; notify owner |
| active/queued Runs | >80% quota | saturation/rejected reservations |
| provider errors | >5% for 5m | >15%; circuit-break/provider incident |
| DB/storage errors | any sustained | page immediately; runtime fails closed |
| lease/usage reconciliation | any backlog | page if older than two lease periods |

Lumen currently relies on platform and Oasis exporters rather than exposing an
unauthenticated application `/metrics` endpoint. Metrics ingress must remain
private. Correlate Run state with durable Postgres rows, not browser state.

## Checks

```bash
docker compose --env-file deploy/.env.production -f deploy/docker-compose.prod.yml ps
docker compose --env-file deploy/.env.production -f deploy/docker-compose.prod.yml logs --since=15m code lab caddy
LUMEN_ENV_FILE=deploy/.env.production scripts/deploy-lumen-vps.sh smoke
```

For SSE incidents, confirm proxy `flush_interval -1`, the 24-hour response-header
timeout, no intermediary buffering, monotonic event sequence, and replay from
the last event ID. For DB/object/control-plane failure, do not bypass fail-closed
startup or persistence errors; repair the dependency, reconcile reservations,
then resume.

For leaked secrets, isolate the service, rotate at its source, restart both
runtimes, invalidate signing material if relevant, and audit access logs. For
cross-tenant suspicion, stop traffic and preserve DB/object/log evidence before
any cleanup.
