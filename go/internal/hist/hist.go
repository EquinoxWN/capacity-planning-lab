// Package hist is a fixed-size latency histogram with a bounded relative error.
//
// Buckets grow by 2^(1/8) (about 9%), from 1 µs up to about 64 s, so a quantile read
// from the histogram is never below the true value and at most 9.06% above it.
package hist

import (
	"math"
	"time"
)

const (
	// perDoubling is the number of buckets per power of two.
	perDoubling = 8
	// doublings covers 1 µs * 2^26, about 67 s.
	doublings = 26
	// NumBuckets is the bucket count, including the overflow bucket.
	NumBuckets = doublings*perDoubling + 2
	// minValue is the upper bound of bucket 0.
	minValue = time.Microsecond
)

// RelativeError is the largest relative overestimate of a quantile below the overflow bucket.
var RelativeError = math.Pow(2, 1.0/perDoubling) - 1

// Histogram counts durations in log-spaced buckets; the zero value is ready to use.
type Histogram struct {
	counts [NumBuckets]uint64
	total  uint64
	sum    time.Duration
	max    time.Duration
}

// upper returns the inclusive upper bound of bucket i.
func upper(i int) time.Duration {
	if i >= NumBuckets-1 {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(float64(minValue) * math.Pow(2, float64(i)/perDoubling))
}

// index returns the bucket for d.
func index(d time.Duration) int {
	if d <= minValue {
		return 0
	}
	i := int(math.Ceil(perDoubling * math.Log2(float64(d)/float64(minValue))))
	if i >= NumBuckets-1 {
		return NumBuckets - 1
	}
	// Correct floating-point rounding at bucket edges.
	for i < NumBuckets-1 && d > upper(i) {
		i++
	}
	for i > 0 && d <= upper(i-1) {
		i--
	}
	return i
}

// Record adds one observation; negative durations count as zero.
func (h *Histogram) Record(d time.Duration) {
	if d < 0 {
		d = 0
	}
	h.counts[index(d)]++
	h.total++
	h.sum += d
	if d > h.max {
		h.max = d
	}
}

// Merge adds every observation of o into h.
func (h *Histogram) Merge(o *Histogram) {
	for i, c := range o.counts {
		h.counts[i] += c
	}
	h.total += o.total
	h.sum += o.sum
	if o.max > h.max {
		h.max = o.max
	}
}

// Count returns the number of observations.
func (h *Histogram) Count() uint64 { return h.total }

// Max returns the largest observation.
func (h *Histogram) Max() time.Duration { return h.max }

// Mean returns the arithmetic mean, or zero when empty.
func (h *Histogram) Mean() time.Duration {
	if h.total == 0 {
		return 0
	}
	return h.sum / time.Duration(h.total)
}

// Quantile returns the nearest-rank q-quantile, rounded up to its bucket bound and capped at Max.
func (h *Histogram) Quantile(q float64) time.Duration {
	if h.total == 0 {
		return 0
	}
	q = min(max(q, 0), 1)
	rank := uint64(math.Ceil(q * float64(h.total)))
	if rank == 0 {
		rank = 1
	}
	var seen uint64
	for i, c := range h.counts {
		seen += c
		if seen >= rank {
			return min(upper(i), h.max)
		}
	}
	return h.max
}
