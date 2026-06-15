package fluid

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- RingBuffer Tests ----

func TestRingBuffer_PushPop(t *testing.T) {
	rb := NewRingBuffer[int](5)
	for i := 0; i < 3; i++ {
		rb.Push(i)
	}
	if rb.Len() != 3 {
		t.Errorf("expected len 3, got %d", rb.Len())
	}
	for i := 0; i < 3; i++ {
		v, ok := rb.Pop()
		if !ok || v != i {
			t.Errorf("expected %d, got %d (ok=%v)", i, v, ok)
		}
	}
	if !rb.IsEmpty() {
		t.Error("should be empty after pops")
	}
}

func TestRingBuffer_Wraparound(t *testing.T) {
	rb := NewRingBuffer[int](3)
	rb.Push(1)
	rb.Push(2)
	rb.Push(3)
	if !rb.IsFull() {
		t.Error("should be full")
	}
	rb.Push(4) // overwrites 1
	if rb.Len() != 3 {
		t.Errorf("expected len 3, got %d", rb.Len())
	}
	v, _ := rb.Pop()
	if v != 2 {
		t.Errorf("expected 2, got %d", v)
	}
}

func TestRingBuffer_Peek(t *testing.T) {
	rb := NewRingBuffer[int](5)
	rb.Push(10)
	rb.Push(20)
	v, ok := rb.Peek()
	if !ok || v != 10 {
		t.Errorf("expected 10, got %d", v)
	}
	if rb.Len() != 2 {
		t.Error("peek should not change length")
	}
}

func TestRingBuffer_PeekAt(t *testing.T) {
	rb := NewRingBuffer[int](5)
	rb.Push(10)
	rb.Push(20)
	rb.Push(30)
	v, ok := rb.PeekAt(1)
	if !ok || v != 20 {
		t.Errorf("expected 20, got %d", v)
	}
	_, ok = rb.PeekAt(5)
	if ok {
		t.Error("expected false for out-of-bounds")
	}
}

func TestRingBuffer_Clear(t *testing.T) {
	rb := NewRingBuffer[int](3)
	rb.Push(1)
	rb.Push(2)
	rb.Clear()
	if !rb.IsEmpty() {
		t.Error("should be empty after clear")
	}
}

func TestRingBuffer_Values(t *testing.T) {
	rb := NewRingBuffer[int](5)
	rb.Push(1)
	rb.Push(2)
	rb.Push(3)
	vals := rb.Values()
	if len(vals) != 3 || vals[0] != 1 || vals[2] != 3 {
		t.Errorf("unexpected values: %v", vals)
	}
}

func TestRingBuffer_Drain(t *testing.T) {
	rb := NewRingBuffer[int](3)
	rb.Push(1)
	rb.Push(2)
	vals := rb.Drain()
	if len(vals) != 2 {
		t.Errorf("expected 2, got %d", len(vals))
	}
	if !rb.IsEmpty() {
		t.Error("should be empty after drain")
	}
}

func TestRingBuffer_PopEmpty(t *testing.T) {
	rb := NewRingBuffer[int](3)
	_, ok := rb.Pop()
	if ok {
		t.Error("expected false for pop on empty")
	}
}

// ---- LRU Cache Tests ----

func TestLRUCache_Basic(t *testing.T) {
	c := NewLRUCache[int](3, 0)
	c.Set("a", 1)
	c.Set("b", 2)

	v, ok := c.Get("a")
	if !ok || v != 1 {
		t.Errorf("expected 1, got %d (ok=%v)", v, ok)
	}
	if c.Len() != 2 {
		t.Errorf("expected len 2, got %d", c.Len())
	}
}

func TestLRUCache_Eviction(t *testing.T) {
	c := NewLRUCache[int](2, 0)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3) // evicts a

	_, ok := c.Get("a")
	if ok {
		t.Error("a should have been evicted")
	}
	v, ok := c.Get("c")
	if !ok || v != 3 {
		t.Error("c should be present")
	}
}

func TestLRUCache_TTL(t *testing.T) {
	c := NewLRUCache[int](5, 50*time.Millisecond)
	c.Set("a", 1)

	v, ok := c.Get("a")
	if !ok || v != 1 {
		t.Error("should get value before expiry")
	}

	time.Sleep(100 * time.Millisecond)
	_, ok = c.Get("a")
	if ok {
		t.Error("should expire after TTL")
	}
}

