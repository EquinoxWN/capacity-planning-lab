package report

import (
	"fmt"
	"math"
	"strings"

	"github.com/EquinoxWN/capacity-planning-lab/go/internal/usage"
)

// Limits used by the verdicts; they mirror the critical-to-quality targets in RFC 0001.
const (
	MaxRecoveryS   = 10.0 // spike: back within the SLO this soon after the peak ends
	RecoveryHoldS  = 3    // spike: samples in a row that must be healthy to call it recovered
	MaxDriftRatio  = 1.25 // soak: last third over first third, heap and goroutines
	FastShedFactor = 1.5  // spike: shed responses within 1.5x the queue's max wait
	ClientUtilMax  = 0.8  // the load generator must stay under 80% of its CPU cap
	SaturatedCPU   = 0.9  // service CPU share of its cap treated as saturated
	edgeTrimS      = 1.0  // seconds skipped at the start of each window (transition)
)

// Context is what the runner knows besides the k6 summary and the samples.
type Context struct {
	OffsetS      float64 `json:"offsetS"`      // sampler seconds at which k6 sent its first request
	MaxWaitMs    float64 `json:"maxWaitMs"`    // the service's queue max wait
	ClientUtil   float64 `json:"clientUtil"`   // k6 CPU time / (wall time x its GOMAXPROCS)
	ServiceAlive bool    `json:"serviceAlive"` // /healthz answered after the run
}

// WindowResult joins the client and server view of one window.
type WindowResult struct {
	Window        Window       `json:"window"`
	Client        Scenario     `json:"client"`
	Server        usage.Window `json:"server"`
	ServerOK      bool         `json:"serverSampled"`
	MeetsSLO      bool         `json:"meetsSlo"`
	ClientLimited bool         `json:"clientLimited"`
	Misses        []string     `json:"misses,omitempty"`
}

// Break is the outcome of a stress test.
type Break struct {
	Found         bool     `json:"found"`
	SustainedRate float64  `json:"sustainedRate"` // highest step that met the SLO before the persistent misses
	FirstMissRate float64  `json:"firstMissRate"` // first step of the persistent misses (0 if none)
	FirstMissStep string   `json:"firstMissStep"` // its name
	Confirmed     bool     `json:"confirmed"`     // a higher step missed the SLO as well
	ClientLimited bool     `json:"clientLimited"` // that step's miss was the load generator, not the service
	Transient     []string `json:"transient"`     // steps that missed but were followed by a passing step
	Reason        string   `json:"reason"`
}

// Drift compares the last third of a soak window with its first third.
type Drift struct {
	HeapRatio       float64 `json:"heapRatio"`
	GoroutinesRatio float64 `json:"goroutinesRatio"`
	P95FirstMs      float64 `json:"meanP95FirstMs"` // mean of the per-second server p95, first third
	P95LastMs       float64 `json:"meanP95LastMs"`  // the same for the last third
}

// Verdict is the pass or fail of a run with the facts behind it.
type Verdict struct {
	Pass      bool     `json:"pass"`
	Checks    []Check  `json:"checks"`
	Break     *Break   `json:"break,omitempty"`
	RecoveryS *float64 `json:"recoveryS,omitempty"`
	Drift     *Drift   `json:"drift,omitempty"`
}

// Check is one named pass/fail fact.
type Check struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"`
}

// allowedDrops is the dropped-iteration budget of a window, as in profiles.js.
func allowedDrops(w Window, slo SLO) float64 {
	return math.Max(1, math.Floor(w.Rate*(w.EndS-w.StartS)*slo.DroppedRate))
}

// misses lists which SLO parts a window's client numbers break.
func misses(sc Scenario, w Window, slo SLO) []string {
	var out []string
	if sc.P95Ms > slo.P95Ms {
		out = append(out, fmt.Sprintf("p95 %.1f ms > %.0f ms", sc.P95Ms, slo.P95Ms))
	}
	if sc.FailedRate > slo.ErrorRate {
		out = append(out, fmt.Sprintf("errors %.2f%% > %.0f%%", sc.FailedRate*100, slo.ErrorRate*100))
	}
	if sc.DroppedIterations > allowedDrops(w, slo) {
		out = append(out, fmt.Sprintf("client dropped %.0f iterations > %.0f", sc.DroppedIterations, allowedDrops(w, slo)))
	}
	if sc.Iterations == 0 {
		out = append(out, "no iterations ran")
	}
	return out
}

// Windows joins every k6 window with the server samples taken during it.
func Windows(s Summary, samples []usage.Sample, ctx Context) []WindowResult {
	out := make([]WindowResult, len(s.Windows))
	for i, w := range s.Windows {
		sc := s.Scenarios[i]
		from := ctx.OffsetS + w.StartS
		if w.EndS-w.StartS > 2*edgeTrimS {
			from += edgeTrimS
		}
		srv, ok := usage.Aggregate(samples, from, ctx.OffsetS+w.EndS)
		r := WindowResult{Window: w, Client: sc, Server: srv, ServerOK: ok}
		classify(&r, s.SLO)
		out[i] = r
	}
	return out
}

