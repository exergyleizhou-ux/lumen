// Package fluid provides fluid data structures: RingBuffer, LRU Cache with TTL,
// Bloom Filter, SkipList, and Count-Min Sketch. All are generic and concurrency-safe
// where noted.
package fluid

import (
	"container/heap"
	"encoding/binary"
	"hash"
	"hash/fnv"
	"math"
	"math/bits"
	"math/rand"
	"sync"
	"time"
)

// ---- RingBuffer ----

// RingBuffer is a fixed-size circular buffer. It is not concurrency-safe;
// wrap with a mutex if needed.
type RingBuffer[T any] struct {
	buf      []T
	head     int
	tail     int
	size     int
	capacity int
}

// NewRingBuffer creates a RingBuffer with the given capacity.
// Capacity must be > 0.
func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	if capacity < 1 {
		capacity = 16
	}
	return &RingBuffer[T]{
		buf:      make([]T, capacity),
		capacity: capacity,
	}
}

// Push adds an element. If the buffer is full, the oldest element is dropped.
func (rb *RingBuffer[T]) Push(v T) {
	rb.buf[rb.tail] = v
	rb.tail = (rb.tail + 1) % rb.capacity
	if rb.size == rb.capacity {
		rb.head = (rb.head + 1) % rb.capacity
	} else {
		rb.size++
	}
}

// Pop removes and returns the oldest element.
// The second return value is false if the buffer is empty.
func (rb *RingBuffer[T]) Pop() (T, bool) {
	var zero T
	if rb.size == 0 {
		return zero, false
	}
	v := rb.buf[rb.head]
	rb.head = (rb.head + 1) % rb.capacity
	rb.size--
	return v, true
}

// Peek returns the oldest element without removing it.
func (rb *RingBuffer[T]) Peek() (T, bool) {
	var zero T
	if rb.size == 0 {
		return zero, false
	}
	return rb.buf[rb.head], true
}

// PeekAt returns the element at a given index (0 = oldest).
func (rb *RingBuffer[T]) PeekAt(idx int) (T, bool) {
	var zero T
	if idx < 0 || idx >= rb.size {
		return zero, false
	}
	return rb.buf[(rb.head+idx)%rb.capacity], true
}

// Len returns the number of elements currently in the buffer.
func (rb *RingBuffer[T]) Len() int { return rb.size }

// Cap returns the buffer capacity.
func (rb *RingBuffer[T]) Cap() int { return rb.capacity }

// IsFull returns true if the buffer is at capacity.
func (rb *RingBuffer[T]) IsFull() bool { return rb.size == rb.capacity }

// IsEmpty returns true if the buffer is empty.
func (rb *RingBuffer[T]) IsEmpty() bool { return rb.size == 0 }

// Clear removes all elements.
func (rb *RingBuffer[T]) Clear() {
	rb.head = 0
	rb.tail = 0
	rb.size = 0
}

// Values returns a slice of all elements in order (oldest first).
func (rb *RingBuffer[T]) Values() []T {
	out := make([]T, rb.size)
	for i := 0; i < rb.size; i++ {
		out[i] = rb.buf[(rb.head+i)%rb.capacity]
	}
	return out
}

// Drain removes and returns all elements.
func (rb *RingBuffer[T]) Drain() []T {
	out := rb.Values()
	rb.Clear()
	return out
}

// ---- LRU Cache with TTL ----

// lruEntry holds a cached value with expiration.
type lruEntry[T any] struct {
	key       string
	value     T
	expiresAt time.Time
}

// LRUCache is a generic LRU cache with optional TTL.
// It is concurrency-safe.
type LRUCache[T any] struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	items    map[string]*listNode[*lruEntry[T]]
	list     *doublyLinkedList[*lruEntry[T]]
}

type listNode[T any] struct {
	value T
	prev  *listNode[T]
	next  *listNode[T]
}

type doublyLinkedList[T any] struct {
	head *listNode[T]
	tail *listNode[T]
}

func newDoublyLinkedList[T any]() *doublyLinkedList[T] {
	return &doublyLinkedList[T]{}
}

func (l *doublyLinkedList[T]) pushFront(v T) *listNode[T] {
	n := &listNode[T]{value: v}
	if l.head == nil {
		l.head = n
		l.tail = n
	} else {
		n.next = l.head
		l.head.prev = n
		l.head = n
	}
	return n
}

