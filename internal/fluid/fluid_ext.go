// Package fluid - extension: HyperLogLog, MinHash, Trie, RateLimiter (token bucket),
// expiring HashSet, sliding window counter.
package fluid

import (
	"hash/fnv"
	"math"
	"math/bits"
	"sync"
	"time"
)

// ---- HyperLogLog ----

// HyperLogLog is a probabilistic cardinality estimator.
type HyperLogLog struct {
	mu        sync.RWMutex
	registers []uint8
	m         uint32 // number of registers
	alpha     float64
}

// NewHyperLogLog creates a HyperLogLog with the given precision.
// Precision p: number of registers = 2^p; typical p=14 (16384 registers).
func NewHyperLogLog(p uint32) *HyperLogLog {
	if p < 4 {
		p = 4
	}
	if p > 18 {
		p = 18
	}
	m := uint32(1) << p
	alpha := 0.7213 / (1 + 1.079/float64(m))
	return &HyperLogLog{
		registers: make([]uint8, m),
		m:         m,
		alpha:     alpha,
	}
}

// Add inserts an item into the estimator.
func (hll *HyperLogLog) Add(data []byte) {
	hll.mu.Lock()
	defer hll.mu.Unlock()

	// Use FNV-1a 64-bit hash
	var hash uint64 = 14695981039346656037 // FNV offset basis
	for _, b := range data {
		hash ^= uint64(b)
		hash *= 1099511628211 // FNV prime
	}
	// Mix hash further for better distribution
	hash ^= hash >> 33
	hash *= 0xff51afd7ed558ccd
	hash ^= hash >> 33

	// Determine p = log2(m)
	p := 0
	for m := hll.m; m > 1; m >>= 1 {
		p++
	}

	// First p bits index the register
	idx := int(hash >> (64 - p))

	// Remaining 64-p bits: count leading zeros + 1
	w := hash << p
	rho := uint8(1)
	if w != 0 {
		rho = uint8(bits.LeadingZeros64(w) + 1)
	} else {
		rho = uint8(64 - p + 1)
	}
	if rho > 64 {
		rho = 64
	}

	if rho > hll.registers[idx] {
		hll.registers[idx] = rho
	}
}

// Estimate returns the estimated cardinality.
func (hll *HyperLogLog) Estimate() uint64 {
	hll.mu.RLock()
	defer hll.mu.RUnlock()

	var sum float64
	var zeros int
	for _, r := range hll.registers {
		// Compute 2^(-r) as 1.0 / 2^r
		sum += 1.0 / float64(uint64(1)<<r)
		if r == 0 {
			zeros++
		}
	}

	m := float64(hll.m)
	estimate := hll.alpha * m * m / sum

	// Small range correction
	if estimate <= 2.5*m && zeros > 0 {
		estimate = m * math.Log(m/float64(zeros))
	}

	// Large range correction
	if estimate > float64(uint64(1)<<32)/30 {
		estimate = -float64(uint64(1)<<32) * math.Log(1-estimate/float64(uint64(1)<<32))
	}

	return uint64(estimate + 0.5)
}

// Merge combines another HyperLogLog into this one.
func (hll *HyperLogLog) Merge(other *HyperLogLog) bool {
	if hll.m != other.m {
		return false
	}
	hll.mu.Lock()
	defer hll.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()

	for i := range hll.registers {
		if other.registers[i] > hll.registers[i] {
			hll.registers[i] = other.registers[i]
		}
	}
	return true
}

// ---- MinHash ----

// MinHash computes MinHash signatures for set similarity estimation.
type MinHash struct {
	numHashes int
	hashFuncs []func([]byte) uint64
}

// NewMinHash creates a MinHash with the given number of hash functions.
func NewMinHash(numHashes int) *MinHash {
	if numHashes < 1 {
		numHashes = 100
	}
	mh := &MinHash{
		numHashes: numHashes,
		hashFuncs: make([]func([]byte) uint64, numHashes),
	}
	for i := range mh.hashFuncs {
		seed := uint64(i + 1)
		mh.hashFuncs[i] = func(data []byte) uint64 {
			h := fnv.New64a()
			h.Write([]byte{byte(seed), byte(seed >> 8), byte(seed >> 16), byte(seed >> 24)})
			h.Write(data)
			return h.Sum64()
		}
	}
	return mh
}

