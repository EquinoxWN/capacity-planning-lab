package runner

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/EquinoxWN/capacity-planning-lab/go/internal/report"
)

// PlannedFraction is the share of the sustained stress rate used as the planned load (40% headroom).
const PlannedFraction = 0.6

// PeakFactor multiplies the sustained rate to get a spike peak that is surely over capacity.
const PeakFactor = 2.0

// SuiteResult is the outcome of stress, then load, spike and soak at rates derived from it.
type SuiteResult struct {
	Reports     []report.Report
	Sustained   float64
	PlannedRate int
	PeakRate    int
}

// Plan derives the planned and peak rates from a stress result.
func Plan(b report.Break) (planned, peak int, err error) {
	if !b.Found || !b.Confirmed || b.ClientLimited || b.SustainedRate <= 0 {
		return 0, 0, fmt.Errorf("stress test gave no usable breaking point: %s", b.Reason)
	}
	return int(math.Floor(b.SustainedRate * PlannedFraction)), int(math.Round(b.SustainedRate * PeakFactor)), nil
}

// Suite runs the stress test, derives the planned rate, then runs load, spike and soak.
func Suite(ctx context.Context, o Options, load LoadFunc) (SuiteResult, error) {
	res := SuiteResult{}
	if o.Log == nil {
		o.Log = func(string, ...any) {}
	}
	o.Kind = "stress"
	stress, err := Run(ctx, o, load)
	if err != nil {
		return res, err
	}
	res.Reports = append(res.Reports, stress)
	planned, peak, err := Plan(*stress.Verdict.Break)
	if err != nil {
		return res, err
	}
	res.Sustained, res.PlannedRate, res.PeakRate = stress.Verdict.Break.SustainedRate, planned, peak
	o.Log("stress: sustained %.0f req/s; planned load %d req/s (%.0f%% headroom), spike peak %d req/s",
		res.Sustained, planned, (1-PlannedFraction)*100, peak)
	for _, kind := range Kinds[1:] {
		ko := o
		ko.Kind = kind
		ko.Params = Params{Scale: o.Params.Scale, Rate: planned, Peak: peak}
		r, err := Run(ctx, ko, load)
		if err != nil {
			return res, err
		}
		res.Reports = append(res.Reports, r)
	}
	return res, os.WriteFile(filepath.Join(o.OutDir, "suite.md"), []byte(SuiteMarkdown(res)), 0o640)
}

// SuiteMarkdown renders one line per run plus the derived rates.
func SuiteMarkdown(s SuiteResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Suite\n\nStress sustained %.0f req/s within the SLO; planned load %d req/s (%.0f%% of it), spike peak %d req/s.\n\n",
		s.Sustained, s.PlannedRate, PlannedFraction*100, s.PeakRate)
	b.WriteString("| Run | Verdict | Detail |\n|---|---|---|\n")
	for _, r := range s.Reports {
		v := "PASS"
		if !r.Verdict.Pass {
			v = "FAIL"
		}
		fmt.Fprintf(&b, "| [%s](%s.md) | %s | %s |\n", r.Kind, r.Kind, v, headline(r))
	}
	b.WriteString("\n" + report.SharedMachineNote + "\n")
	return b.String()
}

// headline is a one-line result for a report.
func headline(r report.Report) string {
	switch {
	case r.Verdict.Break != nil:
		return r.Verdict.Break.Reason
	case r.Verdict.RecoveryS != nil:
		return fmt.Sprintf("recovered %.1f s after the peak", *r.Verdict.RecoveryS)
	case r.Verdict.Drift != nil:
		d := r.Verdict.Drift
		return fmt.Sprintf("heap x%.2f, goroutines x%.2f from first to last third", d.HeapRatio, d.GoroutinesRatio)
	}
	if f := r.Focus(); f != nil {
		return fmt.Sprintf("p95 %.1f ms, errors %.2f%% at %.0f req/s", f.Client.P95Ms, f.Client.FailedRate*100, f.Window.Rate)
	}
	return ""
}
