// Package cache_system provides a generic, thread-safe cache with TTL
// expiration, LRU eviction, and hit/miss statistics. Used by the provider
// layer to cache API responses, by the filesystem layer for content
// caching, and by embeddings for vector caching.
package cache_system

import (
	"container/list"
	"fmt"
	"sync"
	"time"
)

// Cache is a generic, thread-safe cache with TTL and LRU eviction.
type Cache struct {
	mu         sync.Mutex
	items      map[string]*list.Element
	lru        *list.List
	maxItems   int
	defaultTTL time.Duration
	hits       int64
	misses     int64
}

type cacheItem struct {
	key       string
	value     any
	expiresAt time.Time
	byteSize  int64
}

// New creates a cache with the given maximum item count and default TTL.
func New(maxItems int, defaultTTL time.Duration) *Cache {
	if maxItems <= 0 {
		maxItems = 1000
	}
	return &Cache{
		items:      make(map[string]*list.Element),
		lru:        list.New(),
		maxItems:   maxItems,
		defaultTTL: defaultTTL,
	}
}

// Set stores a value with the default TTL.
func (c *Cache) Set(key string, value any) {
	c.SetWithTTL(key, value, c.defaultTTL)
}

// SetWithTTL stores a value with a specific TTL.
func (c *Cache) SetWithTTL(key string, value any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[key]; ok {
		c.lru.MoveToFront(el)
		el.Value.(*cacheItem).value = value
		el.Value.(*cacheItem).expiresAt = time.Now().Add(ttl)
		return
	}

	c.evictExpired()
	for c.lru.Len() >= c.maxItems {
		c.evictOldest()
	}

	item := &cacheItem{key: key, value: value, expiresAt: time.Now().Add(ttl)}
	el := c.lru.PushFront(item)
	c.items[key] = el
}

// Get retrieves a value. Returns (value, true) on hit, (nil, false) on miss.
func (c *Cache) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key]
	if !ok {
		c.misses++
		return nil, false
	}

	item := el.Value.(*cacheItem)
	if time.Now().After(item.expiresAt) {
		c.lru.Remove(el)
		delete(c.items, key)
		c.misses++
		return nil, false
	}

	c.lru.MoveToFront(el)
	c.hits++
	return item.value, true
}

// GetOrSet returns the cached value or computes and stores a new one.
func (c *Cache) GetOrSet(key string, compute func() any) any {
	if val, ok := c.Get(key); ok {
		return val
	}
	val := compute()
	c.Set(key, val)
	return val
}

// Delete removes a key from the cache.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.lru.Remove(el)
		delete(c.items, key)
	}
}

// Clear removes all items.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*list.Element)
	c.lru.Init()
}

// Size returns the current item count.
func (c *Cache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len()
}

// Stats returns hit/miss statistics.
func (c *Cache) Stats() (hits, misses int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.misses
}

// HitRate returns the cache hit rate as a percentage.
func (c *Cache) HitRate() float64 {
	hits, misses := c.Stats()
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total) * 100
}

func (c *Cache) evictExpired() {
	now := time.Now()
	for el := c.lru.Back(); el != nil; {
		prev := el.Prev()
		item := el.Value.(*cacheItem)
		if now.After(item.expiresAt) {
			c.lru.Remove(el)
			delete(c.items, item.key)
		}
		el = prev
	}
}

func (c *Cache) evictOldest() {
	if el := c.lru.Back(); el != nil {
		c.lru.Remove(el)
		delete(c.items, el.Value.(*cacheItem).key)
	}
}

// Keys returns all cached keys.
func (c *Cache) Keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]string, 0, len(c.items))
	for k := range c.items {
		keys = append(keys, k)
	}
	return keys
}

// FormatStats returns a human-readable stats string.
func (c *Cache) FormatStats() string {
	hits, misses := c.Stats()
	total := hits + misses
	rate := c.HitRate()
	return fmt.Sprintf("cache: %.1f%% hit (%d/%d items, %d evictions)",
		rate, hits, total, c.Size())
}

// ── Typed cache wrappers ──────────────────────────────────

// StringCache is a cache specialized for string values.
type StringCache struct {
	inner *Cache
}

// NewStringCache creates a cache for strings.
func NewStringCache(maxItems int, ttl time.Duration) *StringCache {
	return &StringCache{inner: New(maxItems, ttl)}
}

func (c *StringCache) Set(k, v string) { c.inner.Set(k, v) }
func (c *StringCache) Get(k string) (string, bool) {
	v, ok := c.inner.Get(k)
	if !ok {
		return "", false
	}
	return v.(string), true
}

// IntCache is a cache specialized for int values.
type IntCache struct {
	inner *Cache
}

// NewIntCache creates a cache for integers.
func NewIntCache(maxItems int, ttl time.Duration) *IntCache {
	return &IntCache{inner: New(maxItems, ttl)}
}

func (c *IntCache) Set(k string, v int) { c.inner.Set(k, v) }
func (c *IntCache) Get(k string) (int, bool) {
	v, ok := c.inner.Get(k)
	if !ok {
		return 0, false
	}
	return v.(int), true
}