// classify sets a window's SLO misses, verdict and client-limited flag from its two views.
func classify(r *WindowResult, slo SLO) {
	m := misses(r.Client, r.Window, slo)
	if len(m) > 0 && r.Server.StalledSeconds > 0 {
		m = append(m, fmt.Sprintf("host stall: %.0f s with Go scheduler p99 > %.0f ms while the service used < %.0f%% of its CPU cap",
			r.Server.StalledSeconds, usage.StallSchedP99Ms, usage.StallCPUUtil*100))
	}
	r.Misses, r.MeetsSLO = m, r.Client.ThresholdsOK && len(m) == 0
	// Drops while the server was healthy mean the client, not the service, ran out of capacity.
	// Healthy needs all three USE views: no errors (shed), handler latency within the SLO, and no
	// CPU saturation, since requests can wait for a CPU before they ever reach the handler.
	dropMiss := r.Client.DroppedIterations > allowedDrops(r.Window, slo)
	r.ClientLimited = dropMiss && r.ServerOK && r.Server.ShedRate == 0 && r.Server.P95Ms <= slo.P95Ms &&
		r.Server.SchedP99Ms <= slo.P95Ms && r.Server.CPUUtilAvg < SaturatedCPU
}

// Rejudge recomputes a stored report's window verdicts and checks with the current rules,
// without running any load again.
func Rejudge(r *Report) {
	s := Summary{Kind: r.Kind, SLO: r.SLO, Totals: r.Client}
	for i := range r.Windows {
		classify(&r.Windows[i], r.SLO)
		s.Windows = append(s.Windows, r.Windows[i].Window)
	}
	r.Verdict = Judge(s, r.Windows, r.Samples, r.Context)
}

// Judge applies the kind-specific verdict to the joined windows.
func Judge(s Summary, ws []WindowResult, samples []usage.Sample, ctx Context) Verdict {
	v := Verdict{}
	add := func(name string, pass bool, detail string) {
		v.Checks = append(v.Checks, Check{Name: name, Pass: pass, Detail: detail})
	}
	add("service alive after the run", ctx.ServiceAlive, "GET /healthz after k6 finished")
	add("load generator under 80% of its CPU cap", ctx.ClientUtil < ClientUtilMax,
		fmt.Sprintf("k6 used %.0f%% of its CPU cap", ctx.ClientUtil*100))
	for _, w := range ws {
		if w.Window.Gate {
			detail := "meets the SLO"
			if !w.MeetsSLO {
				detail = "misses: " + strings.Join(w.Misses, "; ")
			}
			add(fmt.Sprintf("window %s (%.0f req/s) meets the SLO", w.Window.Name, w.Window.Rate), w.MeetsSLO, detail)
		}
	}
	switch s.Kind {
	case "stress":
		b := breakingPoint(ws)
		v.Break = &b
		add("breaking point found within the steps and confirmed by a higher step", b.Found && b.Confirmed && b.SustainedRate > 0, b.Reason)
	case "spike":
		judgeSpike(s, ws, samples, ctx, &v, add)
	case "soak":
		if d, ok := drift(s, samples, ctx); ok {
			v.Drift = &d
			add("heap does not grow (last third / first third)", d.HeapRatio <= MaxDriftRatio, fmt.Sprintf("ratio %.2f, limit %.2f", d.HeapRatio, MaxDriftRatio))
			add("goroutines do not grow (last third / first third)", d.GoroutinesRatio <= MaxDriftRatio, fmt.Sprintf("ratio %.2f, limit %.2f", d.GoroutinesRatio, MaxDriftRatio))
		} else {
			add("soak window sampled", false, "no server samples in the soak window")
		}
	}
	v.Pass = true
	for _, c := range v.Checks {
		v.Pass = v.Pass && c.Pass
	}
	return v
}