func (l *doublyLinkedList[T]) remove(n *listNode[T]) {
	if n.prev != nil {
		n.prev.next = n.next
	} else {
		l.head = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else {
		l.tail = n.prev
	}
}

func (l *doublyLinkedList[T]) moveToFront(n *listNode[T]) {
	l.remove(n)
	n.prev = nil
	n.next = l.head
	if l.head != nil {
		l.head.prev = n
	}
	l.head = n
	if l.tail == nil {
		l.tail = n
	}
}

func (l *doublyLinkedList[T]) popBack() *listNode[T] {
	if l.tail == nil {
		return nil
	}
	n := l.tail
	l.remove(n)
	return n
}

// NewLRUCache creates an LRU cache with the given capacity and optional TTL.
// Zero or negative TTL means no expiration.
func NewLRUCache[T any](capacity int, ttl time.Duration) *LRUCache[T] {
	if capacity < 1 {
		capacity = 64
	}
	return &LRUCache[T]{
		capacity: capacity,
		ttl:      ttl,
		items:    make(map[string]*listNode[*lruEntry[T]]),
		list:     newDoublyLinkedList[*lruEntry[T]](),
	}
}

// Get returns a cached value by key. Returns the value and true on hit,
// or the zero value and false on miss / expiry.
func (c *LRUCache[T]) Get(key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	node, ok := c.items[key]
	if !ok {
		var zero T
		return zero, false
	}

	entry := node.value
	if c.ttl > 0 && time.Now().After(entry.expiresAt) {
		c.list.remove(node)
		delete(c.items, key)
		var zero T
		return zero, false
	}

	c.list.moveToFront(node)
	return entry.value, true
}

// Set inserts or updates a key with the given value.
func (c *LRUCache[T]) Set(key string, value T) {
	c.mu.Lock()
	defer c.mu.Unlock()

	expiresAt := time.Time{}
	if c.ttl > 0 {
		expiresAt = time.Now().Add(c.ttl)
	}

	if node, ok := c.items[key]; ok {
		node.value.value = value
		node.value.expiresAt = expiresAt
		c.list.moveToFront(node)
		return
	}

	entry := &lruEntry[T]{key: key, value: value, expiresAt: expiresAt}
	node := c.list.pushFront(entry)
	c.items[key] = node

	if len(c.items) > c.capacity {
		oldest := c.list.popBack()
		if oldest != nil {
			delete(c.items, oldest.value.key)
		}
	}
}

// Delete removes a key from the cache.
func (c *LRUCache[T]) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if node, ok := c.items[key]; ok {
		c.list.remove(node)
		delete(c.items, key)
	}
}

// Has returns true if the key exists and is not expired.
func (c *LRUCache[T]) Has(key string) bool {
	_, ok := c.Get(key)
	return ok
}

// Len returns the number of items in the cache.
func (c *LRUCache[T]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// Cap returns the cache capacity.
func (c *LRUCache[T]) Cap() int { return c.capacity }

// Clear removes all items.
func (c *LRUCache[T]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*listNode[*lruEntry[T]])
	c.list = newDoublyLinkedList[*lruEntry[T]]()
}

// Keys returns all non-expired keys in the cache.
func (c *LRUCache[T]) Keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	keys := make([]string, 0, len(c.items))
	for k, node := range c.items {
		if c.ttl <= 0 || now.Before(node.value.expiresAt) {
			keys = append(keys, k)
		}
	}
	return keys
}

// PurgeExpired removes all expired entries.
func (c *LRUCache[T]) PurgeExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ttl <= 0 {
		return 0
	}
	now := time.Now()
	count := 0
	for k, node := range c.items {
		if now.After(node.value.expiresAt) {
			c.list.remove(node)
			delete(c.items, k)
			count++
		}
	}
	return count
}

// ---- Bloom Filter ----

// BloomFilter is a space-efficient probabilistic set data structure.
// False positives are possible; false negatives are not.
// It is concurrency-safe.
type BloomFilter struct {
	mu      sync.RWMutex
	bits    []uint64
	size    uint64 // number of bits (m)
	numHash int    // number of hash functions (k)
	count   uint64 // number of items added
}