// Signature computes the MinHash signature for a set of items.
func (mh *MinHash) Signature(items [][]byte) []uint64 {
	sig := make([]uint64, mh.numHashes)
	for i := range sig {
		sig[i] = math.MaxUint64
	}
	for _, item := range items {
		for i, hf := range mh.hashFuncs {
			h := hf(item)
			if h < sig[i] {
				sig[i] = h
			}
		}
	}
	return sig
}

// Similarity estimates Jaccard similarity between two signatures.
func (mh *MinHash) Similarity(a, b []uint64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	match := 0
	for i := range a {
		if a[i] == b[i] {
			match++
		}
	}
	return float64(match) / float64(len(a))
}

// ---- Trie ----

// TrieNode is a node in a trie.
type TrieNode struct {
	children map[rune]*TrieNode
	isEnd    bool
	value    interface{}
}

// Trie is a prefix tree for string keys.
type Trie struct {
	root *TrieNode
	size int
}

// NewTrie creates an empty trie.
func NewTrie() *Trie {
	return &Trie{
		root: &TrieNode{children: make(map[rune]*TrieNode)},
	}
}

// Insert adds a word to the trie.
func (tr *Trie) Insert(word string, value interface{}) {
	node := tr.root
	for _, ch := range word {
		if node.children[ch] == nil {
			node.children[ch] = &TrieNode{children: make(map[rune]*TrieNode)}
		}
		node = node.children[ch]
	}
	if !node.isEnd {
		tr.size++
	}
	node.isEnd = true
	node.value = value
}

// Search checks if a word exists in the trie.
func (tr *Trie) Search(word string) (interface{}, bool) {
	node := tr.root
	for _, ch := range word {
		if node.children[ch] == nil {
			return nil, false
		}
		node = node.children[ch]
	}
	if node.isEnd {
		return node.value, true
	}
	return nil, false
}

// StartsWith returns all words with the given prefix.
func (tr *Trie) StartsWith(prefix string) []string {
	node := tr.root
	for _, ch := range prefix {
		if node.children[ch] == nil {
			return nil
		}
		node = node.children[ch]
	}
	var results []string
	tr.collect(node, prefix, &results)
	return results
}

func (tr *Trie) collect(node *TrieNode, prefix string, results *[]string) {
	if node.isEnd {
		*results = append(*results, prefix)
	}
	for ch, child := range node.children {
		tr.collect(child, prefix+string(ch), results)
	}
}

// Delete removes a word from the trie.
func (tr *Trie) Delete(word string) bool {
	return tr.deleteRecursive(tr.root, word, 0)
}

func (tr *Trie) deleteRecursive(node *TrieNode, word string, depth int) bool {
	if depth == len(word) {
		if !node.isEnd {
			return false
		}
		node.isEnd = false
		tr.size--
		return len(node.children) == 0
	}
	ch := rune(word[depth])
	child, ok := node.children[ch]
	if !ok {
		return false
	}
	shouldDelete := tr.deleteRecursive(child, word, depth+1)
	if shouldDelete {
		delete(node.children, ch)
		return len(node.children) == 0 && !node.isEnd
	}
	return false
}

// Len returns the number of words in the trie.
func (tr *Trie) Len() int { return tr.size }

// ---- Token Bucket Rate Limiter ----

// RateLimiter implements a token bucket rate limiter.
type RateLimiter struct {
	mu         sync.Mutex
	rate       float64 // tokens per second
	burst      float64 // max tokens
	tokens     float64
	lastRefill time.Time
}