// breakingPoint finds the first step of the trailing run of SLO misses. Load only rises, so a real
// capacity limit keeps failing; a miss followed by a passing step is noise and is listed as transient.
func breakingPoint(ws []WindowResult) Break {
	b := Break{Transient: []string{}}
	first := len(ws)
	for first > 0 && !ws[first-1].MeetsSLO {
		first--
	}
	for _, w := range ws[:first] {
		if !w.MeetsSLO {
			b.Transient = append(b.Transient, w.Window.Name)
		}
	}
	noise := ""
	if len(b.Transient) > 0 {
		noise = fmt.Sprintf(" (transient misses at %s, followed by passing steps)", strings.Join(b.Transient, ", "))
	}
	if first == len(ws) {
		last := 0.0
		if len(ws) > 0 {
			last = ws[len(ws)-1].Window.Rate
			b.SustainedRate = last
		}
		b.Reason = fmt.Sprintf("no step missed the SLO for good up to %.0f req/s; add higher steps%s", last, noise)
		return b
	}
	w := ws[first]
	if first > 0 {
		b.SustainedRate = ws[first-1].Window.Rate
	}
	b.FirstMissRate, b.FirstMissStep, b.Confirmed = w.Window.Rate, w.Window.Name, first < len(ws)-1
	if w.ClientLimited {
		b.ClientLimited = true
		b.Reason = fmt.Sprintf("the load generator could not deliver %.0f req/s while the service was healthy; capacity is at least %.0f req/s%s", w.Window.Rate, b.SustainedRate, noise)
		return b
	}
	b.Found = true
	b.Reason = fmt.Sprintf("from %s at %.0f req/s every step misses the SLO: %s%s", w.Window.Name, w.Window.Rate, strings.Join(w.Misses, ", "), noise)
	if !b.Confirmed {
		b.Reason += "; only the last step missed, so this is unconfirmed"
	}
	if first == 0 {
		b.Reason = "the first step already missed the SLO; start lower. " + b.Reason
	}
	return b
}

// judgeSpike checks fast shedding during the peak and recovery after it.
func judgeSpike(s Summary, ws []WindowResult, samples []usage.Sample, ctx Context, v *Verdict, add func(string, bool, string)) {
	for _, w := range ws {
		if w.Window.Name != "spike" {
			continue
		}
		// Both sides must agree: the service's 503s were fast, and the client's slowest requests too.
		// A request stuck before the handler (connection or accept backlog) shows only on the client.
		limit := ctx.MaxWaitMs * FastShedFactor
		add("overload is refused fast (client p99 and server shed p99)", w.Server.ShedP99Ms <= limit && w.Client.P99Ms <= limit,
			fmt.Sprintf("client p99 %.1f ms, server shed p99 %.1f ms, limit %.0f ms; the service shed %d and answered %d; %.0f client transport errors in the run (connection refused, or no answer within the 2 s timeout)",
				w.Client.P99Ms, w.Server.ShedP99Ms, limit, w.Server.ShedQueueFull+w.Server.ShedWaitTimeout, w.Server.OK, s.Totals.TransportErrors))
		rec, ok := recovery(samples, ctx.OffsetS+w.Window.EndS, s.SLO.P95Ms)
		if ok {
			v.RecoveryS = &rec
		}
		add(fmt.Sprintf("back within the SLO within %.0f s of the peak ending", MaxRecoveryS), ok && rec <= MaxRecoveryS,
			map[bool]string{true: fmt.Sprintf("recovered after %.1f s", rec), false: "never recovered in the samples"}[ok])
	}
}

// recovery returns seconds from end until RecoveryHoldS healthy samples in a row begin.
func recovery(samples []usage.Sample, end, p95 float64) (float64, bool) {
	run := 0
	start := 0.0
	for _, s := range samples {
		if s.T <= end {
			continue
		}
		healthy := s.ShedQueueFull+s.ShedWaitTimeout == 0 && s.P95Ms <= p95
		if !healthy {
			run = 0
			continue
		}
		if run == 0 {
			start = math.Max(0, s.T-s.Dur-end)
		}
		run++
		if run >= RecoveryHoldS {
			return start, true
		}
	}
	return 0, false
}

// drift compares the first and last thirds of the gated soak window.
func drift(s Summary, samples []usage.Sample, ctx Context) (Drift, bool) {
	for _, w := range s.Windows {
		if w.Name != "soak" {
			continue
		}
		from, to := ctx.OffsetS+w.StartS+edgeTrimS, ctx.OffsetS+w.EndS
		third := (to - from) / 3
		a, okA := usage.Aggregate(samples, from, from+third)
		b, okB := usage.Aggregate(samples, to-third, to)
		if !okA || !okB || a.HeapAvgBytes == 0 || a.GoroutinesAvg == 0 {
			return Drift{}, false
		}
		return Drift{
			HeapRatio:       b.HeapAvgBytes / a.HeapAvgBytes,
			GoroutinesRatio: b.GoroutinesAvg / a.GoroutinesAvg,
			P95FirstMs:      meanP95(samples, from, from+third),
			P95LastMs:       meanP95(samples, to-third, to),
		}, true
	}
	return Drift{}, false
}

// meanP95 averages the per-second server p95 of samples inside [from, to].
func meanP95(samples []usage.Sample, from, to float64) float64 {
	sum, n := 0.0, 0
	for _, s := range samples {
		if s.T-s.Dur >= from-0.05 && s.T <= to+0.05 {
			sum += s.P95Ms
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}