// NewBloomFilter creates a Bloom filter with the given capacity and false-positive rate.
func NewBloomFilter(expectedItems int, falsePositiveRate float64) *BloomFilter {
	if expectedItems < 1 {
		expectedItems = 1000
	}
	if falsePositiveRate <= 0 || falsePositiveRate >= 1 {
		falsePositiveRate = 0.01
	}
	// Optimal m = -n * ln(p) / (ln(2)^2)
	n := float64(expectedItems)
	p := falsePositiveRate
	m := -n * math.Log(p) / (math.Ln2 * math.Ln2)
	k := (m / n) * math.Ln2

	size := uint64(math.Ceil(m))
	numHash := int(math.Ceil(k))
	if numHash < 1 {
		numHash = 1
	}
	words := (size + 63) / 64
	if words < 1 {
		words = 1
	}

	return &BloomFilter{
		bits:    make([]uint64, words),
		size:    words * 64,
		numHash: numHash,
	}
}

// NewBloomFilterWithParams creates a filter with explicit size and hash count.
func NewBloomFilterWithParams(numBits uint64, numHashes int) *BloomFilter {
	if numBits < 1 {
		numBits = 64
	}
	if numHashes < 1 {
		numHashes = 1
	}
	words := (numBits + 63) / 64
	return &BloomFilter{
		bits:    make([]uint64, words),
		size:    words * 64,
		numHash: numHashes,
	}
}

// Add inserts an item into the filter.
func (bf *BloomFilter) Add(data []byte) {
	bf.mu.Lock()
	defer bf.mu.Unlock()

	h1, h2 := bf.hash(data)
	for i := 0; i < bf.numHash; i++ {
		pos := (h1 + uint64(i)*h2) % bf.size
		word := pos / 64
		bit := pos % 64
		bf.bits[word] |= 1 << bit
	}
	bf.count++
}

// AddString inserts a string item.
func (bf *BloomFilter) AddString(s string) {
	bf.Add([]byte(s))
}

// Contains tests whether an item may be in the filter.
// Returns true if the item might be present (could be a false positive),
// false if the item is definitely not present.
func (bf *BloomFilter) Contains(data []byte) bool {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	h1, h2 := bf.hash(data)
	for i := 0; i < bf.numHash; i++ {
		pos := (h1 + uint64(i)*h2) % bf.size
		word := pos / 64
		bit := pos % 64
		if bf.bits[word]&(1<<bit) == 0 {
			return false
		}
	}
	return true
}

// ContainsString tests a string.
func (bf *BloomFilter) ContainsString(s string) bool {
	return bf.Contains([]byte(s))
}

// Count returns the approximate number of items added.
func (bf *BloomFilter) Count() uint64 {
	bf.mu.RLock()
	defer bf.mu.RUnlock()
	return bf.count
}

// EstimatedFalsePositiveRate computes the current approximate FPR.
func (bf *BloomFilter) EstimatedFalsePositiveRate() float64 {
	bf.mu.RLock()
	defer bf.mu.RUnlock()
	// (1 - e^(-k*n/m))^k
	k := float64(bf.numHash)
	n := float64(bf.count)
	m := float64(bf.size)
	return math.Pow(1-math.Exp(-k*n/m), k)
}

// Clear resets the filter.
func (bf *BloomFilter) Clear() {
	bf.mu.Lock()
	defer bf.mu.Unlock()
	for i := range bf.bits {
		bf.bits[i] = 0
	}
	bf.count = 0
}

// BitCount returns the number of set bits.
func (bf *BloomFilter) BitCount() int {
	bf.mu.RLock()
	defer bf.mu.RUnlock()
	count := 0
	for _, w := range bf.bits {
		count += bits.OnesCount64(w)
	}
	return count
}

// Merge combines another Bloom filter into this one (bitwise OR).
// Both must have the same size and hash count.
func (bf *BloomFilter) Merge(other *BloomFilter) bool {
	if bf.size != other.size || bf.numHash != other.numHash {
		return false
	}
	bf.mu.Lock()
	defer bf.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()
	for i := range bf.bits {
		bf.bits[i] |= other.bits[i]
	}
	bf.count += other.count
	return true
}

func (bf *BloomFilter) hash(data []byte) (uint64, uint64) {
	h := fnv.New128a()
	h.Write(data)
	sum := h.Sum(nil)
	h1 := binary.BigEndian.Uint64(sum[:8])
	h2 := binary.BigEndian.Uint64(sum[8:])
	return h1, h2
}

// hashWriter is unused but kept for hash.Hash interface satisfaction tests
var _ hash.Hash = fnv.New128a()

