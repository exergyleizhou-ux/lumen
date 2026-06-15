package embeddings

import (
	"testing"
)

func TestChunk(t *testing.T) {
	text := "one two three four five six seven eight nine ten"
	chunks := Chunk(text, 3, 1)
	if len(chunks) < 3 {
		t.Errorf("expected at least 3 chunks, got %d", len(chunks))
	}
}

func TestChunkShort(t *testing.T) {
	chunks := Chunk("short text", 100, 0)
	if len(chunks) != 1 {
		t.Errorf("short text should be 1 chunk, got %d", len(chunks))
	}
}

func TestCosineSimilarity(t *testing.T) {
	a := Vector{1, 0, 0}
	b := Vector{1, 0, 0}
	if CosineSimilarity(a, b) != 1.0 {
		t.Errorf("identical vectors: got %f", CosineSimilarity(a, b))
	}

	c := Vector{0, 1, 0}
	if CosineSimilarity(a, c) != 0.0 {
		t.Errorf("orthogonal: got %f", CosineSimilarity(a, c))
	}
}

func TestCosineDifferentLengths(t *testing.T) {
	a := Vector{1, 0}
	b := Vector{1, 0, 0}
	if CosineSimilarity(a, b) != 0 {
		t.Error("different lengths should return 0")
	}
}

func TestTopK(t *testing.T) {
	query := Vector{1, 0, 0}
	candidates := []Vector{
		{1, 0, 0}, // most similar
		{0, 1, 0},
		{0.5, 0.5, 0},
	}
	top := TopK(query, candidates, 2)
	if len(top) != 2 {
		t.Fatalf("expected 2, got %d", len(top))
	}
	if top[0] != 0 {
		t.Errorf("first should be index 0, got %d", top[0])
	}
}
