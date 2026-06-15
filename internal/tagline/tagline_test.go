package tagline

import (
	"testing"
)

func TestNewExtractor(t *testing.T) {
	e := NewExtractor()
	if e == nil {
		t.Fatal("NewExtractor returned nil")
	}
}

func TestExtractKeywords_Basic(t *testing.T) {
	e := NewExtractor()
	text := "The quick brown fox jumps over the lazy dog"
	tags := e.ExtractKeywords(text)

	// Should find keywords (non-stopwords, >=3 chars)
	if len(tags) == 0 {
		t.Error("expected some keywords")
	}
	// "quick", "brown", "fox", "jumps", "over", "lazy", "dog"
	if len(tags) < 5 {
		t.Errorf("expected at least 5 keywords, got %d: %v", len(tags), tags)
	}
}

func TestExtractKeywords_StopWordsFiltered(t *testing.T) {
	e := NewExtractor()
	text := "the is at which on a an and or but in with to for of"
	tags := e.ExtractKeywords(text)
	// All stop words, no keywords
	if len(tags) != 0 {
		t.Errorf("expected 0 keywords from stop words only, got %d: %v", len(tags), tags)
	}
}

func TestExtractKeywords_ShortWords(t *testing.T) {
	e := NewExtractor()
	e.SetMinWordLen(5)
	text := "the cat sat on the large elephant"
	tags := e.ExtractKeywords(text)
	// Only "large" and "elephant" should appear
	for _, tag := range tags {
		if len(tag.Name) < 5 {
			t.Errorf("word '%s' is too short, should be filtered", tag.Name)
		}
	}
}

func TestExtractKeywords_MaxKeywords(t *testing.T) {
	e := NewExtractor()
	e.SetMaxKeywords(3)
	text := "apple banana cherry date elderberry fig grape honeydew"
	tags := e.ExtractKeywords(text)
	if len(tags) > 3 {
		t.Errorf("expected at most 3 keywords, got %d", len(tags))
	}
}

func TestExtractKeywords_NumericFiltering(t *testing.T) {
	e := NewExtractor()
	text := "version 123 4567 api gateway"
	tags := e.ExtractKeywords(text)
	for _, tag := range tags {
		if isNumeric(tag.Name) {
			t.Errorf("numeric word '%s' should be filtered", tag.Name)
		}
	}
}

func TestExtractEntities_URL(t *testing.T) {
	e := NewExtractor()
	text := "Visit https://example.com/page for more info"
	entities := e.ExtractEntities(text)

	found := false
	for _, ent := range entities {
		if ent.Type == "url" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find URL entity")
	}
}

func TestExtractEntities_Email(t *testing.T) {
	e := NewExtractor()
	text := "Contact us at hello@example.com for support"
	entities := e.ExtractEntities(text)

	found := false
	for _, ent := range entities {
		if ent.Type == "email" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find email entity")
	}
}

func TestExtractEntities_Hashtag(t *testing.T) {
	e := NewExtractor()
	text := "Loving the #golang #coding life"
	entities := e.ExtractEntities(text)

	count := 0
	for _, ent := range entities {
		if ent.Type == "hashtag" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 hashtags, got %d", count)
	}
}

func TestExtractEntities_Date(t *testing.T) {
	e := NewExtractor()
	text := "The event is on 2024-12-25 in the evening"
	entities := e.ExtractEntities(text)

	found := false
	for _, ent := range entities {
		if ent.Type == "date" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find date entity")
	}
}

func TestExtractEntities_CustomPattern(t *testing.T) {
	e := NewExtractor()
	err := e.AddEntityPattern(`\b[A-Z]{3}-\d{3}\b`) // e.g., ABC-123
	if err != nil {
		t.Fatalf("AddEntityPattern error: %v", err)
	}

	text := "The code is XYZ-789 for this project"
	entities := e.ExtractEntities(text)

	found := false
	for _, ent := range entities {
		if ent.Type == "custom" && ent.Name == "XYZ-789" {
			found = true
		}
	}
	if !found {
		t.Error("expected to find custom entity")
	}
}