// ---- SkipList ----

// SkipList is a probabilistic sorted data structure supporting O(log n)
// insertion, deletion, and lookup. Not concurrency-safe.
type SkipList[K comparable, V any] struct {
	head     *skipNode[K, V]
	less     func(a, b K) bool
	maxLevel int
	level    int
	length   int
	rng      *rand.Rand
}

type skipNode[K comparable, V any] struct {
	key     K
	value   V
	forward []*skipNode[K, V]
}

const skipListDefaultMaxLevel = 32
const skipListP = 0.5

// NewSkipList creates a SkipList with a custom less comparator.
func NewSkipList[K comparable, V any](less func(a, b K) bool) *SkipList[K, V] {
	return NewSkipListWithMaxLevel[K, V](less, skipListDefaultMaxLevel)
}

// NewSkipListWithMaxLevel creates a SkipList with a custom max level.
func NewSkipListWithMaxLevel[K comparable, V any](less func(a, b K) bool, maxLevel int) *SkipList[K, V] {
	if maxLevel < 1 {
		maxLevel = skipListDefaultMaxLevel
	}
	head := &skipNode[K, V]{
		forward: make([]*skipNode[K, V], maxLevel),
	}
	return &SkipList[K, V]{
		head:     head,
		less:     less,
		maxLevel: maxLevel,
		level:    1,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Insert adds or updates a key-value pair.
func (sl *SkipList[K, V]) Insert(key K, value V) {
	update := make([]*skipNode[K, V], sl.maxLevel)
	current := sl.head

	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && sl.less(current.forward[i].key, key) {
			current = current.forward[i]
		}
		update[i] = current
	}

	current = current.forward[0]

	if current != nil && !sl.less(key, current.key) && !sl.less(current.key, key) {
		current.value = value
		return
	}

	newLevel := sl.randomLevel()
	if newLevel > sl.level {
		for i := sl.level; i < newLevel; i++ {
			update[i] = sl.head
		}
		sl.level = newLevel
	}

	node := &skipNode[K, V]{
		key:     key,
		value:   value,
		forward: make([]*skipNode[K, V], newLevel),
	}

	for i := 0; i < newLevel; i++ {
		node.forward[i] = update[i].forward[i]
		update[i].forward[i] = node
	}
	sl.length++
}

// Search looks up a key. Returns value and true if found.
func (sl *SkipList[K, V]) Search(key K) (V, bool) {
	current := sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && sl.less(current.forward[i].key, key) {
			current = current.forward[i]
		}
	}
	current = current.forward[0]
	if current != nil && !sl.less(key, current.key) && !sl.less(current.key, key) {
		return current.value, true
	}
	var zero V
	return zero, false
}

// Delete removes a key. Returns true if the key was present.
func (sl *SkipList[K, V]) Delete(key K) bool {
	update := make([]*skipNode[K, V], sl.maxLevel)
	current := sl.head

	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && sl.less(current.forward[i].key, key) {
			current = current.forward[i]
		}
		update[i] = current
	}

	current = current.forward[0]
	if current == nil || sl.less(key, current.key) || sl.less(current.key, key) {
		return false
	}

	for i := 0; i < sl.level; i++ {
		if update[i].forward[i] != current {
			break
		}
		update[i].forward[i] = current.forward[i]
	}

	for sl.level > 1 && sl.head.forward[sl.level-1] == nil {
		sl.level--
	}

	sl.length--
	return true
}

// Len returns the number of elements.
func (sl *SkipList[K, V]) Len() int { return sl.length }

// Keys returns all keys in sorted order.
func (sl *SkipList[K, V]) Keys() []K {
	keys := make([]K, 0, sl.length)
	for n := sl.head.forward[0]; n != nil; n = n.forward[0] {
		keys = append(keys, n.key)
	}
	return keys
}

// Values returns all values in key-sorted order.
func (sl *SkipList[K, V]) Values() []V {
	vals := make([]V, 0, sl.length)
	for n := sl.head.forward[0]; n != nil; n = n.forward[0] {
		vals = append(vals, n.value)
	}
	return vals
}

// Range calls fn for each element between start and end (inclusive).
func (sl *SkipList[K, V]) Range(start, end K, fn func(key K, value V) bool) {
	for n := sl.head.forward[0]; n != nil; n = n.forward[0] {
		if sl.less(n.key, start) {
			continue
		}
		if sl.less(end, n.key) {
			return
		}
		if !fn(n.key, n.value) {
			return
		}
	}
}

