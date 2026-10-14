package hist

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

// exactQuantile is the nearest-rank quantile of sorted values.
func exactQuantile(sorted []time.Duration, q float64) time.Duration {
	rank := int(math.Ceil(q * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	return sorted[rank-1]
}

func TestQuantileErrorIsBounded(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for trial := range 200 {
		n := 1 + r.IntN(2000)
		var h Histogram
		vals := make([]time.Duration, n)
		for i := range vals {
			// Log-uniform between 1 µs and 10 s, the range latency lives in.
			vals[i] = time.Duration(math.Exp(r.Float64()*math.Log(1e7)) * float64(time.Microsecond))
			h.Record(vals[i])
		}
		slices.Sort(vals)
		for _, q := range []float64{0.01, 0.5, 0.9, 0.95, 0.99, 0.999, 1} {
			want, got := exactQuantile(vals, q), h.Quantile(q)
			if got < want {
				t.Fatalf("trial %d q=%v: estimate %v below true %v", trial, q, got, want)
			}
			if float64(got) > float64(want)*(1+RelativeError)+1 {
				t.Fatalf("trial %d q=%v: estimate %v more than %.2f%% above true %v", trial, q, got, RelativeError*100, want)
			}
		}
		if h.Count() != uint64(n) || h.Max() != vals[n-1] {
			t.Fatalf("count %d max %v, want %d %v", h.Count(), h.Max(), n, vals[n-1])
		}
	}
}

func TestBucketEdges(t *testing.T) {
	for i := range NumBuckets - 1 {
		u := upper(i)
		if got := index(u); got != i {
			t.Fatalf("upper(%d)=%v maps to bucket %d", i, u, got)
		}
		if got := index(u + 1); got != i+1 && i+1 < NumBuckets {
			t.Fatalf("upper(%d)+1ns maps to bucket %d, want %d", i, got, i+1)
		}
	}
	if index(time.Hour) != NumBuckets-1 {
		t.Fatal("an hour must land in the overflow bucket")
	}
}

func TestMergeEqualsRecordingEverything(t *testing.T) {
	var a, b, all Histogram
	for i := 1; i <= 1000; i++ {
		d := time.Duration(i) * 37 * time.Microsecond
		all.Record(d)
		if i%3 == 0 {
			a.Record(d)
		} else {
			b.Record(d)
		}
	}
	a.Merge(&b)
	if a != all {
		t.Fatal("merged histogram differs from one that recorded every value")
	}
}

func TestEmptyAndNegative(t *testing.T) {
	var h Histogram
	if h.Quantile(0.99) != 0 || h.Mean() != 0 {
		t.Fatal("empty histogram must report zero")
	}
	h.Record(-time.Second)
	if h.Count() != 1 || h.Max() != 0 || h.Quantile(1) != 0 {
		t.Fatal("negative durations must count as zero")
	}
}
