// Package fluid - more extensions: Cuckoo Filter, Circular Hash Map,
// TimeSeries buffer, Moving Average calculator, Reservoir Sampling.
package fluid

import (
	"crypto/rand"
	"encoding/binary"
	"math"
	"sync"
	"time"
)

// ---- Cuckoo Filter ----

// CuckooFilter is a probabilistic set data structure supporting deletion.
type CuckooFilter struct {
	mu         sync.RWMutex
	buckets    [][]fingerprint
	bucketSize int
	numBuckets int
	count      int
	maxKicks   int
}

type fingerprint byte

// NewCuckooFilter creates a cuckoo filter.
func NewCuckooFilter(numBuckets, bucketSize int) *CuckooFilter {
	if numBuckets < 1 {
		numBuckets = 1024
	}
	if bucketSize < 1 {
		bucketSize = 4
	}
	buckets := make([][]fingerprint, numBuckets)
	for i := range buckets {
		buckets[i] = make([]fingerprint, 0, bucketSize)
	}
	return &CuckooFilter{
		buckets:    buckets,
		bucketSize: bucketSize,
		numBuckets: numBuckets,
		maxKicks:   500,
	}
}

// Insert adds an item to the filter.
func (cf *CuckooFilter) Insert(data []byte) bool {
	cf.mu.Lock()
	defer cf.mu.Unlock()

	fp := cf.fingerprint(data)
	i1 := cf.hash(data)
	i2 := i1 ^ cf.hashFingerprint(fp)

	// Try bucket 1
	if len(cf.buckets[i1]) < cf.bucketSize {
		cf.buckets[i1] = append(cf.buckets[i1], fp)
		cf.count++
		return true
	}

	// Try bucket 2
	if len(cf.buckets[i2]) < cf.bucketSize {
		cf.buckets[i2] = append(cf.buckets[i2], fp)
		cf.count++
		return true
	}

	// Eviction needed
	idx := i1
	for k := 0; k < cf.maxKicks; k++ {
		// Randomly evict an entry from bucket
		e := cf.rngInt() % len(cf.buckets[idx])
		fp, cf.buckets[idx][e] = cf.buckets[idx][e], fp

		// Place evicted fingerprint in alternate bucket
		idx = idx ^ cf.hashFingerprint(fp)
		if len(cf.buckets[idx]) < cf.bucketSize {
			cf.buckets[idx] = append(cf.buckets[idx], fp)
			cf.count++
			return true
		}
	}

	return false // filter is too full
}

// Contains checks if an item may be in the filter.
func (cf *CuckooFilter) Contains(data []byte) bool {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	fp := cf.fingerprint(data)
	i1 := cf.hash(data)
	i2 := i1 ^ cf.hashFingerprint(fp)

	for _, f := range cf.buckets[i1] {
		if f == fp {
			return true
		}
	}
	for _, f := range cf.buckets[i2] {
		if f == fp {
			return true
		}
	}
	return false
}

// Delete removes an item from the filter.
func (cf *CuckooFilter) Delete(data []byte) bool {
	cf.mu.Lock()
	defer cf.mu.Unlock()

	fp := cf.fingerprint(data)
	i1 := cf.hash(data)
	i2 := i1 ^ cf.hashFingerprint(fp)

	for i, f := range cf.buckets[i1] {
		if f == fp {
			cf.buckets[i1] = append(cf.buckets[i1][:i], cf.buckets[i1][i+1:]...)
			cf.count--
			return true
		}
	}
	for i, f := range cf.buckets[i2] {
		if f == fp {
			cf.buckets[i2] = append(cf.buckets[i2][:i], cf.buckets[i2][i+1:]...)
			cf.count--
			return true
		}
	}
	return false
}

func (cf *CuckooFilter) hash(data []byte) uint {
	h := uint(0)
	for _, b := range data {
		h = h*31 + uint(b)
	}
	return h % uint(cf.numBuckets)
}

func (cf *CuckooFilter) hashFingerprint(fp fingerprint) uint {
	return uint(fp) * 0x5bd1e995 % uint(cf.numBuckets)
}