// Min returns the smallest key-value pair.
func (sl *SkipList[K, V]) Min() (K, V, bool) {
	n := sl.head.forward[0]
	if n == nil {
		var zeroK K
		var zeroV V
		return zeroK, zeroV, false
	}
	return n.key, n.value, true
}

// Max returns the largest key-value pair.
func (sl *SkipList[K, V]) Max() (K, V, bool) {
	current := sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil {
			current = current.forward[i]
		}
	}
	if current == sl.head {
		var zeroK K
		var zeroV V
		return zeroK, zeroV, false
	}
	return current.key, current.value, true
}

// Floor returns the largest element <= key.
func (sl *SkipList[K, V]) Floor(key K) (K, V, bool) {
	current := sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && !sl.less(key, current.forward[i].key) {
			current = current.forward[i]
		}
	}
	if current == sl.head {
		var zeroK K
		var zeroV V
		return zeroK, zeroV, false
	}
	return current.key, current.value, true
}

// Ceil returns the smallest element >= key.
func (sl *SkipList[K, V]) Ceil(key K) (K, V, bool) {
	current := sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && sl.less(current.forward[i].key, key) {
			current = current.forward[i]
		}
	}
	current = current.forward[0]
	if current == nil {
		var zeroK K
		var zeroV V
		return zeroK, zeroV, false
	}
	return current.key, current.value, true
}

func (sl *SkipList[K, V]) randomLevel() int {
	level := 1
	for sl.rng.Float64() < skipListP && level < sl.maxLevel {
		level++
	}
	return level
}

// ---- Count-Min Sketch ----

// CountMinSketch is a probabilistic frequency counter.
// It uses multiple hash functions and a 2D table to estimate
// the frequency of items with bounded over-estimation.
// It is concurrency-safe.
type CountMinSketch struct {
	mu     sync.RWMutex
	table  [][]uint64
	width  int // number of columns
	depth  int // number of rows (hash functions)
	hashes []hash.Hash64
	total  uint64
}

// NewCountMinSketch creates a Count-Min Sketch with the given epsilon and delta.
// epsilon = error factor (e.g., 0.001 for 0.1% error)
// delta = probability of exceeding the error bound (e.g., 0.01 for 1%)
func NewCountMinSketch(epsilon, delta float64) *CountMinSketch {
	if epsilon <= 0 || epsilon >= 1 {
		epsilon = 0.001
	}
	if delta <= 0 || delta >= 1 {
		delta = 0.01
	}

	width := int(math.Ceil(math.E / epsilon))
	depth := int(math.Ceil(math.Log(1.0 / delta)))

	if width < 1 {
		width = 100
	}
	if depth < 1 {
		depth = 3
	}

	return NewCountMinSketchWithParams(width, depth)
}

// NewCountMinSketchWithParams creates a sketch with explicit dimensions.
func NewCountMinSketchWithParams(width, depth int) *CountMinSketch {
	if width < 1 {
		width = 100
	}
	if depth < 1 {
		depth = 3
	}

	table := make([][]uint64, depth)
	for i := range table {
		table[i] = make([]uint64, width)
	}

	hashes := make([]hash.Hash64, depth)
	for i := range hashes {
		h := fnv.New64a()
		// Salt each hash differently
		salt := []byte{byte(i), byte(i >> 8), byte(i >> 16), byte(i >> 24)}
		h.Write(salt)
		hashes[i] = h
	}

	return &CountMinSketch{
		table:  table,
		width:  width,
		depth:  depth,
		hashes: hashes,
	}
}

// Increment adds count to the item's estimated frequency.
func (cms *CountMinSketch) Increment(data []byte, count uint64) {
	cms.mu.Lock()
	defer cms.mu.Unlock()

	for i := 0; i < cms.depth; i++ {
		h := cms.hashAt(i, data)
		col := h % uint64(cms.width)
		cms.table[i][col] += count
	}
	cms.total += count
}

// IncrementString is a convenience method for strings.
func (cms *CountMinSketch) IncrementString(s string, count uint64) {
	cms.Increment([]byte(s), count)
}

