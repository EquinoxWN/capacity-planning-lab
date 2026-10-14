package report

import (
	"fmt"
	"strings"

	"github.com/EquinoxWN/capacity-planning-lab/go/internal/usage"
)

// Env describes where and how a run happened.
type Env struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	LogicalCPUs   int    `json:"logicalCpus"`
	GoVersion     string `json:"goVersion"`
	K6Version     string `json:"k6Version"`
	Procs         int    `json:"serviceGomaxprocs"`
	MemLimitBytes int64  `json:"serviceMemLimitBytes"`
	ClientProcs   int    `json:"clientGomaxprocs"`
	Note          string `json:"note"`
}

// ServiceCfg is the service configuration of the run.
type ServiceCfg struct {
	Workers    int     `json:"workers"`
	QueueLimit int     `json:"queueLimit"`
	MaxWaitMs  float64 `json:"maxWaitMs"`
	WorkIters  int     `json:"workIters"`
	WorkKB     int     `json:"workKB"`
}

// Report is everything one run produced.
type Report struct {
	Kind             string         `json:"kind"`
	Label            string         `json:"label"`
	StartedAt        string         `json:"startedAt"`
	Env              Env            `json:"env"`
	Service          ServiceCfg     `json:"service"`
	SLO              SLO            `json:"slo"`
	Context          Context        `json:"context"`
	ClientCPUSeconds float64        `json:"clientCpuSeconds"`
	WallSeconds      float64        `json:"wallSeconds"`
	Client           Totals         `json:"clientTotals"`
	Windows          []WindowResult `json:"windows"`
	Verdict          Verdict        `json:"verdict"`
	Samples          []usage.Sample `json:"samples"`
	DroppedSamples   int            `json:"droppedSamples"`
}

// SharedMachineNote is printed with every result.
const SharedMachineNote = "k6 and the service run on the same machine: both draw on the same CPUs, so these are lab numbers for comparison, not production capacity."

// pct formats a fraction as a percentage.
func pct(v float64) string { return fmt.Sprintf("%.1f%%", v*100) }

// optPct formats an optional fraction.
func optPct(v *float64) string {
	if v == nil {
		return "n/a"
	}
	return pct(*v)
}

// mib formats bytes as MiB.
func mib(v float64) string { return fmt.Sprintf("%.1f MiB", v/(1<<20)) }

// Focus picks the window whose USE numbers best describe the run.
func (r Report) Focus() *WindowResult {
	pick := func(name string) *WindowResult {
		for i := range r.Windows {
			if r.Windows[i].Window.Name == name {
				return &r.Windows[i]
			}
		}
		return nil
	}
	switch r.Kind {
	case "stress":
		if b := r.Verdict.Break; b != nil && b.FirstMissStep != "" {
			return pick(b.FirstMissStep)
		}
		if len(r.Windows) > 0 {
			return &r.Windows[len(r.Windows)-1]
		}
	case "spike":
		return pick("spike")
	default:
		return pick(r.Kind)
	}
	return nil
}

// USETable renders utilization, saturation and errors for one window.
func (r Report) USETable(w *WindowResult) string {
	var b strings.Builder
	s := w.Server
	fmt.Fprintf(&b, "| Resource | Utilization | Saturation | Errors |\n|---|---|---|---|\n")
	fmt.Fprintf(&b, "| Service CPU (GOMAXPROCS=%d) | avg %s, max %s | Go scheduler latency p99 %.2f ms | none at this level |\n",
		r.Env.Procs, pct(s.CPUUtilAvg), pct(s.CPUUtilMax), s.SchedP99Ms)
	fmt.Fprintf(&b, "| Worker pool (%d workers) | busy avg %s, max %s | queue avg %.1f, max %d of %d; queue wait p99 %.1f ms | shed: %d queue full, %d wait timeout (%s of requests) |\n",
		r.Service.Workers, pct(s.WorkerUtilAvg), pct(s.WorkerUtilMax), s.QueueAvg, s.QueueMax, r.Service.QueueLimit, s.WaitP99Ms, s.ShedQueueFull, s.ShedWaitTimeout, pct(s.ShedRate))
	alive := "process stayed up, /healthz answered"
	if !r.Context.ServiceAlive {
		alive = "**/healthz did not answer after the run**"
	}
	fmt.Fprintf(&b, "| Memory (limit %s) | max %s of the limit (%s mapped, heap max %s) | GC CPU %s, %.1f GC cycles/s | %s |\n",
		mib(float64(r.Env.MemLimitBytes)), pct(s.MemLimitUtilMax), mib(float64(s.MemMappedMax)), mib(float64(s.HeapMaxBytes)), pct(s.GCCPUFrac), s.GCPerSec, alive)
	fmt.Fprintf(&b, "| Host CPU (%d logical CPUs, shared with k6 and other work) | avg %s, max %s | run queue not read; %.0f s stalled (Go scheduler p99 > %.0f ms while the service used < %.0f%% of its CPU cap) | n/a |\n",
		r.Env.LogicalCPUs, optPct(s.HostCPUUtilAvg), optPct(s.HostCPUUtilMax), s.StalledSeconds, usage.StallSchedP99Ms, usage.StallCPUUtil*100)
	fmt.Fprintf(&b, "| Load generator (k6, GOMAXPROCS=%d, whole run) | %s of its CPU cap (%.1f CPU-s in %.1f s) | dropped iterations %.0f in this window (%.0f in the run), VUs max %.0f | transport errors %.0f in the run |\n",
		r.Env.ClientProcs, pct(r.Context.ClientUtil), r.ClientCPUSeconds, r.WallSeconds, w.Client.DroppedIterations, r.Client.DroppedIterations, r.Client.VUsMax, r.Client.TransportErrors)
	return b.String()
}

