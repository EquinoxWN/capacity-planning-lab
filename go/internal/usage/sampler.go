// Package usage samples the USE signals (utilization, saturation, errors) of the service,
// its Go runtime and the host once per interval.
package usage

import (
	"context"
	"sync"
	"time"

	"github.com/EquinoxWN/capacity-planning-lab/go/internal/hist"
	"github.com/EquinoxWN/capacity-planning-lab/go/internal/service"
)

// MaxSamples bounds memory: one hour of one-second samples.
const MaxSamples = 3600

// Stall thresholds: runnable goroutines waited this long (p99) while the process used less than
// this share of its CPU cap, so the CPU went to other processes on the machine.
const (
	StallSchedP99Ms = 50.0
	StallCPUUtil    = 0.7
)

// Stalled reports whether a sample shows the host withholding CPU from the service.
func (s *Sample) Stalled() bool { return s.SchedP99Ms > StallSchedP99Ms && s.CPUUtil < StallCPUUtil }

// Cutter hands out the service statistics of the interval that just ended.
type Cutter interface {
	Cut() service.Interval
}

// Sample is one interval of USE signals; every rate is per second of the interval.
type Sample struct {
	T               float64  `json:"t"`   // seconds since sampling started, at the end of the interval
	Dur             float64  `json:"dur"` // interval length in seconds
	OK              uint64   `json:"ok"`
	ShedQueueFull   uint64   `json:"shedQueueFull"`
	ShedWaitTimeout uint64   `json:"shedWaitTimeout"`
	Canceled        uint64   `json:"canceled"`
	P50Ms           float64  `json:"p50Ms"`
	P95Ms           float64  `json:"p95Ms"`
	P99Ms           float64  `json:"p99Ms"`
	WaitP99Ms       float64  `json:"waitP99Ms"`
	WorkerUtil      float64  `json:"workerUtil"`
	QueueAvg        float64  `json:"queueAvg"`
	QueueMax        int      `json:"queueMax"`
	CPUUtil         float64  `json:"cpuUtil"`               // process CPU time / (interval x GOMAXPROCS)
	HostCPUUtil     *float64 `json:"hostCpuUtil,omitempty"` // whole machine, all logical CPUs
	MemMappedBytes  uint64   `json:"memMappedBytes"`        // runtime memory minus memory returned to the OS
	MemLimitUtil    float64  `json:"memLimitUtil"`
	HeapBytes       uint64   `json:"heapBytes"`
	GCCycles        uint64   `json:"gcCycles"`
	GCCPUFrac       float64  `json:"gcCpuFrac"` // GC assist + dedicated mark + pause CPU / total CPU capacity
	SchedP99Ms      float64  `json:"schedP99Ms"`
	Goroutines      uint64   `json:"goroutines"`

	iv           service.Interval
	sched        []uint64
	schedBuckets []float64
	gcBusy, cpu  float64
	hostBusy     time.Duration
	hostTotal    time.Duration
}

// Sampler records one Sample per interval until its context ends.
type Sampler struct {
	src     Cutter
	procs   int
	workers int
	start   time.Time

	mu      sync.Mutex
	samples []Sample
	dropped int

	prevCPU              time.Duration
	prevHostBusy, prevHT time.Duration
	hostOK               bool
	prevRT               runtimeReading
	lastTick             time.Time
}

// NewSampler starts a sampler; procs is GOMAXPROCS and workers the pool size.
func NewSampler(src Cutter, procs, workers int) *Sampler {
	s := &Sampler{src: src, procs: max(procs, 1), workers: max(workers, 1)}
	src.Cut() // discard anything before sampling starts
	s.prevCPU, _ = ProcessCPU()
	if b, t, err := HostCPU(); err == nil {
		s.prevHostBusy, s.prevHT, s.hostOK = b, t, true
	}
	s.prevRT = readRuntime()
	s.start = time.Now()
	s.lastTick = s.start
	return s
}

// Since converts a wall-clock time to seconds since sampling started.
func (s *Sampler) Since(t time.Time) float64 { return t.Sub(s.start).Seconds() }

// Run samples every interval and once more when ctx ends.
func (s *Sampler) Run(ctx context.Context, interval time.Duration) {
	tk := time.NewTicker(interval)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			s.Tick()
			return
		case <-tk.C:
			s.Tick()
		}
	}
}