func TestAddEntityPattern_Invalid(t *testing.T) {
	e := NewExtractor()
	err := e.AddEntityPattern(`[invalid`)
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestRankTags(t *testing.T) {
	tags := []Tag{
		{Name: "common", Weight: 0.1, Count: 10},
		{Name: "rare", Weight: 0.9, Count: 1},
	}
	ranked := RankTags(tags)
	// After ranking: rare weight = 0.9 * log2(2) = 0.9, common = 0.1 * log2(11) ≈ 0.346
	if ranked[0].Name != "rare" {
		t.Errorf("expected 'rare' first, got '%s'", ranked[0].Name)
	}
}

func TestNormalizeWeights(t *testing.T) {
	tags := []Tag{
		{Name: "a", Weight: 0.3},
		{Name: "b", Weight: 0.7},
	}
	NormalizeWeights(tags)
	sum := tags[0].Weight + tags[1].Weight
	if sum < 0.99 || sum > 1.01 {
		t.Errorf("weights should sum to ~1.0, got %.4f", sum)
	}
}

func TestGenerateCloud(t *testing.T) {
	tags := []Tag{
		{Name: "hot", Weight: 0.8},
		{Name: "warm", Weight: 0.5},
		{Name: "cold", Weight: 0.2},
	}
	cloud := GenerateCloud(tags)
	if cloud.MaxWeight != 0.8 {
		t.Errorf("expected max 0.8, got %f", cloud.MaxWeight)
	}
	if cloud.MinWeight != 0.2 {
		t.Errorf("expected min 0.2, got %f", cloud.MinWeight)
	}
}

func TestTagCloud_FontSize(t *testing.T) {
	cloud := GenerateCloud([]Tag{
		{Name: "a", Weight: 0.0},
		{Name: "b", Weight: 1.0},
	})
	if cloud.FontSize(cloud.Tags[0]) != 1 {
		t.Errorf("expected font size 1 for min weight, got %d", cloud.FontSize(cloud.Tags[0]))
	}
	if cloud.FontSize(cloud.Tags[1]) != 5 {
		t.Errorf("expected font size 5 for max weight, got %d", cloud.FontSize(cloud.Tags[1]))
	}
}

func TestTagCloud_TopN(t *testing.T) {
	tags := []Tag{
		{Name: "a", Weight: 0.9},
		{Name: "b", Weight: 0.8},
		{Name: "c", Weight: 0.7},
	}
	cloud := GenerateCloud(tags)
	top := cloud.TopN(2)
	if len(top) != 2 || top[0].Name != "a" || top[1].Name != "b" {
		t.Errorf("unexpected top 2: %v", top)
	}
}

func TestCoOccurrenceMatrix_AddGet(t *testing.T) {
	cm := NewCoOccurrenceMatrix()
	cm.Add([]string{"go", "programming"})
	cm.Add([]string{"go", "programming"})
	cm.Add([]string{"go", "concurrency"})

	count := cm.Get("go", "programming")
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
	count = cm.Get("go", "concurrency")
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}
}

func TestCoOccurrenceMatrix_Related(t *testing.T) {
	cm := NewCoOccurrenceMatrix()
	cm.Add([]string{"go", "programming"})
	cm.Add([]string{"go", "programming"})
	cm.Add([]string{"go", "concurrency"})
	cm.Add([]string{"go", "testing"})

	related := cm.Related("go", 2)
	if len(related) != 2 {
		t.Errorf("expected 2 related, got %d", len(related))
	}
	if related[0].Name != "programming" {
		t.Errorf("expected 'programming' most related, got '%s'", related[0].Name)
	}
}

func TestCoOccurrenceMatrix_AllTags(t *testing.T) {
	cm := NewCoOccurrenceMatrix()
	cm.Add([]string{"a", "b"})
	cm.Add([]string{"c", "d"})

	tags := cm.AllTags()
	if len(tags) != 4 {
		t.Errorf("expected 4 tags, got %d", len(tags))
	}
}

func TestCoOccurrenceMatrix_Matrix(t *testing.T) {
	cm := NewCoOccurrenceMatrix()
	cm.Add([]string{"x", "y"})

	matrix := cm.Matrix()
	if v := matrix["x"]["y"]; v != 1 {
		t.Errorf("expected 1, got %d", v)
	}
}

func TestExtractPhrases(t *testing.T) {
	text := "the quick brown fox jumps over the quick brown dog"
	phrases := ExtractPhrases(text, 2)

	// "quick brown" should appear twice
	found := false
	for _, p := range phrases {
		if p.Name == "quick brown" && p.Count >= 2 {
			found = true
		}
	}
	if !found {
		t.Error("expected 'quick brown' bigram with count >= 2")
	}
}

