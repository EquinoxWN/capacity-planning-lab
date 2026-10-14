package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/EquinoxWN/capacity-planning-lab/go/internal/report"
	"github.com/EquinoxWN/capacity-planning-lab/go/internal/service"
)

// fakeWindows are short windows per kind so a run takes about a second.
var fakeWindows = map[string][]report.Window{
	"load":   {{Name: "warmup", Rate: 50, StartS: 0, EndS: 0.4}, {Name: "load", Rate: 50, StartS: 0.4, EndS: 1.2, Gate: true}},
	"soak":   {{Name: "warmup", Rate: 50, StartS: 0, EndS: 0.3}, {Name: "soak", Rate: 50, StartS: 0.3, EndS: 1.5, Gate: true}},
	"spike":  {{Name: "before", Rate: 50, StartS: 0, EndS: 0.4, Gate: true}, {Name: "spike", Rate: 100, StartS: 0.4, EndS: 0.6}, {Name: "after", Rate: 50, StartS: 0.6, EndS: 1.4, Gate: true}},
	"stress": {{Name: "step01", Rate: 100, StartS: 0, EndS: 0.4}, {Name: "step02", Rate: 200, StartS: 0.4, EndS: 0.8}, {Name: "step03", Rate: 300, StartS: 0.8, EndS: 1.2}, {Name: "step04", Rate: 400, StartS: 1.2, EndS: 1.6}},
}

// fakeLoad sends real requests to the service for the planned time and writes a k6-style summary.
// Stress steps from the third on report an SLO miss, so the suite finds a breaking point at 200 req/s.
func fakeLoad(t *testing.T, calls *[]Params) LoadFunc {
	return func(ctx context.Context, kind, base, summaryPath string, p Params) (ClientResult, error) {
		*calls = append(*calls, p)
		ws := fakeWindows[kind]
		end := time.Now().Add(time.Duration(ws[len(ws)-1].EndS * float64(time.Second)))
		n := 0
		for time.Now().Before(end) {
			res, err := http.Get(base + "/work")
			if err != nil {
				return ClientResult{}, err
			}
			_, _ = io.Copy(io.Discard, res.Body)
			res.Body.Close()
			n++
			time.Sleep(5 * time.Millisecond)
		}
		sum := report.Summary{Schema: report.Schema, Kind: kind, Scale: "quick", Label: "fake " + kind,
			SLO: report.SLO{P95Ms: 50, ErrorRate: 0.01, DroppedRate: 0.01}, Windows: ws,
			Totals: report.Totals{Requests: float64(n)}}
		for i, w := range ws {
			ok := !(kind == "stress" && i >= 2)
			sc := report.Scenario{Name: w.Name, Iterations: float64(n / len(ws)), ThresholdsOK: ok}
			if !ok {
				sc.P95Ms = 80
			}
			sum.Scenarios = append(sum.Scenarios, sc)
		}
		b, _ := json.Marshal(sum)
		return ClientResult{CPUSeconds: 0.01, Version: "fake"}, os.WriteFile(summaryPath, b, 0o600)
	}
}

// opts returns fast options writing into a temp dir.
func opts(t *testing.T) Options {
	return Options{Kind: "load", Params: Params{Scale: "quick"}, Service: service.Config{Workers: 2, QueueLimit: 4, MaxWait: 50 * time.Millisecond, WorkIters: 10, WorkKB: 1},
		Addr: "127.0.0.1:0", OutDir: t.TempDir(), ClientProcs: 1, Interval: 100 * time.Millisecond}
}

