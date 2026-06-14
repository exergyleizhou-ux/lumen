---
name: postgres-patterns
description: PostgreSQL patterns — indexing, query optimization, locking.
---
# PostgreSQL Patterns
PostgreSQL development patterns:

1. **Indexing**: B-tree for equality/ranges, GIN for full-text/arrays, GiST for geo. Partial indexes.
2. **EXPLAIN ANALYZE**: Always check query plans before deploying.
3. **Connection pooling**: Use PgBouncer or pgpool for connection management.
4. **Locking**: `SELECT ... FOR UPDATE SKIP LOCKED` for queue patterns.
5. **Bulk operations**: Use COPY or `INSERT ... ON CONFLICT` for upserts.
6. **Partitioning**: Consider table partitioning for very large tables (>100M rows).
7. **VACUUM**: Understand autovacuum and monitor bloat.
8. **JSONB**: Use for semi-structured data, index with GIN.
