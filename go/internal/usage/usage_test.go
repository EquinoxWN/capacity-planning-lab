package usage

import (
	"crypto/sha256"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/EquinoxWN/capacity-planning-lab/go/internal/service"
)

// fakeCutter returns prepared intervals in order.
type fakeCutter struct{ next []service.Interval }

// Cut returns the next prepared interval, or an empty one.
func (f *fakeCutter) Cut() service.Interval {
	if len(f.next) == 0 {
		return service.Interval{}
	}
	iv := f.next[0]
	f.next = f.next[1:]
	return iv
}

func TestParseProcStat(t *testing.T) {
	in := []byte("cpu  100 20 30 800 50 0 0 0 0 0\ncpu0 1 2 3 4 5 6 7 8 9 10\n")
	busy, total, err := parseProcStat(in, 100)
	if err != nil {
		t.Fatal(err)
	}
	// busy = user+nice+system = 150 ticks; total = 1000 ticks; 100 ticks/s.
	if busy != 1500*time.Millisecond || total != 10*time.Second {
		t.Fatalf("busy %v total %v", busy, total)
	}
	for _, bad := range []string{"", "intr 1 2 3", "cpu 1 2 x 4 5", "cpu 1 -2 3 4 5"} {
		if _, _, err := parseProcStat([]byte(bad), 100); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestHistQuantile(t *testing.T) {
	buckets := []float64{math.Inf(-1), 0.001, 0.002, 0.004, math.Inf(1)}
	counts := []uint64{0, 90, 9, 1}
	if got := histQuantile(counts, buckets, 0.5); got != 0.002 {
		t.Fatalf("p50 %v, want 0.002", got)
	}
	if got := histQuantile(counts, buckets, 0.99); got != 0.004 {
		t.Fatalf("p99 %v, want 0.004", got)
	}
	if got := histQuantile(counts, buckets, 1); got != 0.004 {
		t.Fatalf("p100 in the +Inf bucket must report its lower bound, got %v", got)
	}
	if histQuantile([]uint64{0, 0}, []float64{0, 1, 2}, 0.5) != 0 || histQuantile([]uint64{1}, []float64{0}, 0.5) != 0 {
		t.Fatal("empty or malformed histograms must report 0")
	}
	if d := histDelta([]uint64{1, 2}, []uint64{3, 2, 5}); d[0] != 2 || d[1] != 0 || d[2] != 5 {
		t.Fatalf("delta %v", d)
	}
}

func TestCPUCountersMove(t *testing.T) {
	before, err := ProcessCPU()
	if errors.Is(err, ErrUnsupported) {
		t.Skip("no CPU counters on this platform")
	}
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(80 * time.Millisecond)
	sum := sha256.Sum256(nil)
	for time.Now().Before(deadline) {
		sum = sha256.Sum256(sum[:])
	}
	after, err := ProcessCPU()
	if err != nil || after <= before {
		t.Fatalf("process CPU did not advance: %v -> %v (%v)", before, after, err)
	}
	busy, total, err := HostCPU()
	if err != nil || busy <= 0 || busy > total {
		t.Fatalf("host CPU busy %v total %v err %v", busy, total, err)
	}
}

func TestSamplerAndAggregate(t *testing.T) {
	mk := func(ok uint64, busySec float64, lat time.Duration) service.Interval {
		var iv service.Interval
		iv.Outcomes[service.OK] = ok
		iv.Outcomes[service.ShedQueueFull] = 1
		iv.BusySeconds = busySec
		for range ok {
			iv.Latency.Record(lat)
		}
		return iv
	}
	src := &fakeCutter{}
	s := NewSampler(src, 2, 4)
	src.next = []service.Interval{mk(10, 0, time.Millisecond), mk(30, 0, 9*time.Millisecond)}
	time.Sleep(20 * time.Millisecond)
	s.Tick()
	time.Sleep(20 * time.Millisecond)
	s.Tick()
	got, dropped := s.Samples()
	if len(got) != 2 || dropped != 0 {
		t.Fatalf("%d samples, %d dropped", len(got), dropped)
	}
	if got[0].OK != 10 || got[1].OK != 30 || got[1].ShedQueueFull != 1 {
		t.Fatalf("counts %+v", got)
	}
	if got[0].T <= 0 || got[1].T <= got[0].T || got[1].Goroutines == 0 || got[1].MemMappedBytes == 0 {
		t.Fatalf("timeline or runtime fields missing: %+v", got[1])
	}
	w, ok := Aggregate(got, 0, got[1].T)
	if !ok || w.Samples != 2 || w.OK != 40 || w.ShedQueueFull != 2 {
		t.Fatalf("window %+v", w)
	}
	// 30 of 40 requests took 9 ms, so p95 lies in the 9 ms bucket and p50 too.
	if w.P95Ms < 9 || w.P95Ms > 9*(1+0.0906) || math.Abs(w.ShedRate-2.0/42) > 1e-9 {
		t.Fatalf("p95 %.3f shed rate %.4f", w.P95Ms, w.ShedRate)
	}
	if _, ok := Aggregate(got, 100, 200); ok {
		t.Fatal("a window with no samples must report ok=false")
	}
}

func TestSamplerCapsMemory(t *testing.T) {
	s := NewSampler(&fakeCutter{}, 1, 1)
	for range MaxSamples + 5 {
		s.lastTick = s.lastTick.Add(-time.Millisecond) // make every interval non-empty
		s.Tick()
	}
	got, dropped := s.Samples()
	if len(got) != MaxSamples || dropped != 5 {
		t.Fatalf("%d kept, %d dropped", len(got), dropped)
	}
}

func TestStalledSamplesAreCounted(t *testing.T) {
	samples := []Sample{
		{T: 1, Dur: 1, SchedP99Ms: 200, CPUUtil: 0.3},  // starved by the host
		{T: 2, Dur: 1, SchedP99Ms: 200, CPUUtil: 0.95}, // saturated by its own load
		{T: 3, Dur: 1, SchedP99Ms: 2, CPUUtil: 0.3},    // idle
	}
	w, ok := Aggregate(samples, 0, 3)
	if !ok || w.StalledSeconds != 1 {
		t.Fatalf("stalled %.0f s, want 1", w.StalledSeconds)
	}
}
