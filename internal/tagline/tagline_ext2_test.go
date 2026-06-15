package tagline

import (
	"testing"
)

func TestSegment(t *testing.T) {
	text := "Hello world. This is a test! Is it working?"
	sentences := Segment(text)
	if len(sentences) < 2 {
		t.Errorf("expected at least 2 sentences, got %d: %v", len(sentences), sentences)
	}
}

func TestSegmentParagraphs(t *testing.T) {
	text := "Para one.\n\nPara two.\n\nPara three."
	paras := SegmentParagraphs(text)
	if len(paras) != 3 {
		t.Errorf("expected 3 paragraphs, got %d", len(paras))
	}
}

func TestComputeReadability(t *testing.T) {
	text := "The cat sat on the mat. The dog ran in the park. Birds fly in the sky."
	score := ComputeReadability(text)
	if score.Words == 0 {
		t.Error("should count words")
	}
	if score.Sentences == 0 {
		t.Error("should count sentences")
	}
	t.Logf("Readability: ease=%.2f grade=%.2f level=%s", score.FleschEase, score.FleschKincaid, score.Level)
}

func TestComputeReadability_Empty(t *testing.T) {
	score := ComputeReadability("")
	if score.Level != "unknown" {
		t.Errorf("expected 'unknown', got '%s'", score.Level)
	}
}

func TestTFIDFVectorizer(t *testing.T) {
	v := NewTFIDFVectorizer()
	docs := []string{
		"golang is great for systems programming",
		"python is great for data science",
		"golang and python are both programming languages",
	}
	vecs := v.FitTransform(docs)
	if len(vecs) != 3 {
		t.Fatalf("expected 3 vectors, got %d", len(vecs))
	}
	if v.VocabularySize() == 0 {
		t.Error("vocabulary should not be empty")
	}
}

func TestCosineSimilarity(t *testing.T) {
	a := map[int]float64{0: 1.0, 1: 2.0}
	b := map[int]float64{0: 1.0, 1: 2.0}
	sim := CosineSimilarity(a, b)
	if sim < 0.99 || sim > 1.01 {
		t.Errorf("identical vectors should have similarity ~1.0, got %f", sim)
	}

	c := map[int]float64{2: 1.0, 3: 2.0}
	sim2 := CosineSimilarity(a, c)
	if sim2 > 0.01 {
		t.Errorf("disjoint vectors should have similarity ~0, got %f", sim2)
	}
}

func TestTextNormalizer(t *testing.T) {
	tn := NewTextNormalizer()
	result := tn.Normalize("  Héllo   Wörld!  ")
	if result != "hello world!" {
		t.Errorf("unexpected normalized text: %q", result)
	}
}

func TestLevenshtein(t *testing.T) {
	if Levenshtein("kitten", "sitting") != 3 {
		t.Errorf("expected 3, got %d", Levenshtein("kitten", "sitting"))
	}
	if Levenshtein("", "abc") != 3 {
		t.Errorf("expected 3, got %d", Levenshtein("", "abc"))
	}
	if Levenshtein("abc", "abc") != 0 {
		t.Errorf("expected 0 for identical, got %d", Levenshtein("abc", "abc"))
	}
}

func TestJaccardSimilarity(t *testing.T) {
	sim := JaccardSimilarity("hello world", "hello world")
	if sim != 1.0 {
		t.Errorf("expected 1.0, got %f", sim)
	}

	sim = JaccardSimilarity("hello", "world")
	if sim > 0.01 {
		t.Errorf("expected ~0, got %f", sim)
	}

	sim = JaccardSimilarity("hello world", "hello golang")
	if sim < 0.3 || sim > 0.7 {
		t.Errorf("expected ~0.5, got %f", sim)
	}
}

func TestTopNgrams(t *testing.T) {
	// TopNgrams uses character-level ngrams
	ngrams := TopNgrams("hello hello world", 2, 2)
	if len(ngrams) != 2 {
		t.Errorf("expected 2 ngrams, got %d", len(ngrams))
	}
	if ngrams[0].Count < ngrams[1].Count {
		t.Errorf("first ngram should have highest count")
	}
}

func TestNgramFreq(t *testing.T) {
	freq := NgramFreq("ababab", 2)
	if freq["ab"] != 3 || freq["ba"] != 2 {
		t.Errorf("unexpected ngram freq: %v", freq)
	}
}

func TestRemoveAccents(t *testing.T) {
	s := removeAccents("Café naïve résumé")
	if s != "Cafe naive resume" {
		t.Errorf("unexpected: %q", s)
	}
}

func TestRemovePunctuation(t *testing.T) {
	s := removePunctuation("Hello, world! How's it going?")
	if s != "Hello  world  How s it going " {
		t.Errorf("unexpected: %q", s)
	}
}

func TestCollapseWhitespace(t *testing.T) {
	s := collapseWhitespace("  hello   world  ")
	if s != "hello world" {
		t.Errorf("unexpected: %q", s)
	}
}
