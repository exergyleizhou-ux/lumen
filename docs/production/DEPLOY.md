# Lumen Hosted Deployment

This procedure creates a release candidate; production execution requires an
explicit change approval. Lumen Code and Lab are private services behind Caddy.

## Prerequisites

- Pin `LUMEN_IMAGE`, `LUMEN_PREVIOUS_IMAGE`, and `MIGRATOR_IMAGE` by digest.
- Inject secrets from the platform secret manager. Required secrets are the
  dedicated Workbench JWT secret (32+ random bytes), runtime-ingest secret
  (32+ random bytes), Postgres URL, and provider credential. Do not put values
  in Git, image layers, Compose overrides, or command history.
- Postgres must already contain the Oasis Workbench migrations. Object and
  workspace volumes need encrypted, backed-up storage. The control-plane URL is
  reachable only on the private network.
- TLS terminates at the platform ingress. It forwards Host, Origin, Cookie and
  `X-Request-ID` to the loopback-bound Lumen Caddy port.

## Release

```bash
cp .env.example deploy/.env.production
# Populate deploy/.env.production from the secret manager; chmod 600 it.
LUMEN_ENV_FILE=deploy/.env.production scripts/deploy-lumen-vps.sh check
LUMEN_ENV_FILE=deploy/.env.production scripts/deploy-lumen-vps.sh migrate
LUMEN_ENV_FILE=deploy/.env.production scripts/deploy-lumen-vps.sh deploy
LUMEN_ENV_FILE=deploy/.env.production scripts/deploy-lumen-vps.sh smoke
```

Migration is an explicit one-shot step before application replacement; app
replicas never race migrations. Take a verified database snapshot first and
record schema version, old/new image digests, operator, timestamp, and ticket.

## Gates

1. Starting hosted Code or Lab without JWT, database, object directory,
   control-plane or provider secrets fails closed.
2. Lab `/api/lab/health` proves liveness and `/api/lab/readyz` proves capacity.
   Code root is the current liveness probe; an authenticated `/v1/status` is the
   functional readiness probe until the dedicated Code readiness endpoint ships.
3. Mint a real short-lived token through Oasis. Through the proxy, run one Code
   edit/verification and one Lab project/artifact flow; reconnect each SSE stream,
   cancel a disposable run, and download its owner-scoped evidence bundle.
4. Confirm the Oasis quota reservation/debit/completion records, Postgres Run
   rows, object bytes and SHA-256 metadata agree.
5. Keep the old image and DB snapshot until the observation window closes.

Never publish ports 8080 or 4310. Compose exposes them only on its internal
network and binds Caddy to host loopback.
