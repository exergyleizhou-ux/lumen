package main

import (
	"testing"

	"lumen/internal/event"
	"lumen/internal/provider"
)

// With a configured provider pricing block, cost uses those rates (not the
// hardcoded DeepSeek default) so non-DeepSeek users see accurate spend.
func TestUsageCostUsesConfiguredPricing(t *testing.T) {
	p := &provider.Pricing{Input: 1.0, Output: 2.0, CacheHit: 0.5} // per 1M tokens
	u := &event.Usage{CacheMissTokens: 1_000_000, CacheHitTokens: 1_000_000, CompletionTokens: 1_000_000}
	// (1M*0.5 + 1M*1.0 + 1M*2.0) / 1e6 = 3.5
	if got := usageCost(p, u); got != 3.5 {
		t.Errorf("configured pricing: got %v want 3.5", got)
	}
}

// With no pricing configured, fall back to the built-in DeepSeek rate (current
// behavior — the default DeepSeek user is unaffected).
func TestUsageCostFallsBackToDefault(t *testing.T) {
	u := &event.Usage{PromptTokens: 1_000_000, CacheHitTokens: 0, CompletionTokens: 0}
	// deepseekCost(1M, 0, 0) = 1M * 0.14/1e6 = 0.14
	if got := usageCost(nil, u); got != 0.14 {
		t.Errorf("default fallback: got %v want 0.14", got)
	}
}

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