// Estimate returns the estimated frequency of an item.
func (cms *CountMinSketch) Estimate(data []byte) uint64 {
	cms.mu.RLock()
	defer cms.mu.RUnlock()

	min := uint64(math.MaxUint64)
	for i := 0; i < cms.depth; i++ {
		h := cms.hashAt(i, data)
		col := h % uint64(cms.width)
		if cms.table[i][col] < min {
			min = cms.table[i][col]
		}
	}
	return min
}

// EstimateString is a convenience method for strings.
func (cms *CountMinSketch) EstimateString(s string) uint64 {
	return cms.Estimate([]byte(s))
}

// Total returns the total count added.
func (cms *CountMinSketch) Total() uint64 {
	cms.mu.RLock()
	defer cms.mu.RUnlock()
	return cms.total
}

// Merge combines another Count-Min Sketch into this one.
// Both must have the same dimensions.
func (cms *CountMinSketch) Merge(other *CountMinSketch) bool {
	if cms.width != other.width || cms.depth != other.depth {
		return false
	}
	cms.mu.Lock()
	defer cms.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()

	for i := 0; i < cms.depth; i++ {
		for j := 0; j < cms.width; j++ {
			cms.table[i][j] += other.table[i][j]
		}
	}
	cms.total += other.total
	return true
}

// Clear resets the sketch.
func (cms *CountMinSketch) Clear() {
	cms.mu.Lock()
	defer cms.mu.Unlock()
	for i := range cms.table {
		for j := range cms.table[i] {
			cms.table[i][j] = 0
		}
	}
	cms.total = 0
}

// Dimensions returns width and depth.
func (cms *CountMinSketch) Dimensions() (width, depth int) {
	return cms.width, cms.depth
}

func (cms *CountMinSketch) hashAt(idx int, data []byte) uint64 {
	h := cms.hashes[idx]
	h.Reset()
	h.Write(data)
	return h.Sum64()
}

// ---- Utility types ----

// IntLess is a Less comparator for integers.
func IntLess(a, b int) bool { return a < b }

// StringLess is a Less comparator for strings.
func StringLess(a, b string) bool { return a < b }

// ---- PriorityQueue (bonus heap-backed priority queue) ----

// PriorityQueue is a heap-backed generic priority queue.
type PriorityQueue[T any] struct {
	items []pqItem[T]
	less  func(a, b T) bool
}

type pqItem[T any] struct {
	value    T
	priority int
	index    int
}

// NewPriorityQueue creates a priority queue with a custom comparator.
func NewPriorityQueue[T any](less func(a, b T) bool) *PriorityQueue[T] {
	pq := &PriorityQueue[T]{
		items: make([]pqItem[T], 0),
		less:  less,
	}
	heap.Init((*pqHeap[T])(pq))
	return pq
}

// Push adds an item with the given priority.
func (pq *PriorityQueue[T]) Push(value T, priority int) {
	item := pqItem[T]{value: value, priority: priority}
	heap.Push((*pqHeap[T])(pq), item)
}

// Pop removes and returns the highest-priority item.
func (pq *PriorityQueue[T]) Pop() (T, bool) {
	if len(pq.items) == 0 {
		var zero T
		return zero, false
	}
	item := heap.Pop((*pqHeap[T])(pq)).(pqItem[T])
	return item.value, true
}

// Peek returns the highest-priority item without removing it.
func (pq *PriorityQueue[T]) Peek() (T, bool) {
	if len(pq.items) == 0 {
		var zero T
		return zero, false
	}
	return pq.items[0].value, true
}

// Len returns the number of items.
func (pq *PriorityQueue[T]) Len() int { return len(pq.items) }

// pqHeap adapts PriorityQueue to heap.Interface.
type pqHeap[T any] PriorityQueue[T]

func (h *pqHeap[T]) Len() int           { return len(h.items) }
func (h *pqHeap[T]) Less(i, j int) bool { return h.items[i].priority < h.items[j].priority }
func (h *pqHeap[T]) Swap(i, j int) {
	h.items[i], h.items[j] = h.items[j], h.items[i]
	h.items[i].index = i
	h.items[j].index = j
}

func (h *pqHeap[T]) Push(x interface{}) {
	item := x.(pqItem[T])
	item.index = len(h.items)
	h.items = append(h.items, item)
}

func (h *pqHeap[T]) Pop() interface{} {
	old := h.items
	n := len(old)
	item := old[n-1]
	old[n-1] = pqItem[T]{} // avoid memory leak
	h.items = old[:n-1]
	return item
}