func TestLRUCache_UpdateResetsPosition(t *testing.T) {
	c := NewLRUCache[int](2, 0)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("a", 10) // should move a to front
	c.Set("c", 3)  // should evict b (not a)

	_, ok := c.Get("b")
	if ok {
		t.Error("b should have been evicted")
	}
	v, ok := c.Get("a")
	if !ok || v != 10 {
		t.Errorf("a should be present with value 10, got %d", v)
	}
}

func TestLRUCache_Delete(t *testing.T) {
	c := NewLRUCache[int](3, 0)
	c.Set("a", 1)
	c.Delete("a")
	_, ok := c.Get("a")
	if ok {
		t.Error("a should be deleted")
	}
	if c.Len() != 0 {
		t.Error("len should be 0")
	}
}

func TestLRUCache_Has(t *testing.T) {
	c := NewLRUCache[int](3, 0)
	c.Set("a", 1)
	if !c.Has("a") {
		t.Error("Has should return true")
	}
	if c.Has("b") {
		t.Error("Has should return false")
	}
}

func TestLRUCache_Keys(t *testing.T) {
	c := NewLRUCache[int](5, 0)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3)
	keys := c.Keys()
	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(keys))
	}
}

func TestLRUCache_Clear(t *testing.T) {
	c := NewLRUCache[int](3, 0)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Clear()
	if c.Len() != 0 {
		t.Error("should be empty after clear")
	}
}

func TestLRUCache_PurgeExpired(t *testing.T) {
	c := NewLRUCache[int](5, 10*time.Millisecond)
	c.Set("a", 1)
	c.Set("b", 2)
	time.Sleep(30 * time.Millisecond)
	n := c.PurgeExpired()
	if n != 2 {
		t.Errorf("expected 2 purged, got %d", n)
	}
}

func TestLRUCache_Concurrency(t *testing.T) {
	c := NewLRUCache[int](100, 0)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := string(rune('a' + (idx % 26)))
			c.Set(key, idx)
			c.Get(key)
		}(i)
	}
	wg.Wait()
}

// ---- Bloom Filter Tests ----

func TestBloomFilter_AddContains(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01)
	bf.AddString("hello")
	bf.AddString("world")

	if !bf.ContainsString("hello") {
		t.Error("should contain 'hello'")
	}
	if !bf.ContainsString("world") {
		t.Error("should contain 'world'")
	}
	if bf.ContainsString("missing") {
		t.Error("should not contain 'missing'")
	}
}

func TestBloomFilter_FalsePositiveRate(t *testing.T) {
	bf := NewBloomFilter(10000, 0.01)
	// Add 1000 items
	for i := 0; i < 1000; i++ {
		bf.AddString(string(rune(i)))
	}
	fpr := bf.EstimatedFalsePositiveRate()
	t.Logf("Estimated FPR: %.6f", fpr)
	if fpr > 0.1 {
		t.Errorf("FPR too high: %.6f", fpr)
	}
}

func TestBloomFilter_Count(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01)
	for i := 0; i < 100; i++ {
		bf.AddString("item")
	}
	if bf.Count() != 100 {
		t.Errorf("expected count 100, got %d", bf.Count())
	}
}

func TestBloomFilter_Clear(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01)
	bf.AddString("test")
	bf.Clear()
	if bf.ContainsString("test") {
		t.Error("should not contain after clear")
	}
}

func TestBloomFilter_Merge(t *testing.T) {
	bf1 := NewBloomFilterWithParams(256, 3)
	bf2 := NewBloomFilterWithParams(256, 3)

	bf1.AddString("a")
	bf2.AddString("b")

	if !bf1.Merge(bf2) {
		t.Error("merge should succeed for same params")
	}
	if !bf1.ContainsString("a") {
		t.Error("should still contain 'a'")
	}
	if !bf1.ContainsString("b") {
		t.Error("should now contain 'b'")
	}
}

func TestBloomFilter_MergeMismatch(t *testing.T) {
	bf1 := NewBloomFilterWithParams(256, 3)
	bf2 := NewBloomFilterWithParams(512, 4)
	if bf1.Merge(bf2) {
		t.Error("merge should fail for different params")
	}
}

func TestBloomFilter_Concurrency(t *testing.T) {
	bf := NewBloomFilter(10000, 0.01)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s := string(rune('a' + (idx % 26)))
			bf.AddString(s)
			bf.ContainsString(s)
		}(i)
	}
	wg.Wait()
}

