# Lumen Rollback and Forward Fix

## Decision

Prefer application rollback when the new schema is backward compatible. Prefer
a forward fix when new writes have occurred or a down migration could discard
Run, approval, usage, Artifact, or quota data.

## Application rollback

```bash
LUMEN_ENV_FILE=deploy/.env.production scripts/deploy-lumen-vps.sh rollback
LUMEN_ENV_FILE=deploy/.env.production scripts/deploy-lumen-vps.sh smoke
```

This replaces Code and Lab with `LUMEN_PREVIOUS_IMAGE`; it does not mutate the
database or volumes. Verify JWT rejection, owner isolation, SSE replay, one
Code Run and one Lab Run before reopening traffic.

## Database response

1. Stop new Run creation and drain/cancel active Runs.
2. Record the current schema version and image digest.
3. If no incompatible writes occurred, use the reviewed Oasis down migration
   and immediately run its up migration in a disposable restore to prove the
   path. Never improvise SQL on production.
4. If writes occurred, deploy a compatible forward-fix migrator and app. Restore
   from the pre-deploy snapshot only with incident-command approval because it
   discards subsequent data.
5. Object storage is append/idempotency protected but is not rolled back with
   Postgres. Reconcile metadata to object keys and quarantine orphans; do not
   bulk-delete them during the incident.

Old local SQLite installations remain local-mode inputs. Back up `lumen.db`,
start the new binary once, exercise a disposable Run, then retain the backup so
the old binary can be tested read-only. Hosted mode never silently falls back
to SQLite.
