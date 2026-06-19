package main

import "testing"

// Cache-hit input tokens are ~10x cheaper on DeepSeek, so a mostly-cached call
// must cost much less than billing the whole prompt at the miss rate. The footer
// previously overstated cost on cached runs (often >90% hits).
func TestDeepseekCost_CacheHitsAreCheaper(t *testing.T) {
	cached := deepseekCost(10000, 10000, 100) // all input cache hits
	uncached := deepseekCost(10000, 0, 100)   // no cache hits
	if !(cached < uncached) {
		t.Errorf("cache hits must cost less: cached=%v uncached=%v", cached, uncached)
	}
	// the all-miss flat cost is the upper bound; a fully-cached call is well under it
	flat := float64(10000)*0.14/1e6 + float64(100)*0.28/1e6
	if cached >= flat {
		t.Errorf("a fully-cached call (%v) should be below the all-miss cost (%v)", cached, flat)
	}
}

// With zero cache hits the result equals the plain miss+output computation.
func TestDeepseekCost_NoCacheMatchesFlat(t *testing.T) {
	got := deepseekCost(1000, 0, 50)
	want := float64(1000)*0.14/1e6 + float64(50)*0.28/1e6
	if got != want {
		t.Errorf("no-cache cost = %v, want %v", got, want)
	}
}