// Tick closes the current interval and stores its sample.
func (s *Sampler) Tick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	iv := s.src.Cut()
	dur := now.Sub(s.lastTick).Seconds()
	s.lastTick = now
	if dur <= 0 {
		return
	}
	smp := Sample{T: s.Since(now), Dur: dur, iv: iv}
	smp.OK = iv.Outcomes[service.OK]
	smp.ShedQueueFull = iv.Outcomes[service.ShedQueueFull]
	smp.ShedWaitTimeout = iv.Outcomes[service.ShedWaitTimeout]
	smp.Canceled = iv.Outcomes[service.Canceled]
	smp.P50Ms = ms(iv.Latency.Quantile(0.50))
	smp.P95Ms = ms(iv.Latency.Quantile(0.95))
	smp.P99Ms = ms(iv.Latency.Quantile(0.99))
	smp.WaitP99Ms = ms(iv.Wait.Quantile(0.99))
	smp.WorkerUtil = iv.BusySeconds / (dur * float64(s.workers))
	smp.QueueAvg = iv.QueueSeconds / dur
	smp.QueueMax = iv.QueueMax

	if cpu, err := ProcessCPU(); err == nil {
		smp.CPUUtil = (cpu - s.prevCPU).Seconds() / (dur * float64(s.procs))
		s.prevCPU = cpu
	}
	if s.hostOK {
		if b, t, err := HostCPU(); err == nil && t > s.prevHT {
			u := float64(b-s.prevHostBusy) / float64(t-s.prevHT)
			smp.HostCPUUtil = &u
			smp.hostBusy, smp.hostTotal = b-s.prevHostBusy, t-s.prevHT
			s.prevHostBusy, s.prevHT = b, t
		}
	}
	rt := readRuntime()
	smp.MemMappedBytes = rt.memMapped
	if rt.memLimit > 0 {
		smp.MemLimitUtil = float64(rt.memMapped) / float64(rt.memLimit)
	}
	smp.HeapBytes = rt.heapObjects
	smp.GCCycles = rt.gcCycles - min(s.prevRT.gcCycles, rt.gcCycles)
	smp.gcBusy, smp.cpu = rt.gcBusyCPU-s.prevRT.gcBusyCPU, rt.totalCPU-s.prevRT.totalCPU
	if smp.cpu > 0 {
		smp.GCCPUFrac = smp.gcBusy / smp.cpu
	}
	smp.sched, smp.schedBuckets = histDelta(s.prevRT.schedCounts, rt.schedCounts), rt.schedBuckets
	smp.SchedP99Ms = histQuantile(smp.sched, smp.schedBuckets, 0.99) * 1000
	smp.Goroutines = rt.goroutines
	s.prevRT = rt

	if len(s.samples) >= MaxSamples {
		s.dropped++
		return
	}
	s.samples = append(s.samples, smp)
}

// Samples returns a copy of the samples so far and how many were dropped at the cap.
func (s *Sampler) Samples() ([]Sample, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Sample(nil), s.samples...), s.dropped
}

// ms converts a duration to float milliseconds.
func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// Window summarises the samples that fall entirely inside [From, To] seconds.
type Window struct {
	From            float64  `json:"from"`
	To              float64  `json:"to"`
	Samples         int      `json:"samples"`
	Seconds         float64  `json:"seconds"`
	OK              uint64   `json:"ok"`
	ShedQueueFull   uint64   `json:"shedQueueFull"`
	ShedWaitTimeout uint64   `json:"shedWaitTimeout"`
	Canceled        uint64   `json:"canceled"`
	OKPerSec        float64  `json:"okPerSec"`
	ShedRate        float64  `json:"shedRate"` // shed / (ok + shed)
	P50Ms           float64  `json:"p50Ms"`
	P95Ms           float64  `json:"p95Ms"`
	P99Ms           float64  `json:"p99Ms"`
	MaxMs           float64  `json:"maxMs"`
	ShedP99Ms       float64  `json:"shedP99Ms"`
	WaitP99Ms       float64  `json:"waitP99Ms"`
	WorkerUtilAvg   float64  `json:"workerUtilAvg"`
	WorkerUtilMax   float64  `json:"workerUtilMax"`
	QueueAvg        float64  `json:"queueAvg"`
	QueueMax        int      `json:"queueMax"`
	CPUUtilAvg      float64  `json:"cpuUtilAvg"`
	CPUUtilMax      float64  `json:"cpuUtilMax"`
	HostCPUUtilAvg  *float64 `json:"hostCpuUtilAvg,omitempty"`
	HostCPUUtilMax  *float64 `json:"hostCpuUtilMax,omitempty"`
	MemLimitUtilMax float64  `json:"memLimitUtilMax"`
	MemMappedMax    uint64   `json:"memMappedMaxBytes"`
	HeapAvgBytes    float64  `json:"heapAvgBytes"`
	HeapMaxBytes    uint64   `json:"heapMaxBytes"`
	GCCPUFrac       float64  `json:"gcCpuFrac"`
	GCPerSec        float64  `json:"gcPerSec"`
	SchedP99Ms      float64  `json:"schedP99Ms"`
	GoroutinesAvg   float64  `json:"goroutinesAvg"`
	GoroutinesMax   uint64   `json:"goroutinesMax"`
	StalledSeconds  float64  `json:"stalledSeconds"` // seconds of samples where Stalled() held
}