func TestRunJoinsClientAndServerViews(t *testing.T) {
	var calls []Params
	o := opts(t)
	r, err := Run(context.Background(), o, fakeLoad(t, &calls))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Verdict.Pass || len(r.Windows) != 2 || !r.Context.ServiceAlive {
		t.Fatalf("verdict %+v", r.Verdict)
	}
	lw := r.Windows[1]
	if !lw.ServerOK || lw.Server.OK == 0 || lw.Server.CPUUtilAvg < 0 {
		t.Fatalf("load window not sampled on the server: %+v", lw.Server)
	}
	if r.Env.Procs < 1 || r.Env.K6Version != "fake" || len(r.Samples) < 5 {
		t.Fatalf("env %+v, %d samples", r.Env, len(r.Samples))
	}
	for _, f := range []string{"load.json", "load.md", "load.k6.json"} {
		if _, err := os.Stat(filepath.Join(o.OutDir, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
}

func TestRunRefusesBadInput(t *testing.T) {
	var calls []Params
	o := opts(t)
	o.Addr = "0.0.0.0:0"
	if _, err := Run(context.Background(), o, fakeLoad(t, &calls)); err == nil || !strings.Contains(err.Error(), "not loopback") {
		t.Fatalf("non-loopback address accepted: %v", err)
	}
	o = opts(t)
	o.Kind = "chaos"
	if _, err := Run(context.Background(), o, fakeLoad(t, &calls)); err == nil {
		t.Fatal("unknown kind accepted")
	}
	o = opts(t)
	wrongKind := func(ctx context.Context, kind, base, path string, p Params) (ClientResult, error) {
		return fakeLoad(t, &calls)(ctx, "soak", base, path, p)
	}
	if _, err := Run(context.Background(), o, wrongKind); err == nil || !strings.Contains(err.Error(), "expected \"load\"") {
		t.Fatalf("summary of another kind accepted: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("load called %d times, want 1 (only the last case reaches it)", len(calls))
	}
}

func TestSuiteDerivesRatesFromTheStressResult(t *testing.T) {
	var calls []Params
	o := opts(t)
	res, err := Suite(context.Background(), o, fakeLoad(t, &calls))
	if err != nil {
		t.Fatal(err)
	}
	if res.Sustained != 200 || res.PlannedRate != 120 || res.PeakRate != 400 || len(res.Reports) != 4 {
		t.Fatalf("suite %+v", res)
	}
	for _, p := range calls[1:] {
		if p.Rate != 120 || p.Peak != 400 {
			t.Fatalf("follow-up run got %+v, want rate 120 peak 400", p)
		}
	}
	md, err := os.ReadFile(filepath.Join(o.OutDir, "suite.md"))
	if err != nil || !strings.Contains(string(md), "planned load 120 req/s") {
		t.Fatalf("suite.md: %v\n%s", err, md)
	}
}

func TestPlanNeedsAConfirmedBreak(t *testing.T) {
	if _, _, err := Plan(report.Break{Found: true, Confirmed: false, SustainedRate: 100}); err == nil {
		t.Fatal("unconfirmed break accepted")
	}
	if _, _, err := Plan(report.Break{Found: false, ClientLimited: true, Confirmed: true, SustainedRate: 100}); err == nil {
		t.Fatal("client-limited result accepted")
	}
	p, k, err := Plan(report.Break{Found: true, Confirmed: true, SustainedRate: 1000})
	if err != nil || p != 600 || k != 2000 {
		t.Fatalf("plan %d %d %v", p, k, err)
	}
}

func TestK6CommandIsContained(t *testing.T) {
	k := K6{Bin: "k6", Scripts: "js/k6", Procs: 3, Home: filepath.Join("x", "home")}
	env := k.Env([]string{"PATH=/bin", "K6_CLOUD_TOKEN=secret", "k6_no_usage_report=false", "GOMAXPROCS=20", "AppData=C:/Users/me"})
	for _, banned := range []string{"K6_CLOUD_TOKEN=secret", "k6_no_usage_report=false", "GOMAXPROCS=20", "AppData=C:/Users/me"} {
		if slices.Contains(env, banned) {
			t.Errorf("inherited %q", banned)
		}
	}
	for _, want := range []string{"PATH=/bin", "K6_NO_USAGE_REPORT=true", "K6_AUTO_EXTENSION_RESOLUTION=false", "GOMAXPROCS=3", "APPDATA=" + filepath.Join("x", "home", "config")} {
		if !slices.Contains(env, want) {
			t.Errorf("missing %q in %v", want, env)
		}
	}
	args := k.Args("stress", "http://127.0.0.1:1", "out/s.json", Params{Scale: "quick", Rate: 10, Steps: []int{1, 2}})
	got := strings.Join(args, " ")
	want := fmt.Sprintf("run --no-usage-report --quiet -e BASE_URL=http://127.0.0.1:1 -e SUMMARY_PATH=out/s.json -e RATE=10 -e SCALE=quick -e STEPS=1,2 %s", filepath.Join("js", "k6", "stress.js"))
	if got != want {
		t.Fatalf("args\n got %s\nwant %s", got, want)
	}
}

func TestRejudgeRewritesStoredReports(t *testing.T) {
	var calls []Params
	o := opts(t)
	first, err := Run(context.Background(), o, fakeLoad(t, &calls))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(o.OutDir, "load.md")); err != nil {
		t.Fatal(err)
	}
	got, err := Rejudge(o.OutDir)
	if err != nil || len(got) != 1 {
		t.Fatalf("rejudge: %v (%d reports)", err, len(got))
	}
	if got[0].Verdict.Pass != first.Verdict.Pass || len(got[0].Verdict.Checks) != len(first.Verdict.Checks) {
		t.Fatalf("verdict changed without a rule change: %+v vs %+v", got[0].Verdict, first.Verdict)
	}
	if _, err := os.Stat(filepath.Join(o.OutDir, "load.md")); err != nil {
		t.Fatal("load.md not rewritten")
	}
	if _, err := Rejudge(t.TempDir()); err == nil {
		t.Fatal("an empty directory must be an error")
	}
}
