// Package clustermap - extension: consistent hashing, distributed counters,
// cluster-wide locks, quorum-based decisions.
package clustermap

import (
	"crypto/md5"
	"fmt"
	"hash/crc32"
	"sort"
	"sync"
	"time"
)

// ---- Consistent Hashing ----

// ConsistentHashRing maps keys to nodes using consistent hashing.
type ConsistentHashRing struct {
	mu       sync.RWMutex
	nodes    map[uint32]NodeID
	sortedKeys []uint32
	replicas int
}

// NewConsistentHashRing creates a consistent hash ring.
func NewConsistentHashRing(replicas int) *ConsistentHashRing {
	if replicas < 1 {
		replicas = 100
	}
	return &ConsistentHashRing{
		nodes:    make(map[uint32]NodeID),
		replicas: replicas,
	}
}

// AddNode adds a node to the ring.
func (chr *ConsistentHashRing) AddNode(id NodeID) {
	chr.mu.Lock()
	defer chr.mu.Unlock()

	for i := 0; i < chr.replicas; i++ {
		key := chr.hash(fmt.Sprintf("%s:%d", id, i))
		chr.nodes[key] = id
		chr.sortedKeys = append(chr.sortedKeys, key)
	}
	sort.Slice(chr.sortedKeys, func(i, j int) bool {
		return chr.sortedKeys[i] < chr.sortedKeys[j]
	})
}

// RemoveNode removes a node from the ring.
func (chr *ConsistentHashRing) RemoveNode(id NodeID) {
	chr.mu.Lock()
	defer chr.mu.Unlock()

	var newKeys []uint32
	for _, key := range chr.sortedKeys {
		if chr.nodes[key] != id {
			newKeys = append(newKeys, key)
		} else {
			delete(chr.nodes, key)
		}
	}
	chr.sortedKeys = newKeys
}

// GetNode returns the node responsible for a key.
func (chr *ConsistentHashRing) GetNode(key string) (NodeID, bool) {
	chr.mu.RLock()
	defer chr.mu.RUnlock()

	if len(chr.sortedKeys) == 0 {
		return "", false
	}

	hash := chr.hash(key)
	idx := sort.Search(len(chr.sortedKeys), func(i int) bool {
		return chr.sortedKeys[i] >= hash
	})

	if idx >= len(chr.sortedKeys) {
		idx = 0
	}

	return chr.nodes[chr.sortedKeys[idx]], true
}

// GetNodes returns N distinct nodes for a key (for replication).
func (chr *ConsistentHashRing) GetNodes(key string, n int) []NodeID {
	chr.mu.RLock()
	defer chr.mu.RUnlock()

	if len(chr.sortedKeys) == 0 {
		return nil
	}

	hash := chr.hash(key)
	idx := sort.Search(len(chr.sortedKeys), func(i int) bool {
		return chr.sortedKeys[i] >= hash
	})
	if idx >= len(chr.sortedKeys) {
		idx = 0
	}

	seen := make(map[NodeID]bool)
	var result []NodeID
	for i := 0; i < len(chr.sortedKeys) && len(result) < n; i++ {
		nodeID := chr.nodes[chr.sortedKeys[(idx+i)%len(chr.sortedKeys)]]
		if !seen[nodeID] {
			seen[nodeID] = true
			result = append(result, nodeID)
		}
	}
	return result
}

func (chr *ConsistentHashRing) hash(key string) uint32 {
	return crc32.ChecksumIEEE([]byte(key))
}

// ---- Distributed Counter ----

// DistributedCounter is a counter that can be incremented across nodes.
type DistributedCounter struct {
	mu    sync.RWMutex
	count int64
	name  string
	history []CounterEvent
}

// CounterEvent records a change to the counter.
type CounterEvent struct {
	Delta     int64
	Timestamp time.Time
	Source    NodeID
}

// NewDistributedCounter creates a distributed counter.
func NewDistributedCounter(name string) *DistributedCounter {
	return &DistributedCounter{
		name:    name,
		history: make([]CounterEvent, 0),
	}
}

// Increment adds to the counter.
func (dc *DistributedCounter) Increment(delta int64, source NodeID) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.count += delta
	dc.history = append(dc.history, CounterEvent{
		Delta: delta, Timestamp: time.Now(), Source: source,
	})
}

// Value returns the current count.
func (dc *DistributedCounter) Value() int64 {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return dc.count
}

// Snapshot returns count and recent history.
func (dc *DistributedCounter) Snapshot() (int64, []CounterEvent) {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	hist := make([]CounterEvent, len(dc.history))
	copy(hist, dc.history)
	return dc.count, hist
}