// ---- SkipList Tests ----

func TestSkipList_InsertSearch(t *testing.T) {
	sl := NewSkipList[int, string](IntLess)
	sl.Insert(5, "five")
	sl.Insert(3, "three")
	sl.Insert(7, "seven")

	v, ok := sl.Search(5)
	if !ok || v != "five" {
		t.Errorf("expected 'five', got %q (ok=%v)", v, ok)
	}
	_, ok = sl.Search(99)
	if ok {
		t.Error("should not find 99")
	}
}

func TestSkipList_InsertUpdate(t *testing.T) {
	sl := NewSkipList[int, string](IntLess)
	sl.Insert(1, "one")
	sl.Insert(1, "ONE")
	v, _ := sl.Search(1)
	if v != "ONE" {
		t.Errorf("expected 'ONE', got %q", v)
	}
}

func TestSkipList_Delete(t *testing.T) {
	sl := NewSkipList[int, string](IntLess)
	sl.Insert(1, "one")
	sl.Insert(2, "two")

	if !sl.Delete(1) {
		t.Error("delete should succeed")
	}
	_, ok := sl.Search(1)
	if ok {
		t.Error("1 should be deleted")
	}
	if sl.Len() != 1 {
		t.Errorf("expected len 1, got %d", sl.Len())
	}
	if sl.Delete(99) {
		t.Error("delete should fail for missing key")
	}
}

func TestSkipList_Keys(t *testing.T) {
	sl := NewSkipList[int, string](IntLess)
	sl.Insert(3, "c")
	sl.Insert(1, "a")
	sl.Insert(2, "b")

	keys := sl.Keys()
	if len(keys) != 3 || keys[0] != 1 || keys[1] != 2 || keys[2] != 3 {
		t.Errorf("unexpected keys: %v", keys)
	}
}

func TestSkipList_Values(t *testing.T) {
	sl := NewSkipList[int, string](IntLess)
	sl.Insert(3, "c")
	sl.Insert(1, "a")
	sl.Insert(2, "b")
	vals := sl.Values()
	if len(vals) != 3 || vals[0] != "a" || vals[2] != "c" {
		t.Errorf("unexpected values: %v", vals)
	}
}

func TestSkipList_Range(t *testing.T) {
	sl := NewSkipList[int, int](IntLess)
	for i := 1; i <= 10; i++ {
		sl.Insert(i, i*10)
	}

	var sum int
	sl.Range(3, 7, func(k, v int) bool {
		sum += v
		return true
	})
	if sum != 30+40+50+60+70 {
		t.Errorf("unexpected sum: %d", sum)
	}
}

func TestSkipList_MinMax(t *testing.T) {
	sl := NewSkipList[int, string](IntLess)
	k, v, ok := sl.Min()
	if ok {
		t.Error("min on empty should be false")
	}

	sl.Insert(10, "ten")
	sl.Insert(5, "five")
	sl.Insert(15, "fifteen")

	k, v, ok = sl.Min()
	if !ok || k != 5 || v != "five" {
		t.Errorf("min: got %d/%q", k, v)
	}

	k, v, ok = sl.Max()
	if !ok || k != 15 || v != "fifteen" {
		t.Errorf("max: got %d/%q", k, v)
	}
}

func TestSkipList_FloorCeil(t *testing.T) {
	sl := NewSkipList[int, string](IntLess)
	sl.Insert(10, "ten")
	sl.Insert(20, "twenty")
	sl.Insert(30, "thirty")

	k, _, ok := sl.Floor(25)
	if !ok || k != 20 {
		t.Errorf("floor of 25 should be 20, got %d", k)
	}

	k, _, ok = sl.Ceil(25)
	if !ok || k != 30 {
		t.Errorf("ceil of 25 should be 30, got %d", k)
	}

	_, _, ok = sl.Floor(5)
	if ok {
		t.Error("floor of 5 should not exist")
	}

	k, _, ok = sl.Ceil(5)
	if !ok || k != 10 {
		t.Errorf("ceil of 5 should be 10, got %d", k)
	}
}

func TestSkipList_StringType(t *testing.T) {
	sl := NewSkipList[string, int](StringLess)
	sl.Insert("banana", 2)
	sl.Insert("apple", 1)
	sl.Insert("cherry", 3)

	keys := sl.Keys()
	if keys[0] != "apple" || keys[1] != "banana" || keys[2] != "cherry" {
		t.Errorf("unexpected string order: %v", keys)
	}
}

