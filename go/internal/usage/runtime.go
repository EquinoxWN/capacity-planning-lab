package usage

import (
	"math"
	"runtime/metrics"
)

// Runtime metric names read on every sample.
const (
	mMemTotal    = "/memory/classes/total:bytes"
	mMemReleased = "/memory/classes/heap/released:bytes"
	mHeapObjects = "/memory/classes/heap/objects:bytes"
	mMemLimit    = "/gc/gomemlimit:bytes"
	mGCCycles    = "/gc/cycles/total:gc-cycles"
	mGCAssist    = "/cpu/classes/gc/mark/assist:cpu-seconds"
	mGCDedicated = "/cpu/classes/gc/mark/dedicated:cpu-seconds"
	mGCPause     = "/cpu/classes/gc/pause:cpu-seconds"
	mCPUTotal    = "/cpu/classes/total:cpu-seconds"
	mSchedLat    = "/sched/latencies:seconds"
	mGoroutines  = "/sched/goroutines:goroutines"
)

// runtimeReading is one read of the Go runtime's own counters.
type runtimeReading struct {
	memMapped, heapObjects, memLimit, gcCycles, goroutines uint64
	gcBusyCPU, totalCPU                                    float64
	schedCounts                                            []uint64
	schedBuckets                                           []float64
}

// readRuntime samples the runtime metrics this package uses.
func readRuntime() runtimeReading {
	names := []string{mMemTotal, mMemReleased, mHeapObjects, mMemLimit, mGCCycles, mGCAssist, mGCDedicated, mGCPause, mCPUTotal, mSchedLat, mGoroutines}
	s := make([]metrics.Sample, len(names))
	for i, n := range names {
		s[i].Name = n
	}
	metrics.Read(s)
	u := func(i int) uint64 {
		if s[i].Value.Kind() == metrics.KindUint64 {
			return s[i].Value.Uint64()
		}
		return 0
	}
	f := func(i int) float64 {
		if s[i].Value.Kind() == metrics.KindFloat64 {
			return s[i].Value.Float64()
		}
		return 0
	}
	r := runtimeReading{
		memMapped:   u(0) - min(u(1), u(0)),
		heapObjects: u(2),
		memLimit:    u(3),
		gcCycles:    u(4),
		gcBusyCPU:   f(5) + f(6) + f(7),
		totalCPU:    f(8),
		goroutines:  u(10),
	}
	if s[9].Value.Kind() == metrics.KindFloat64Histogram {
		h := s[9].Value.Float64Histogram()
		r.schedCounts = append([]uint64(nil), h.Counts...)
		r.schedBuckets = h.Buckets
	}
	return r
}

// histDelta returns cur minus prev bucket by bucket; prev may be empty.
func histDelta(prev, cur []uint64) []uint64 {
	out := make([]uint64, len(cur))
	for i, c := range cur {
		if i < len(prev) && prev[i] <= c {
			c -= prev[i]
		}
		out[i] = c
	}
	return out
}

// histQuantile returns the upper bucket bound holding the q-quantile of a runtime histogram.
func histQuantile(counts []uint64, buckets []float64, q float64) float64 {
	var total uint64
	for _, c := range counts {
		total += c
	}
	if total == 0 || len(buckets) != len(counts)+1 {
		return 0
	}
	rank := uint64(math.Ceil(q * float64(total)))
	rank = max(rank, 1)
	var seen uint64
	for i, c := range counts {
		seen += c
		if seen >= rank {
			hi := buckets[i+1]
			if math.IsInf(hi, 1) {
				return buckets[i]
			}
			return hi
		}
	}
	return buckets[len(buckets)-1]
}
