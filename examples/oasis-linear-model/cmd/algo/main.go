// Example C2D algorithm: linear-model trainer.
// Reads a CSV dataset, trains a simple linear model using SGD, outputs predictions.
// This is the reference implementation for the Oasis C2D author toolchain (P4).
package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"strconv"
)

func main() {
	var params struct {
		DatasetPath string                 `json:"dataset_path"`
		Params      map[string]interface{} `json:"params"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&params); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	// Load hyperparams
	nEstimators := 100
	learningRate := 0.1
	if v, ok := params.Params["n_estimators"].(float64); ok && v > 0 {
		nEstimators = int(v)
	}
	if v, ok := params.Params["learning_rate"].(float64); ok && v > 0 {
		learningRate = v
	}

	// Load dataset
	X, y, err := loadCSV(params.DatasetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: load dataset: %v\n", err)
		os.Exit(1)
	}
	if len(X) == 0 || len(X[0]) == 0 {
		fmt.Fprintf(os.Stderr, "ERROR: empty dataset\n")
		os.Exit(1)
	}

	// Train linear model (SGD)
	nFeatures := len(X[0])
	weights := make([]float64, nFeatures)
	rng := rand.New(rand.NewSource(42))

	for epoch := 0; epoch < nEstimators; epoch++ {
		idx := rng.Intn(len(X))
		pred := dot(weights, X[idx])
		error := pred - y[idx]
		for j := 0; j < nFeatures; j++ {
			weights[j] -= learningRate * error * X[idx][j]
		}
	}

	// Make predictions on training data
	predictions := make([]float64, len(X))
	totalError := 0.0
	for i := range X {
		predictions[i] = dot(weights, X[i])
		totalError += math.Abs(predictions[i] - y[i])
	}
	mae := totalError / float64(len(X))

	// Output result
	output := map[string]interface{}{
		"status":      "ok",
		"algorithm":   "linear-model",
		"weights":     weights,
		"n_features":  nFeatures,
		"n_samples":   len(X),
		"mae":         mae,
		"predictions": predictions[:min(10, len(predictions))],
		"predicted":   len(predictions),
	}
	json.NewEncoder(os.Stdout).Encode(output)
}

func loadCSV(path string) ([][]float64, []float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, nil, err
	}
	_ = header

	var X [][]float64
	var y []float64
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		if len(row) < 2 {
			continue
		}
		features := make([]float64, len(row)-1)
		for i := 0; i < len(row)-1; i++ {
			v, _ := strconv.ParseFloat(row[i], 64)
			features[i] = v
		}
		target, _ := strconv.ParseFloat(row[len(row)-1], 64)
		X = append(X, features)
		y = append(y, target)
	}
	return X, y, nil
}

func dot(a, b []float64) float64 {
	sum := 0.0
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