// ---- Count-Min Sketch Tests ----

func TestCountMinSketch_Basic(t *testing.T) {
	cms := NewCountMinSketch(0.001, 0.01)
	cms.IncrementString("hello", 5)
	cms.IncrementString("hello", 3)
	cms.IncrementString("world", 1)

	est := cms.EstimateString("hello")
	if est < 8 {
		t.Errorf("estimate for 'hello' should be >= 8, got %d", est)
	}
	est = cms.EstimateString("world")
	if est < 1 {
		t.Errorf("estimate for 'world' should be >= 1, got %d", est)
	}
	est = cms.EstimateString("missing")
	if est > 8 {
		t.Errorf("estimate for 'missing' should be low, got %d", est)
	}
}

func TestCountMinSketch_Total(t *testing.T) {
	cms := NewCountMinSketch(0.001, 0.01)
	cms.IncrementString("a", 10)
	cms.IncrementString("b", 5)
	if cms.Total() != 15 {
		t.Errorf("expected total 15, got %d", cms.Total())
	}
}

func TestCountMinSketch_Clear(t *testing.T) {
	cms := NewCountMinSketch(0.001, 0.01)
	cms.IncrementString("x", 100)
	cms.Clear()
	if cms.Total() != 0 {
		t.Error("total should be 0 after clear")
	}
	if cms.EstimateString("x") != 0 {
		t.Error("estimate should be 0 after clear")
	}
}

func TestCountMinSketch_Merge(t *testing.T) {
	w, d := 200, 4
	cms1 := NewCountMinSketchWithParams(w, d)
	cms2 := NewCountMinSketchWithParams(w, d)

	cms1.IncrementString("shared", 10)
	cms2.IncrementString("shared", 5)
	cms2.IncrementString("unique", 3)

	if !cms1.Merge(cms2) {
		t.Error("merge should succeed")
	}
	if cms1.Total() != 18 {
		t.Errorf("expected total 18, got %d", cms1.Total())
	}
	if cms1.EstimateString("shared") < 15 {
		t.Error("estimate for 'shared' should be >= 15")
	}
	if cms1.EstimateString("unique") < 3 {
		t.Error("estimate for 'unique' should be >= 3")
	}
}

func TestCountMinSketch_MergeMismatch(t *testing.T) {
	cms1 := NewCountMinSketchWithParams(100, 3)
	cms2 := NewCountMinSketchWithParams(200, 4)
	if cms1.Merge(cms2) {
		t.Error("merge should fail for mismatched dimensions")
	}
}

func TestCountMinSketch_Dimensions(t *testing.T) {
	cms := NewCountMinSketchWithParams(150, 5)
	w, d := cms.Dimensions()
	if w != 150 || d != 5 {
		t.Errorf("expected 150x5, got %dx%d", w, d)
	}
}

func TestCountMinSketch_Concurrency(t *testing.T) {
	cms := NewCountMinSketch(0.001, 0.01)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cms.IncrementString("concurrent", 1)
		}(i)
	}
	wg.Wait()
	if cms.Total() != 100 {
		t.Errorf("expected total 100, got %d", cms.Total())
	}
}

// ---- PriorityQueue Tests ----

func TestPriorityQueue_Basic(t *testing.T) {
	pq := NewPriorityQueue[string](StringLess)
	pq.Push("low", 10)
	pq.Push("high", 1)
	pq.Push("mid", 5)

	v, ok := pq.Pop()
	if !ok || v != "high" {
		t.Errorf("expected 'high', got %q", v)
	}
	v, ok = pq.Pop()
	if !ok || v != "mid" {
		t.Errorf("expected 'mid', got %q", v)
	}
	v, ok = pq.Pop()
	if !ok || v != "low" {
		t.Errorf("expected 'low', got %q", v)
	}
	_, ok = pq.Pop()
	if ok {
		t.Error("should be empty")
	}
}

func TestPriorityQueue_Peek(t *testing.T) {
	pq := NewPriorityQueue[int](IntLess)
	pq.Push(100, 10)
	v, ok := pq.Peek()
	if !ok || v != 100 {
		t.Errorf("expected 100, got %d", v)
	}
	if pq.Len() != 1 {
		t.Error("peek should not change length")
	}
}

