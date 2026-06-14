---
name: database-migrations
description: Safe database migration patterns for schema and data changes.
---
# Database Migrations
Safe migration practices:

1. **Backward compatible**: New columns must be nullable or have defaults.
2. **Expand-contract**: Add first, remove later — never rename a column in one step.
3. **No-downtime**: Avoid locks — use ONLINE/CONCURRENTLY where available.
4. **Data migrations**: Run in batches, with idempotency checks.
5. **Rollback plan**: Every migration has a tested reverse migration.
6. **Separate DDL and DML**: Schema changes first, data changes after.
