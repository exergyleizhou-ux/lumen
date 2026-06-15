package tagline

import (
	"testing"
)

func TestClassifier_TrainClassify(t *testing.T) {
	c := NewClassifier()
	c.Train("golang is great for concurrency", "tech")
	c.Train("python is good for data science", "tech")
	c.Train("the movie was fantastic and beautiful", "arts")
	c.Train("this painting is wonderful and amazing", "arts")

	class, confidence := c.Classify("golang concurrency is great")
	if class != "tech" {
		t.Errorf("expected 'tech', got '%s'", class)
	}
	if confidence <= 0 {
		t.Error("confidence should be positive")
	}
}

func TestClassifier_Empty(t *testing.T) {
	c := NewClassifier()
	class, conf := c.Classify("anything")
	if class != "" || conf != 0 {
		t.Errorf("empty classifier: expected ('', 0), got ('%s', %f)", class, conf)
	}
}

func TestClassifier_Probabilities(t *testing.T) {
	c := NewClassifier()
	c.Train("golang is great", "tech")
	c.Train("beautiful art", "arts")

	probs := c.ClassProbabilities("golang and art")
	if probs == nil {
		t.Fatal("should have probabilities")
	}
	if len(probs) != 2 {
		t.Errorf("expected 2 classes, got %d", len(probs))
	}
	// Probabilities should sum to approximately 1
	var sum float64
	for _, p := range probs {
		sum += p
	}
	if sum < 0.99 || sum > 1.01 {
		t.Errorf("probabilities should sum to ~1, got %f", sum)
	}
}

func TestAnalyzeSentiment_Positive(t *testing.T) {
	result := AnalyzeSentiment("this is great and wonderful and fantastic")
	if result.Label != "positive" {
		t.Errorf("expected 'positive', got '%s' (score: %f)", result.Label, result.Score)
	}
	if result.Score <= 0 {
		t.Error("score should be positive")
	}
}

func TestAnalyzeSentiment_Negative(t *testing.T) {
	result := AnalyzeSentiment("this is terrible and horrible and awful")
	if result.Label != "negative" {
		t.Errorf("expected 'negative', got '%s' (score: %f)", result.Label, result.Score)
	}
	if result.Score >= 0 {
		t.Error("score should be negative")
	}
}

func TestAnalyzeSentiment_Neutral(t *testing.T) {
	result := AnalyzeSentiment("the quick brown fox")
	if result.Label != "neutral" {
		t.Errorf("expected 'neutral', got '%s'", result.Label)
	}
}

func TestExpandKeywords(t *testing.T) {
	tags := []Tag{
		{Name: "golang", Weight: 1.0, Count: 5},
	}
	thesaurus := map[string][]string{
		"golang": {"go", "go-lang"},
	}
	expanded := ExpandKeywords(tags, thesaurus)
	if len(expanded) < 2 {
		t.Errorf("expected at least 2 tags, got %d", len(expanded))
	}
}

func TestDetectLanguage(t *testing.T) {
	lang, _ := DetectLanguage("the quick brown fox jumps over the lazy dog")
	t.Logf("Detected: %s", lang)
	// Should likely detect English
}

func TestNgrams(t *testing.T) {
	ngrams := Ngrams("hello", 2)
	if len(ngrams) != 4 {
		t.Errorf("expected 4 bigrams for 'hello', got %d: %v", len(ngrams), ngrams)
	}
}

func TestNgrams_TooShort(t *testing.T) {
	ngrams := Ngrams("hi", 3)
	if len(ngrams) != 0 {
		t.Errorf("expected 0 ngrams for short text, got %d", len(ngrams))
	}
}

func TestNgrams_InvalidN(t *testing.T) {
	ngrams := Ngrams("hello", 0)
	if ngrams != nil {
		t.Error("expected nil for n=0")
	}
}

func TestWordNgrams(t *testing.T) {
	ngrams := WordNgrams("the quick brown fox", 2)
	if len(ngrams) != 3 {
		t.Errorf("expected 3 bigrams, got %d: %v", len(ngrams), ngrams)
	}
}

func TestWordNgrams_TooShort(t *testing.T) {
	ngrams := WordNgrams("hello", 2)
	if len(ngrams) != 0 {
		t.Error("expected 0 for single word with n=2")
	}
}

func TestClusterTags(t *testing.T) {
	cm := NewCoOccurrenceMatrix()
	cm.Add([]string{"go", "golang", "concurrency"})
	cm.Add([]string{"python", "data", "science"})
	cm.Add([]string{"go", "golang"})
	cm.Add([]string{"python", "data"})

	clusters := ClusterTags(cm, 1)
	if len(clusters) < 1 {
		t.Error("expected at least 1 cluster")
	}
}