// Aggregate summarises samples whose interval lies inside [from, to]; ok is false when none do.
func Aggregate(samples []Sample, from, to float64) (Window, bool) {
	const slack = 0.05 // ticker jitter
	w := Window{From: from, To: to}
	var lat, shed, wait hist.Histogram
	var sched []uint64
	var buckets []float64
	var busySec, queueSec, cpuSec, gcBusy, gcTotal, heapSum, gorSum float64
	var hostBusy, hostTotal time.Duration
	hostSeen := false
	for i := range samples {
		s := &samples[i]
		if s.T-s.Dur < from-slack || s.T > to+slack {
			continue
		}
		w.Samples++
		w.Seconds += s.Dur
		w.OK += s.OK
		w.ShedQueueFull += s.ShedQueueFull
		w.ShedWaitTimeout += s.ShedWaitTimeout
		w.Canceled += s.Canceled
		lat.Merge(&s.iv.Latency)
		shed.Merge(&s.iv.ShedLatency)
		wait.Merge(&s.iv.Wait)
		busySec += s.WorkerUtil * s.Dur
		queueSec += s.QueueAvg * s.Dur
		cpuSec += s.CPUUtil * s.Dur
		w.WorkerUtilMax = max(w.WorkerUtilMax, s.WorkerUtil)
		w.QueueMax = max(w.QueueMax, s.QueueMax)
		w.CPUUtilMax = max(w.CPUUtilMax, s.CPUUtil)
		if s.HostCPUUtil != nil {
			hostSeen = true
			hostBusy += s.hostBusy
			hostTotal += s.hostTotal
			if w.HostCPUUtilMax == nil || *s.HostCPUUtil > *w.HostCPUUtilMax {
				v := *s.HostCPUUtil
				w.HostCPUUtilMax = &v
			}
		}
		w.MemLimitUtilMax = max(w.MemLimitUtilMax, s.MemLimitUtil)
		w.MemMappedMax = max(w.MemMappedMax, s.MemMappedBytes)
		w.HeapMaxBytes = max(w.HeapMaxBytes, s.HeapBytes)
		heapSum += float64(s.HeapBytes)
		gcBusy += s.gcBusy
		gcTotal += s.cpu
		w.GCPerSec += float64(s.GCCycles)
		sched = sumInto(sched, s.sched)
		if len(s.schedBuckets) > 0 {
			buckets = s.schedBuckets
		}
		gorSum += float64(s.Goroutines)
		if s.Stalled() {
			w.StalledSeconds += s.Dur
		}
		w.GoroutinesMax = max(w.GoroutinesMax, s.Goroutines)
	}
	if w.Samples == 0 || w.Seconds <= 0 {
		return w, false
	}
	n := float64(w.Samples)
	w.OKPerSec = float64(w.OK) / w.Seconds
	if all := w.OK + w.ShedQueueFull + w.ShedWaitTimeout; all > 0 {
		w.ShedRate = float64(w.ShedQueueFull+w.ShedWaitTimeout) / float64(all)
	}
	w.P50Ms, w.P95Ms, w.P99Ms, w.MaxMs = ms(lat.Quantile(0.5)), ms(lat.Quantile(0.95)), ms(lat.Quantile(0.99)), ms(lat.Max())
	w.ShedP99Ms, w.WaitP99Ms = ms(shed.Quantile(0.99)), ms(wait.Quantile(0.99))
	w.WorkerUtilAvg, w.QueueAvg, w.CPUUtilAvg = busySec/w.Seconds, queueSec/w.Seconds, cpuSec/w.Seconds
	if hostSeen && hostTotal > 0 {
		v := float64(hostBusy) / float64(hostTotal)
		w.HostCPUUtilAvg = &v
	}
	w.HeapAvgBytes = heapSum / n
	if gcTotal > 0 {
		w.GCCPUFrac = gcBusy / gcTotal
	}
	w.GCPerSec /= w.Seconds
	w.SchedP99Ms = histQuantile(sched, buckets, 0.99) * 1000
	w.GoroutinesAvg = gorSum / n
	return w, true
}

// sumInto adds b into a bucket by bucket, growing a as needed.
func sumInto(a, b []uint64) []uint64 {
	if len(a) < len(b) {
		a = append(a, make([]uint64, len(b)-len(a))...)
	}
	for i, v := range b {
		a[i] += v
	}
	return a
}