func TestScoreSentences(t *testing.T) {
	sentences := []string{
		"golang is great for concurrency",
		"the weather is nice today",
	}

	keywords := []Tag{
		{Name: "golang", Weight: 0.9},
		{Name: "concurrency", Weight: 0.8},
	}

	scored := ScoreSentences(sentences, keywords)
	if scored[0].Index != 0 {
		t.Errorf("expected sentence 0 highest scored, got index %d", scored[0].Index)
	}
}

func TestDeduplicateTags(t *testing.T) {
	tags := []Tag{
		{Name: "golang"},
		{Name: "gollang"}, // typo
		{Name: "python"},
	}
	deduped := DeduplicateTags(tags, 0.7)
	if len(deduped) < 2 {
		t.Errorf("expected at least 2 after dedup, got %d", len(deduped))
	}
}

func TestProcessBatch(t *testing.T) {
	e := NewExtractor()
	texts := []string{
		"hello world from golang",
		"python is great for data science",
	}
	result := e.ProcessBatch(texts)
	if result.Documents != 2 {
		t.Errorf("expected 2 documents, got %d", result.Documents)
	}
	if len(result.Tags[0]) == 0 {
		t.Error("expected keywords from first document")
	}
}

func TestSimilarity(t *testing.T) {
	if similarity("hello", "hello") != 1.0 {
		t.Error("identical strings should have similarity 1")
	}
	if similarity("hello", "world") >= 0.8 {
		t.Error("different strings should have low similarity")
	}
}

func TestExtractKeywordsTFIDF(t *testing.T) {
	docs := []string{
		"golang is a programming language for systems",
		"python is a programming language for data science",
		"golang and python are both great languages",
	}
	results := ExtractKeywordsTFIDF(docs, 5)
	if len(results) != 3 {
		t.Fatalf("expected 3 result sets, got %d", len(results))
	}
	if len(results[0]) == 0 {
		t.Error("first doc should have keywords")
	}
}

func TestExtractKeywordsTFIDF_EmptyDocs(t *testing.T) {
	results := ExtractKeywordsTFIDF(nil, 5)
	if results != nil {
		t.Error("expected nil for empty docs")
	}
}

func TestAddStopWord(t *testing.T) {
	e := NewExtractor()
	e.AddStopWord("specialword")

	text := "this specialword should be filtered"
	tags := e.ExtractKeywords(text)

	for _, tag := range tags {
		if tag.Name == "specialword" {
			t.Error("stop word should be filtered")
		}
	}
}

func TestSetStopWords(t *testing.T) {
	e := NewExtractor()
	e.SetStopWords([]string{"custom1", "custom2"})

	text := "custom1 custom2 hello"
	tags := e.ExtractKeywords(text)

	// Only "hello" should remain
	if len(tags) != 1 || tags[0].Name != "hello" {
		t.Errorf("expected only 'hello', got %v", tags)
	}
}

func TestTokenize(t *testing.T) {
	words := tokenize("Hello, world! How's it going?")
	expected := []string{"Hello", "world", "How's", "it", "going"}
	if len(words) != len(expected) {
		t.Errorf("expected %d words, got %d: %v", len(expected), len(words), words)
	}
}

func TestIsNumeric(t *testing.T) {
	if !isNumeric("12345") {
		t.Error("'12345' should be numeric")
	}
	if isNumeric("abc123") {
		t.Error("'abc123' should not be numeric")
	}
	if !isNumeric("3.14") {
		t.Error("'3.14' should be numeric")
	}
}

func TestTagCloud_EmptyCloud(t *testing.T) {
	cloud := GenerateCloud(nil)
	if cloud.Tags != nil {
		t.Error("expected nil tags for empty cloud")
	}
}

func TestFontSize_EqualWeights(t *testing.T) {
	cloud := GenerateCloud([]Tag{
		{Name: "a", Weight: 0.5},
		{Name: "b", Weight: 0.5},
	})
	if cloud.FontSize(cloud.Tags[0]) != 3 {
		t.Errorf("expected font size 3 for equal weights, got %d", cloud.FontSize(cloud.Tags[0]))
	}
}