func TestPriorityQueue_Empty(t *testing.T) {
	pq := NewPriorityQueue[int](IntLess)
	_, ok := pq.Peek()
	if ok {
		t.Error("peek on empty should return false")
	}
	_, ok = pq.Pop()
	if ok {
		t.Error("pop on empty should return false")
	}
}

// ---- StringLess / IntLess Tests ----

func TestIntLess(t *testing.T) {
	if !IntLess(1, 2) {
		t.Error("1 < 2 should be true")
	}
	if IntLess(3, 2) {
		t.Error("3 < 2 should be false")
	}
}

func TestStringLess(t *testing.T) {
	if !StringLess("a", "b") {
		t.Error("'a' < 'b' should be true")
	}
	if StringLess("z", "a") {
		t.Error("'z' < 'a' should be false")
	}
}

// ---- LRU TTL extended test ----

func TestLRUCache_TTL_NoExpiryWhenZero(t *testing.T) {
	c := NewLRUCache[int](3, 0)
	c.Set("a", 1)
	time.Sleep(10 * time.Millisecond)
	_, ok := c.Get("a")
	if !ok {
		t.Error("should not expire when TTL is zero")
	}
}

// ---- Large RingBuffer test ----

func TestRingBuffer_LargeCapacity(t *testing.T) {
	rb := NewRingBuffer[int](10000)
	for i := 0; i < 5000; i++ {
		rb.Push(i)
	}
	if rb.Len() != 5000 {
		t.Errorf("expected len 5000, got %d", rb.Len())
	}
}

// ---- BloomFilter BitCount ----

func TestBloomFilter_BitCount(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01)
	bf.AddString("test")
	bc := bf.BitCount()
	if bc == 0 {
		t.Error("bit count should be > 0 after add")
	}
}

// ---- SkipList empty tests ----

func TestSkipList_EmptyMinMax(t *testing.T) {
	sl := NewSkipList[int, string](IntLess)
	_, _, ok := sl.Min()
	if ok {
		t.Error("min on empty should be false")
	}
	_, _, ok = sl.Max()
	if ok {
		t.Error("max on empty should be false")
	}
}

// ---- CountMinSketch default params ----

func TestCountMinSketch_DefaultParams(t *testing.T) {
	cms := NewCountMinSketch(0, 2) // invalid values should fall back to defaults
	w, d := cms.Dimensions()
	if w < 1 || d < 1 {
		t.Error("should have valid dimensions with default params")
	}
}

// ---- Benchmark-style stress tests ----

func TestSkipList_LargeInsert(t *testing.T) {
	sl := NewSkipList[int, int](IntLess)
	n := 1000
	for i := 0; i < n; i++ {
		sl.Insert(i, i*2)
	}
	if sl.Len() != n {
		t.Errorf("expected len %d, got %d", n, sl.Len())
	}
	// Search all
	for i := 0; i < n; i++ {
		v, ok := sl.Search(i)
		if !ok || v != i*2 {
			t.Errorf("search miss for %d", i)
		}
	}
}

func TestLRUCache_CapacityDefault(t *testing.T) {
	c := NewLRUCache[int](0, 0) // should default to 64
	if c.Cap() < 1 {
		t.Error("capacity should be > 0")
	}
}

// ---- Test NewBloomFilter edge cases ----

func TestBloomFilter_DefaultParams(t *testing.T) {
	bf := NewBloomFilter(-1, 2.0) // invalid, should default
	if bf.ContainsString("anything") {
		t.Error("empty filter should not contain anything")
	}
}

// ---- Edge cases for strings in LRU cache ----

func TestLRUCache_LongKeys(t *testing.T) {
	c := NewLRUCache[int](10, 0)
	longKey := strings.Repeat("x", 1000)
	c.Set(longKey, 42)
	v, ok := c.Get(longKey)
	if !ok || v != 42 {
		t.Error("should handle long keys")
	}
}

// ---- SkipList edge: single element ----

func TestSkipList_SingleElement(t *testing.T) {
	sl := NewSkipList[int, string](IntLess)
	sl.Insert(42, "answer")
	k, v, ok := sl.Min()
	if !ok || k != 42 || v != "answer" {
		t.Error("min/max should work with single element")
	}
	k, v, ok = sl.Max()
	if !ok || k != 42 || v != "answer" {
		t.Error("min/max should work with single element")
	}
	sl.Delete(42)
	if sl.Len() != 0 {
		t.Error("should be empty after deleting only element")
	}
}
