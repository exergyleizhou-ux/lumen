// R11: Regression fixture accumulation for C2D algorithm runs.
// Each successful C2D execution becomes a permanent regression test.
package editverify

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// RegressionFixture records one successful C2D run for regression testing.
type RegressionFixture struct {
	AlgoName    string `json:"algo_name"`
	AlgoDigest  string `json:"algo_digest"`
	DatasetID   string `json:"dataset_id"`
	InputHash   string `json:"input_hash"`
	OutputHash  string `json:"output_hash"`
	Signature   string `json:"signature"` // Ed25519 hex
	SavedAt     string `json:"saved_at"`
}

// RegressionStore persists regression fixtures.
type RegressionStore struct {
	dir string
}

// NewRegressionStore creates a store under the given directory.
func NewRegressionStore(dir string) *RegressionStore {
	os.MkdirAll(dir, 0755)
	return &RegressionStore{dir: dir}
}

// Save persists a fixture as JSON.
func (s *RegressionStore) Save(fixture RegressionFixture) error {
	path := filepath.Join(s.dir, fixture.AlgoName+".json")
	data, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Load returns all fixtures for an algorithm, sorted by saved time.
func (s *RegressionStore) Load(algoName string) ([]RegressionFixture, error) {
	path := filepath.Join(s.dir, algoName+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fixtures []RegressionFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		return nil, fmt.Errorf("parse fixtures: %w", err)
	}
	sort.Slice(fixtures, func(i, j int) bool {
		return fixtures[i].SavedAt < fixtures[j].SavedAt
	})
	return fixtures, nil
}

// Verify checks that an output hash matches the stored fixture for the given input.
func (s *RegressionStore) Verify(algoName, inputHash, outputHash string) (bool, error) {
	fixtures, err := s.Load(algoName)
	if err != nil {
		return false, err
	}
	for _, f := range fixtures {
		if f.InputHash == inputHash {
			return f.OutputHash == outputHash, nil
		}
	}
	return false, fmt.Errorf("no fixture found for input hash %s", inputHash[:12])
}

// ListAlgorithms returns the names of algorithms that have regression fixtures.
func (s *RegressionStore) ListAlgorithms() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name()[:len(e.Name())-5])
		}
	}
	sort.Strings(names)
	return names, nil
}

// HashFixture computes the input hash for a regression fixture from its raw data.
func HashFixture(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:])
}
