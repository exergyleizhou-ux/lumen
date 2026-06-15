package clustermap

import (
	"testing"
	"time"
)

func TestConsistentHashRing_AddGetNode(t *testing.T) {
	chr := NewConsistentHashRing(10)
	chr.AddNode("node-a")
	chr.AddNode("node-b")

	node, ok := chr.GetNode("my-key")
	if !ok {
		t.Fatal("GetNode failed")
	}
	if node != "node-a" && node != "node-b" {
		t.Errorf("unexpected node: %s", node)
	}
}

func TestConsistentHashRing_Deterministic(t *testing.T) {
	chr := NewConsistentHashRing(10)
	chr.AddNode("a")
	chr.AddNode("b")

	n1, _ := chr.GetNode("key1")
	n2, _ := chr.GetNode("key1")
	if n1 != n2 {
		t.Error("same key should always map to same node")
	}
}

func TestConsistentHashRing_RemoveNode(t *testing.T) {
	chr := NewConsistentHashRing(10)
	chr.AddNode("a")
	chr.AddNode("b")
	chr.RemoveNode("a")

	node, ok := chr.GetNode("any-key")
	if !ok || node != "b" {
		t.Errorf("expected 'b' after removing 'a', got %s", node)
	}
}

func TestConsistentHashRing_Empty(t *testing.T) {
	chr := NewConsistentHashRing(10)
	_, ok := chr.GetNode("key")
	if ok {
		t.Error("empty ring should return false")
	}
}

func TestConsistentHashRing_GetNodes(t *testing.T) {
	chr := NewConsistentHashRing(10)
	chr.AddNode("a")
	chr.AddNode("b")
	chr.AddNode("c")

	nodes := chr.GetNodes("key", 2)
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0] == nodes[1] {
		t.Error("should return distinct nodes")
	}
}

func TestDistributedCounter(t *testing.T) {
	dc := NewDistributedCounter("test-counter")
	dc.Increment(5, "node-a")
	dc.Increment(3, "node-b")

	if dc.Value() != 8 {
		t.Errorf("expected 8, got %d", dc.Value())
	}

	val, hist := dc.Snapshot()
	if val != 8 {
		t.Errorf("snapshot val: expected 8, got %d", val)
	}
	if len(hist) != 2 {
		t.Errorf("expected 2 history events, got %d", len(hist))
	}
}

func TestClusterLock_TryAcquireRelease(t *testing.T) {
	cl := NewClusterLock("test-lock", 5*time.Second)

	if !cl.TryAcquire("node-1") {
		t.Error("should acquire free lock")
	}
	if cl.TryAcquire("node-2") {
		t.Error("should not acquire held lock")
	}
	if !cl.IsOwner("node-1") {
		t.Error("node-1 should be owner")
	}
	if !cl.Release("node-1") {
		t.Error("should release owned lock")
	}
	if cl.Release("node-3") {
		t.Error("should not release unowned lock")
	}
}

func TestClusterLock_Expiry(t *testing.T) {
	cl := NewClusterLock("exp-lock", 1*time.Millisecond)
	cl.TryAcquire("node-1")
	time.Sleep(10 * time.Millisecond)

	// Lock should be expired
	if cl.IsOwner("node-1") {
		t.Error("lock should be expired")
	}
	if !cl.TryAcquire("node-2") {
		t.Error("should acquire expired lock")
	}
}

func TestClusterLock_Owner(t *testing.T) {
	cl := NewClusterLock("lock", 10*time.Second)
	if cl.Owner() != "" {
		t.Error("unowned lock should return empty owner")
	}
	cl.TryAcquire("node-x")
	if cl.Owner() != "node-x" {
		t.Errorf("expected 'node-x', got '%s'", cl.Owner())
	}
}

func TestQuorumVoter(t *testing.T) {
	qv := NewQuorumVoter(5, 3)
	qv.Cast("yes")
	qv.Cast("yes")
	qv.Cast("no")

	result := qv.Tally()
	if !result.Passed {
		t.Error("quorum should pass with 3 votes")
	}
	if result.Votes != 3 {
		t.Errorf("expected 3 votes, got %d", result.Votes)
	}
}

func TestQuorumVoter_NotEnough(t *testing.T) {
	qv := NewQuorumVoter(5, 3)
	qv.Cast("yes")
	qv.Cast("yes")

	result := qv.Tally()
	if result.Passed {
		t.Error("quorum should not pass with 2 votes")
	}
}

func TestQuorumVoter_Reset(t *testing.T) {
	qv := NewQuorumVoter(5, 3)
	qv.Cast("yes")
	qv.Reset()

	result := qv.Tally()
	if result.Passed {
		t.Error("should be no votes after reset")
	}
}

func TestMetadataStore(t *testing.T) {
	ms := NewMetadataStore()
	ms.Set("node-1", "zone", "us-east")
	ms.Set("node-1", "version", "2.0")

	v, ok := ms.Get("node-1", "zone")
	if !ok || v != "us-east" {
		t.Errorf("unexpected zone: %s", v)
	}

	_, ok = ms.Get("node-2", "zone")
	if ok {
		t.Error("should not find unknown node")
	}

	all := ms.All("node-1")
	if len(all) != 2 {
		t.Errorf("expected 2 entries, got %d", len(all))
	}
}

func TestNodeHash(t *testing.T) {
	h := NodeHash("test-node")
	if h == 0 {
		t.Error("hash should not be zero")
	}
}