// NewRateLimiter creates a rate limiter.
func NewRateLimiter(rate, burst float64) *RateLimiter {
	if rate <= 0 {
		rate = 1
	}
	if burst <= 0 {
		burst = rate
	}
	return &RateLimiter{
		rate:       rate,
		burst:      burst,
		tokens:     burst,
		lastRefill: time.Now(),
	}
}

// Allow checks if a request is allowed. Consumes 1 token if allowed.
func (rl *RateLimiter) Allow() bool {
	return rl.AllowN(1)
}

// AllowN checks if n tokens can be consumed.
func (rl *RateLimiter) AllowN(n float64) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()
	rl.tokens += elapsed * rl.rate
	if rl.tokens > rl.burst {
		rl.tokens = rl.burst
	}
	rl.lastRefill = now

	if rl.tokens >= n {
		rl.tokens -= n
		return true
	}
	return false
}

// Tokens returns the current number of available tokens.
func (rl *RateLimiter) Tokens() float64 {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()
	tokens := rl.tokens + elapsed*rl.rate
	if tokens > rl.burst {
		tokens = rl.burst
	}
	return tokens
}

// ---- Sliding Window Counter ----

// SlidingWindowCounter counts events within a sliding time window.
type SlidingWindowCounter struct {
	mu             sync.Mutex
	buckets        []int64
	window         time.Duration
	bucketDuration time.Duration
	lastIdx        int
	lastTime       time.Time
}

// NewSlidingWindowCounter creates a sliding window counter.
func NewSlidingWindowCounter(window time.Duration, numBuckets int) *SlidingWindowCounter {
	if numBuckets < 1 {
		numBuckets = 10
	}
	return &SlidingWindowCounter{
		buckets:        make([]int64, numBuckets),
		window:         window,
		bucketDuration: window / time.Duration(numBuckets),
		lastTime:       time.Now(),
	}
}

// Increment adds 1 to the counter.
func (swc *SlidingWindowCounter) Increment() {
	swc.mu.Lock()
	defer swc.mu.Unlock()
	swc.advance()
	swc.buckets[swc.lastIdx]++
}

// Count returns the total count in the window.
func (swc *SlidingWindowCounter) Count() int64 {
	swc.mu.Lock()
	defer swc.mu.Unlock()
	swc.advance()
	var total int64
	for _, b := range swc.buckets {
		total += b
	}
	return total
}

func (swc *SlidingWindowCounter) advance() {
	now := time.Now()
	elapsed := now.Sub(swc.lastTime)
	steps := int(elapsed / swc.bucketDuration)
	if steps > len(swc.buckets) {
		steps = len(swc.buckets)
	}
	for i := 0; i < steps; i++ {
		swc.lastIdx = (swc.lastIdx + 1) % len(swc.buckets)
		swc.buckets[swc.lastIdx] = 0
	}
	if steps > 0 {
		swc.lastTime = now
	}
}

// ---- Expiring Set ----

// ExpiringSet is a set where entries expire after a TTL.
type ExpiringSet struct {
	mu    sync.RWMutex
	items map[string]time.Time
	ttl   time.Duration
}

// NewExpiringSet creates an expiring set.
func NewExpiringSet(ttl time.Duration) *ExpiringSet {
	return &ExpiringSet{
		items: make(map[string]time.Time),
		ttl:   ttl,
	}
}

// Add inserts an item.
func (es *ExpiringSet) Add(item string) {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.items[item] = time.Now().Add(es.ttl)
}

// Contains checks membership.
func (es *ExpiringSet) Contains(item string) bool {
	es.mu.RLock()
	defer es.mu.RUnlock()
	expiry, ok := es.items[item]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		return false
	}
	return true
}

// Purge removes expired items.
func (es *ExpiringSet) Purge() int {
	es.mu.Lock()
	defer es.mu.Unlock()
	now := time.Now()
	count := 0
	for k, v := range es.items {
		if now.After(v) {
			delete(es.items, k)
			count++
		}
	}
	return count
}

// Len returns the current size including expired items.
func (es *ExpiringSet) Len() int {
	es.mu.RLock()
	defer es.mu.RUnlock()
	return len(es.items)
}
