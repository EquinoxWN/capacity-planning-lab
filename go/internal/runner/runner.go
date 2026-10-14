// Package runner starts the service in-process, drives one k6 test against it, samples
// USE signals the whole time, and writes the joined report.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/EquinoxWN/capacity-planning-lab/go/internal/report"
	"github.com/EquinoxWN/capacity-planning-lab/go/internal/service"
	"github.com/EquinoxWN/capacity-planning-lab/go/internal/usage"
)

// Kinds are the test kinds, in the order the suite runs them.
var Kinds = []string{"stress", "load", "spike", "soak"}

// Params are the load-shape inputs passed to the k6 script; zero means the script default.
type Params struct {
	Scale string
	Rate  int
	Peak  int
	HoldS int
	Steps []int
}

// Env renders the parameters as k6 -e variables.
func (p Params) Env() map[string]string {
	env := map[string]string{"SCALE": p.Scale}
	put := func(k string, v int) {
		if v > 0 {
			env[k] = strconv.Itoa(v)
		}
	}
	put("RATE", p.Rate)
	put("PEAK", p.Peak)
	put("HOLD_S", p.HoldS)
	if len(p.Steps) > 0 {
		parts := make([]string, len(p.Steps))
		for i, s := range p.Steps {
			parts[i] = strconv.Itoa(s)
		}
		env["STEPS"] = strings.Join(parts, ",")
	}
	return env
}

// ClientResult is what the load generator reports besides its summary file.
type ClientResult struct {
	CPUSeconds float64
	Version    string
}

// LoadFunc drives load at baseURL and writes the k6 summary to summaryPath.
type LoadFunc func(ctx context.Context, kind, baseURL, summaryPath string, p Params) (ClientResult, error)

// Options configures one run.
type Options struct {
	Kind        string
	Params      Params
	Service     service.Config
	Addr        string // loopback listen address; port 0 picks a free port
	OutDir      string
	ClientProcs int
	Interval    time.Duration
	Log         func(format string, args ...any)
}

// Run executes one test and writes <kind>.json and <kind>.md to OutDir.
func Run(ctx context.Context, o Options, load LoadFunc) (report.Report, error) {
	if !validKind(o.Kind) {
		return report.Report{}, fmt.Errorf("unknown test kind %q", o.Kind)
	}
	if o.Interval <= 0 {
		o.Interval = time.Second
	}
	if o.Log == nil {
		o.Log = func(string, ...any) {}
	}
	if err := os.MkdirAll(o.OutDir, 0o750); err != nil {
		return report.Report{}, err
	}
	svc, err := service.New(o.Service, time.Now)
	if err != nil {
		return report.Report{}, err
	}
	ln, err := service.Listen(o.Addr)
	if err != nil {
		return report.Report{}, err
	}
	srv := service.NewServer(svc.Handler())
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	base := "http://" + ln.Addr().String()
	if err := waitHealthy(ctx, base); err != nil {
		return report.Report{}, err
	}
	o.Log("service listening on %s (GOMAXPROCS=%d, memory limit %d MiB)", base, runtime.GOMAXPROCS(0), debug.SetMemoryLimit(-1)>>20)

	sampler := usage.NewSampler(svc.Stats(), runtime.GOMAXPROCS(0), o.Service.Workers)
	sctx, stopSampling := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() { sampler.Run(sctx, o.Interval) })

	summaryPath := filepath.Join(o.OutDir, o.Kind+".k6.json")
	_ = os.Remove(summaryPath) // a stale summary must never be read as this run's
	started := time.Now()
	client, loadErr := load(ctx, o.Kind, base, summaryPath, o.Params)
	wall := time.Since(started).Seconds()
	alive := healthy(base)
	stopSampling()
	wg.Wait()
	if loadErr != nil {
		return report.Report{}, loadErr
	}
	select {
	case err := <-serveErr:
		return report.Report{}, fmt.Errorf("service stopped during the run: %w", err)
	default:
	}

	f, err := os.Open(summaryPath)
	if err != nil {
		return report.Report{}, fmt.Errorf("k6 summary: %w", err)
	}
	sum, err := report.ParseSummary(f)
	_ = f.Close()
	if err != nil {
		return report.Report{}, err
	}
	if sum.Kind != o.Kind {
		return report.Report{}, fmt.Errorf("k6 summary is for %q, expected %q", sum.Kind, o.Kind)
	}
	samples, dropped := sampler.Samples()
	first := svc.Stats().FirstRequest()
	if first.IsZero() {
		return report.Report{}, errors.New("the service received no requests")
	}
	clientProcs := max(o.ClientProcs, 1)
	rctx := report.Context{
		OffsetS:      sampler.Since(first),
		MaxWaitMs:    float64(o.Service.MaxWait) / float64(time.Millisecond),
		ClientUtil:   client.CPUSeconds / (wall * float64(clientProcs)),
		ServiceAlive: alive,
	}
	ws := report.Windows(sum, samples, rctx)
	rep := report.Report{
		Kind:      o.Kind,
		Label:     sum.Label,
		StartedAt: started.UTC().Format(time.RFC3339),
		Env: report.Env{
			OS: runtime.GOOS, Arch: runtime.GOARCH, LogicalCPUs: runtime.NumCPU(), GoVersion: runtime.Version(),
			K6Version: client.Version, Procs: runtime.GOMAXPROCS(0), MemLimitBytes: debug.SetMemoryLimit(-1),
			ClientProcs: clientProcs, Note: report.SharedMachineNote,
		},
		Service: report.ServiceCfg{
			Workers: o.Service.Workers, QueueLimit: o.Service.QueueLimit,
			MaxWaitMs: rctx.MaxWaitMs, WorkIters: o.Service.WorkIters, WorkKB: o.Service.WorkKB,
		},
		SLO:              sum.SLO,
		Context:          rctx,
		ClientCPUSeconds: client.CPUSeconds,
		WallSeconds:      wall,
		Client:           sum.Totals,
		Windows:          ws,
		Verdict:          report.Judge(sum, ws, samples, rctx),
		Samples:          samples,
		DroppedSamples:   dropped,
	}
	return rep, write(o.OutDir, rep)
}

// write stores the report as JSON and Markdown.
func write(dir string, r report.Report) error {
	j, err := json.MarshalIndent(r, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, r.Kind+".json"), append(j, '\n'), 0o640); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, r.Kind+".md"), []byte(report.Markdown(r)), 0o640)
}

// validKind reports whether k is one of Kinds.
func validKind(k string) bool {
	for _, x := range Kinds {
		if x == k {
			return true
		}
	}
	return false
}

// healthy reports whether /healthz answers 200 within a second.
func healthy(base string) bool {
	c := http.Client{Timeout: time.Second}
	res, err := c.Get(base + "/healthz")
	if err != nil {
		return false
	}
	_ = res.Body.Close()
	return res.StatusCode == http.StatusOK
}

// waitHealthy polls /healthz until it answers or five seconds pass.
func waitHealthy(ctx context.Context, base string) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if healthy(base) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	return errors.New("service did not become healthy within 5 s")
}