// ---- Cluster Lock ----

// ClusterLock provides a distributed mutual exclusion lock.
type ClusterLock struct {
	mu       sync.Mutex
	name     string
	owner    NodeID
	acquired time.Time
	ttl      time.Duration
	waiters  []chan bool
}

// NewClusterLock creates a cluster-wide lock.
func NewClusterLock(name string, ttl time.Duration) *ClusterLock {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &ClusterLock{
		name: name,
		ttl:  ttl,
	}
}

// TryAcquire attempts to acquire the lock.
func (cl *ClusterLock) TryAcquire(node NodeID) bool {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	if cl.owner == "" || time.Since(cl.acquired) > cl.ttl {
		cl.owner = node
		cl.acquired = time.Now()
		return true
	}
	return false
}

// Release releases the lock if owned by the given node.
func (cl *ClusterLock) Release(node NodeID) bool {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	if cl.owner == node {
		cl.owner = ""
		return true
	}
	return false
}

// IsOwner returns true if the node holds the lock.
func (cl *ClusterLock) IsOwner(node NodeID) bool {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return cl.owner == node && time.Since(cl.acquired) <= cl.ttl
}

// Owner returns the current lock owner.
func (cl *ClusterLock) Owner() NodeID {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.owner != "" && time.Since(cl.acquired) <= cl.ttl {
		return cl.owner
	}
	return ""
}

// ---- Quorum ----

// QuorumResult holds the result of a quorum vote.
type QuorumResult struct {
	Passed   bool
	Votes    int
	Required int
	Total    int
}

// QuorumVoter manages quorum-based decisions.
type QuorumVoter struct {
	mu    sync.Mutex
	votes map[string]int // decision -> count
	total int
	quorum int
}

// NewQuorumVoter creates a quorum voter.
func NewQuorumVoter(totalNodes, quorum int) *QuorumVoter {
	if quorum < 1 {
		quorum = totalNodes/2 + 1
	}
	return &QuorumVoter{
		votes:  make(map[string]int),
		total:  totalNodes,
		quorum: quorum,
	}
}

// Cast records a vote.
func (qv *QuorumVoter) Cast(choice string) {
	qv.mu.Lock()
	defer qv.mu.Unlock()
	qv.votes[choice]++
}

// Tally counts votes and determines if quorum is reached.
func (qv *QuorumVoter) Tally() *QuorumResult {
	qv.mu.Lock()
	defer qv.mu.Unlock()

	var totalVotes int
	var maxChoice string
	var maxVotes int
	for choice, count := range qv.votes {
		totalVotes += count
		if count > maxVotes {
			maxVotes = count
			maxChoice = choice
		}
	}

	_ = maxChoice
	return &QuorumResult{
		Passed:   totalVotes >= qv.quorum,
		Votes:    totalVotes,
		Required: qv.quorum,
		Total:    qv.total,
	}
}

// Reset clears all votes.
func (qv *QuorumVoter) Reset() {
	qv.mu.Lock()
	defer qv.mu.Unlock()
	qv.votes = make(map[string]int)
}

// ---- Node metadata store ----

// MetadataStore holds key-value metadata for cluster nodes.
type MetadataStore struct {
	mu   sync.RWMutex
	data map[NodeID]map[string]string
}

// NewMetadataStore creates a metadata store.
func NewMetadataStore() *MetadataStore {
	return &MetadataStore{
		data: make(map[NodeID]map[string]string),
	}
}

// Set sets metadata for a node.
func (ms *MetadataStore) Set(node NodeID, key, value string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.data[node] == nil {
		ms.data[node] = make(map[string]string)
	}
	ms.data[node][key] = value
}

// Get retrieves metadata for a node.
func (ms *MetadataStore) Get(node NodeID, key string) (string, bool) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	if m, ok := ms.data[node]; ok {
		v, exists := m[key]
		return v, exists
	}
	return "", false
}

// All returns all metadata for a node.
func (ms *MetadataStore) All(node NodeID) map[string]string {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	if m, ok := ms.data[node]; ok {
		result := make(map[string]string, len(m))
		for k, v := range m {
			result[k] = v
		}
		return result
	}
	return nil
}

// ---- Node hash utility ----

// NodeHash computes a simple hash for a node.
func NodeHash(id NodeID) uint64 {
	h := md5.Sum([]byte(id))
	return uint64(h[0])<<56 | uint64(h[1])<<48 | uint64(h[2])<<40 | uint64(h[3])<<32 |
		uint64(h[4])<<24 | uint64(h[5])<<16 | uint64(h[6])<<8 | uint64(h[7])
}
