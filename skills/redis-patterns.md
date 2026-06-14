---
name: redis-patterns
description: Redis patterns — caching, rate limiting, queues, distributed locks.
---
# Redis Patterns
Redis usage patterns:

1. **Caching**: Cache-aside pattern. Set TTL. Use hash tags for related keys.
2. **Rate limiting**: Sliding window with sorted sets, or token bucket with INCR.
3. **Queues**: Use lists (LPUSH/RPOP) or streams for reliable message delivery.
4. **Distributed locks**: Use SET NX PX for locks, with Redlock for high availability.
5. **Session store**: Hash per session. Use TTL for expiration.
6. **Leaderboards**: Sorted sets with ZADD/ZRANGE.
7. **Pub/Sub**: For real-time notifications. Use streams for persistence.