func (cf *CuckooFilter) fingerprint(data []byte) fingerprint {
	var h byte
	for _, b := range data {
		h = h ^ b
		h = h*17 + 13
	}
	if h == 0 {
		h = 1
	}
	return fingerprint(h)
}

func (cf *CuckooFilter) rngInt() int {
	var b [8]byte
	rand.Read(b[:])
	return int(binary.BigEndian.Uint64(b[:]))
}

// ---- TimeSeries Buffer ----

// TimeSeriesPoint is a timestamped value.
type TimeSeriesPoint struct {
	Timestamp time.Time
	Value     float64
}

// TimeSeriesBuffer stores timestamped data points in a ring buffer.
type TimeSeriesBuffer struct {
	mu       sync.RWMutex
	points   []TimeSeriesPoint
	capacity int
	head     int
	size     int
}

// NewTimeSeriesBuffer creates a time series buffer.
func NewTimeSeriesBuffer(capacity int) *TimeSeriesBuffer {
	if capacity < 1 {
		capacity = 1024
	}
	return &TimeSeriesBuffer{
		points:   make([]TimeSeriesPoint, capacity),
		capacity: capacity,
	}
}

// Add appends a data point.
func (tsb *TimeSeriesBuffer) Add(value float64) {
	tsb.mu.Lock()
	defer tsb.mu.Unlock()

	tsb.points[tsb.head] = TimeSeriesPoint{Timestamp: time.Now(), Value: value}
	tsb.head = (tsb.head + 1) % tsb.capacity
	if tsb.size < tsb.capacity {
		tsb.size++
	}
}

// All returns all points in chronological order.
func (tsb *TimeSeriesBuffer) All() []TimeSeriesPoint {
	tsb.mu.RLock()
	defer tsb.mu.RUnlock()

	result := make([]TimeSeriesPoint, tsb.size)
	start := (tsb.head - tsb.size + tsb.capacity) % tsb.capacity
	for i := 0; i < tsb.size; i++ {
		result[i] = tsb.points[(start+i)%tsb.capacity]
	}
	return result
}

// Len returns the number of points stored.
func (tsb *TimeSeriesBuffer) Len() int {
	tsb.mu.RLock()
	defer tsb.mu.RUnlock()
	return tsb.size
}

// ---- Moving Average ----

// MovingAverage computes moving averages over time series data.
type MovingAverage struct {
	mu       sync.Mutex
	window   int
	values   []float64
	pos      int
	size     int
	sum      float64
}

// NewMovingAverage creates a simple moving average calculator.
func NewMovingAverage(window int) *MovingAverage {
	if window < 1 {
		window = 10
	}
	return &MovingAverage{
		window: window,
		values: make([]float64, window),
	}
}

// Add adds a value and returns the current average.
func (ma *MovingAverage) Add(value float64) float64 {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	if ma.size == ma.window {
		ma.sum -= ma.values[ma.pos]
	} else {
		ma.size++
	}

	ma.values[ma.pos] = value
	ma.sum += value
	ma.pos = (ma.pos + 1) % ma.window

	if ma.size == 0 {
		return 0
	}
	return ma.sum / float64(ma.size)
}

// Avg returns the current moving average.
func (ma *MovingAverage) Avg() float64 {
	ma.mu.Lock()
	defer ma.mu.Unlock()
	if ma.size == 0 {
		return 0
	}
	return ma.sum / float64(ma.size)
}

// ---- Exponential Moving Average ----

// EMovingAverage computes exponential moving average.
type EMovingAverage struct {
	mu     sync.Mutex
	alpha  float64
	value  float64
	inited bool
}

// NewEMovingAverage creates an EMA calculator.
func NewEMovingAverage(alpha float64) *EMovingAverage {
	if alpha <= 0 || alpha > 1 {
		alpha = 0.3
	}
	return &EMovingAverage{alpha: alpha}
}

