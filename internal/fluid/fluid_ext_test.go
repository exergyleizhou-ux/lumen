package fluid

import (
	"testing"
	"time"
)

func TestHyperLogLog_Basic(t *testing.T) {
	hll := NewHyperLogLog(10)

	for i := 0; i < 1000; i++ {
		hll.Add([]byte{byte(i), byte(i >> 8)})
	}

	est := hll.Estimate()
	t.Logf("HLL estimate for 1000 unique: %d", est)
	if est < 500 || est > 2000 {
		t.Errorf("estimate %d is way off from 1000", est)
	}
}

func TestHyperLogLog_Empty(t *testing.T) {
	hll := NewHyperLogLog(8)
	if hll.Estimate() != 0 {
		t.Errorf("empty HLL should estimate 0, got %d", hll.Estimate())
	}
}

func TestHyperLogLog_Merge(t *testing.T) {
	hll1 := NewHyperLogLog(10)
	hll2 := NewHyperLogLog(10)

	for i := 0; i < 500; i++ {
		hll1.Add([]byte{byte(i), byte(i >> 8), byte(i >> 16), byte(i >> 24)})
	}
	for i := 250; i < 750; i++ {
		hll2.Add([]byte{byte(i), byte(i >> 8), byte(i >> 16), byte(i >> 24)})
	}

	if !hll1.Merge(hll2) {
		t.Error("merge should succeed")
	}
	est := hll1.Estimate()
	t.Logf("Merged estimate: %d", est)
	if est < 400 || est > 1500 {
		t.Errorf("merged estimate %d unreasonable for 750 unique items", est)
	}
}

func TestHyperLogLog_MergeMismatch(t *testing.T) {
	hll1 := NewHyperLogLog(8)
	hll2 := NewHyperLogLog(10)
	if hll1.Merge(hll2) {
		t.Error("merge should fail for different sizes")
	}
}

func TestMinHash_Basic(t *testing.T) {
	mh := NewMinHash(50)

	setA := [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")}
	setB := [][]byte{[]byte("c"), []byte("d"), []byte("e"), []byte("f")}

	sigA := mh.Signature(setA)
	sigB := mh.Signature(setB)

	sim := mh.Similarity(sigA, sigB)
	t.Logf("MinHash similarity: %.4f (expected ~0.5)", sim)
	if sim < 0.2 || sim > 0.8 {
		t.Errorf("similarity %.4f seems off for 50%% overlap", sim)
	}
}

func TestMinHash_Identical(t *testing.T) {
	mh := NewMinHash(50)
	items := [][]byte{[]byte("x"), []byte("y")}
	s1 := mh.Signature(items)
	s2 := mh.Signature(items)
	if mh.Similarity(s1, s2) != 1.0 {
		t.Error("identical sets should have similarity 1.0")
	}
}

func TestTrie_InsertSearch(t *testing.T) {
	tr := NewTrie()
	tr.Insert("hello", 1)
	tr.Insert("world", 2)

	v, ok := tr.Search("hello")
	if !ok || v != 1 {
		t.Errorf("expected 1, got %v (ok=%v)", v, ok)
	}

	_, ok = tr.Search("missing")
	if ok {
		t.Error("should not find missing word")
	}
}

func TestTrie_StartsWith(t *testing.T) {
	tr := NewTrie()
	tr.Insert("hello", nil)
	tr.Insert("help", nil)
	tr.Insert("helicopter", nil)
	tr.Insert("world", nil)

	results := tr.StartsWith("hel")
	if len(results) != 3 {
		t.Errorf("expected 3 words starting with 'hel', got %d: %v", len(results), results)
	}
}

func TestTrie_Delete(t *testing.T) {
	tr := NewTrie()
	tr.Insert("test", nil)
	if tr.Len() != 1 {
		t.Error("len should be 1")
	}
	if !tr.Delete("test") {
		t.Error("delete should succeed")
	}
	if tr.Len() != 0 {
		t.Error("len should be 0 after delete")
	}
	_, ok := tr.Search("test")
	if ok {
		t.Error("should not find deleted word")
	}
}

func TestTrie_DeleteMissing(t *testing.T) {
	tr := NewTrie()
	if tr.Delete("missing") {
		t.Error("delete should fail for missing word")
	}
}

func TestRateLimiter_Allow(t *testing.T) {
	rl := NewRateLimiter(100, 10) // 100 tokens/sec, burst 10

	// Should allow burst
	for i := 0; i < 10; i++ {
		if !rl.Allow() {
			t.Errorf("should allow request %d", i+1)
		}
	}
	// Next one should be denied (no tokens left)
	if rl.Allow() {
		t.Error("should deny after burst exhausted")
	}
}

func TestRateLimiter_Refill(t *testing.T) {
	rl := NewRateLimiter(1000, 1)
	rl.Allow() // consume the burst token

	// Wait for refill
	time.Sleep(5 * time.Millisecond)

	if !rl.Allow() {
		t.Error("should allow after refill")
	}
}

func TestRateLimiter_Tokens(t *testing.T) {
	rl := NewRateLimiter(100, 5)
	tokens := rl.Tokens()
	if tokens < 4.9 || tokens > 5.1 {
		t.Errorf("expected ~5 tokens, got %f", tokens)
	}
}

func TestSlidingWindowCounter(t *testing.T) {
	swc := NewSlidingWindowCounter(time.Second, 10)
	swc.Increment()
	swc.Increment()
	swc.Increment()

	count := swc.Count()
	if count != 3 {
		t.Errorf("expected 3, got %d", count)
	}
}

func TestSlidingWindowCounter_Expiry(t *testing.T) {
	swc := NewSlidingWindowCounter(10*time.Millisecond, 5)
	swc.Increment()
	time.Sleep(20 * time.Millisecond)
	swc.Increment()

	count := swc.Count()
	if count > 2 {
		t.Errorf("old counts should expire: got %d", count)
	}
}

func TestExpiringSet_Basic(t *testing.T) {
	es := NewExpiringSet(100 * time.Millisecond)
	es.Add("item1")

	if !es.Contains("item1") {
		t.Error("should contain item1")
	}
	if es.Contains("item2") {
		t.Error("should not contain item2")
	}
}

func TestExpiringSet_Expiry(t *testing.T) {
	es := NewExpiringSet(5 * time.Millisecond)
	es.Add("temp")
	time.Sleep(20 * time.Millisecond)

	if es.Contains("temp") {
		t.Error("should expire")
	}
}

func TestExpiringSet_Purge(t *testing.T) {
	es := NewExpiringSet(5 * time.Millisecond)
	es.Add("a")
	es.Add("b")
	time.Sleep(20 * time.Millisecond)

	purged := es.Purge()
	if purged != 2 {
		t.Errorf("expected 2 purged, got %d", purged)
	}
	if es.Len() != 0 {
		t.Error("should be empty after purge")
	}
}