// Markdown renders the whole report for docs/results.
func Markdown(r Report) string {
	var b strings.Builder
	verdict := "PASS"
	if !r.Verdict.Pass {
		verdict = "FAIL"
	}
	fmt.Fprintf(&b, "# %s run: %s\n\n", r.Kind, verdict)
	fmt.Fprintf(&b, "%s.\n\n", strings.TrimSuffix(r.Label, "."))
	fmt.Fprintf(&b, "- Started %s on %s/%s, %d logical CPUs; %s, k6 %s.\n", r.StartedAt, r.Env.OS, r.Env.Arch, r.Env.LogicalCPUs, r.Env.GoVersion, r.Env.K6Version)
	fmt.Fprintf(&b, "- Service: bound to 127.0.0.1, GOMAXPROCS=%d, memory limit %s, %d workers, queue %d, max wait %.0f ms, %d SHA-256 rounds and %d KiB per request.\n",
		r.Env.Procs, mib(float64(r.Env.MemLimitBytes)), r.Service.Workers, r.Service.QueueLimit, r.Service.MaxWaitMs, r.Service.WorkIters, r.Service.WorkKB)
	fmt.Fprintf(&b, "- Client: k6 with GOMAXPROCS=%d. %s\n", r.Env.ClientProcs, SharedMachineNote)
	fmt.Fprintf(&b, "- SLO for gated windows: p95 <= %.0f ms, errors <= %.0f%%, dropped iterations <= %.0f%%.\n\n", r.SLO.P95Ms, r.SLO.ErrorRate*100, r.SLO.DroppedRate*100)

	b.WriteString("| Window | Target req/s | Client p95 ms | Client p99 ms | Failed | Dropped | Server ok/s | Shed | CPU avg | Workers busy | Queue avg/max | Sched p99 ms | Host CPU | Stalled s | SLO |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, w := range r.Windows {
		slo := "meets"
		if !w.MeetsSLO {
			slo = "misses"
		}
		if w.ClientLimited {
			slo += " (client-limited)"
		}
		if w.Window.Gate {
			slo += ", gated"
		}
		s := w.Server
		fmt.Fprintf(&b, "| %s | %.0f | %.1f | %.1f | %s | %.0f | %.0f | %s | %s | %s | %.1f/%d | %.2f | %s | %.0f | %s |\n",
			w.Window.Name, w.Window.Rate, w.Client.P95Ms, w.Client.P99Ms, pct(w.Client.FailedRate), w.Client.DroppedIterations,
			s.OKPerSec, pct(s.ShedRate), pct(s.CPUUtilAvg), pct(s.WorkerUtilAvg), s.QueueAvg, s.QueueMax, s.SchedP99Ms, optPct(s.HostCPUUtilAvg), s.StalledSeconds, slo)
	}
	if f := r.Focus(); f != nil {
		fmt.Fprintf(&b, "\n## USE method, window %s (%.0f req/s)\n\n%s", f.Window.Name, f.Window.Rate, r.USETable(f))
	}
	b.WriteString("\n## Checks\n\n")
	for _, c := range r.Verdict.Checks {
		mark := "pass"
		if !c.Pass {
			mark = "FAIL"
		}
		fmt.Fprintf(&b, "- %s: %s (%s)\n", mark, c.Name, c.Detail)
	}
	if br := r.Verdict.Break; br != nil {
		fmt.Fprintf(&b, "\nBreaking point: %s. Highest step that met the SLO: %.0f req/s.\n", br.Reason, br.SustainedRate)
	}
	if r.Verdict.RecoveryS != nil {
		fmt.Fprintf(&b, "\nRecovery after the spike: %.1f s.\n", *r.Verdict.RecoveryS)
	}
	if d := r.Verdict.Drift; d != nil {
		fmt.Fprintf(&b, "\nDrift (last third / first third): heap x%.2f, goroutines x%.2f, mean per-second server p95 %.1f -> %.1f ms.\n", d.HeapRatio, d.GoroutinesRatio, d.P95FirstMs, d.P95LastMs)
	}
	if r.DroppedSamples > 0 {
		fmt.Fprintf(&b, "\n%d samples were dropped at the %d-sample cap.\n", r.DroppedSamples, usage.MaxSamples)
	}
	return b.String()
}