// Add adds a value and returns the new EMA.
func (ema *EMovingAverage) Add(value float64) float64 {
	ema.mu.Lock()
	defer ema.mu.Unlock()

	if !ema.inited {
		ema.value = value
		ema.inited = true
	} else {
		ema.value = ema.alpha*value + (1-ema.alpha)*ema.value
	}
	return ema.value
}

// Value returns the current EMA.
func (ema *EMovingAverage) Value() float64 {
	ema.mu.Lock()
	defer ema.mu.Unlock()
	return ema.value
}

// ---- Reservoir Sampling ----

// ReservoirSample maintains a uniform random sample of a stream.
type ReservoirSample[T any] struct {
	mu     sync.Mutex
	res    []T
	size   int
	count  int
}

// NewReservoirSample creates a reservoir sampler.
func NewReservoirSample[T any](size int) *ReservoirSample[T] {
	return &ReservoirSample[T]{
		res:  make([]T, size),
		size: size,
	}
}

// Add processes a new item from the stream.
func (rs *ReservoirSample[T]) Add(item T) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	rs.count++
	if rs.count <= rs.size {
		rs.res[rs.count-1] = item
		return
	}

	// Replace with probability k/n
	j := rs.rngInt() % rs.count
	if j < rs.size {
		rs.res[j] = item
	}
}

// Sample returns the current sample.
func (rs *ReservoirSample[T]) Sample() []T {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	result := make([]T, rs.size)
	if rs.count < rs.size {
		copy(result, rs.res[:rs.count])
		return result[:rs.count]
	}
	copy(result, rs.res)
	return result
}

// Count returns the total number of items seen.
func (rs *ReservoirSample[T]) Count() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.count
}

func (rs *ReservoirSample[T]) rngInt() int {
	var b [8]byte
	rand.Read(b[:])
	v := int(binary.BigEndian.Uint64(b[:]))
	if v < 0 {
		v = -v
	}
	return v
}

// ---- Quantile Sketch ----

// QuantileSketch maintains approximate quantiles using a simple GK-like algorithm.
type QuantileSketch struct {
	mu       sync.Mutex
	values   []float64
	maxSize  int
	sorted   bool
}

// NewQuantileSketch creates a quantile sketch.
func NewQuantileSketch(maxSize int) *QuantileSketch {
	if maxSize < 10 {
		maxSize = 1000
	}
	return &QuantileSketch{
		values:  make([]float64, 0, maxSize),
		maxSize: maxSize,
	}
}

// Add inserts a value into the sketch.
func (qs *QuantileSketch) Add(v float64) {
	qs.mu.Lock()
	defer qs.mu.Unlock()

	if len(qs.values) >= qs.maxSize {
		// Reservoir-like replacement
		idx := qs.rngInt() % (len(qs.values) + 1)
		if idx < len(qs.values) {
			qs.values[idx] = v
		}
	} else {
		qs.values = append(qs.values, v)
	}
	qs.sorted = false
}

// Quantile returns the approximate value at the given quantile (0.0-1.0).
func (qs *QuantileSketch) Quantile(q float64) float64 {
	qs.mu.Lock()
	defer qs.mu.Unlock()

	if len(qs.values) == 0 {
		return 0
	}

	if !qs.sorted {
		sortFloat64s(qs.values)
		qs.sorted = true
	}

	if q <= 0 {
		return qs.values[0]
	}
	if q >= 1 {
		return qs.values[len(qs.values)-1]
	}

	pos := q * float64(len(qs.values)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return qs.values[lo]
	}
	frac := pos - float64(lo)
	return qs.values[lo]*(1-frac) + qs.values[hi]*frac
}

func sortFloat64s(a []float64) {
	// Simple insertion sort for small-ish arrays
	for i := 1; i < len(a); i++ {
		key := a[i]
		j := i - 1
		for j >= 0 && a[j] > key {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = key
	}
}

func (qs *QuantileSketch) rngInt() int {
	var b [8]byte
	rand.Read(b[:])
	v := int(binary.BigEndian.Uint64(b[:]))
	if v < 0 {
		v = -v
	}
	return v
}
