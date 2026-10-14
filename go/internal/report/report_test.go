package report

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/EquinoxWN/capacity-planning-lab/go/internal/usage"
)

// contractPath is the summary k6 wrote in a real run; js/test/summary.test.js checks the same file.
const contractPath = "../../../testdata/contract/summary-v1.json"

// loadContract parses the shared contract fixture.
func loadContract(t *testing.T) (Summary, []byte) {
	t.Helper()
	b, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseSummary(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return s, b
}

func TestParsesTheContractFixture(t *testing.T) {
	s, _ := loadContract(t)
	if s.Kind != "stress" || len(s.Windows) != 5 || s.Totals.Requests != 15504 || s.Totals.TransportErrors != 78 {
		t.Fatalf("unexpected summary: %+v", s)
	}
	if s.Scenarios[0].ThresholdsOK || !s.Scenarios[4].ThresholdsOK || s.Scenarios[0].FailedRate != 0.156 {
		t.Fatalf("scenario verdicts not carried over: %+v", s.Scenarios)
	}
}

func TestRejectsBrokenSummaries(t *testing.T) {
	_, good := loadContract(t)
	mutate := func(f func(m map[string]any)) []byte {
		var m map[string]any
		if err := json.Unmarshal(good, &m); err != nil {
			t.Fatal(err)
		}
		f(m)
		b, _ := json.Marshal(m)
		return b
	}
	win := func(m map[string]any, i int) map[string]any { return m["windows"].([]any)[i].(map[string]any) }
	cases := map[string][]byte{
		"wrong schema":      mutate(func(m map[string]any) { m["schema"] = "caplab.k6-summary.v0" }),
		"unknown field":     mutate(func(m map[string]any) { m["extra"] = 1 }),
		"unknown kind":      mutate(func(m map[string]any) { m["kind"] = "chaos" }),
		"windows overlap":   mutate(func(m map[string]any) { win(m, 1)["startS"] = 1.0 }),
		"empty window":      mutate(func(m map[string]any) { win(m, 2)["endS"] = win(m, 2)["startS"] }),
		"bad window name":   mutate(func(m map[string]any) { win(m, 0)["name"] = "../x" }),
		"name mismatch":     mutate(func(m map[string]any) { m["scenarios"].([]any)[0].(map[string]any)["name"] = "other" }),
		"rate above one":    mutate(func(m map[string]any) { m["totals"].(map[string]any)["failedRate"] = 1.5 }),
		"negative latency":  mutate(func(m map[string]any) { m["scenarios"].([]any)[1].(map[string]any)["p95Ms"] = -1 }),
		"missing scenarios": mutate(func(m map[string]any) { m["scenarios"] = []any{} }),
		"bad slo":           mutate(func(m map[string]any) { m["slo"].(map[string]any)["errorRate"] = 0 }),
		"trailing data":     append(append([]byte{}, good...), []byte("{}")...),
		"oversized":         append(append([]byte{}, good...), bytes.Repeat([]byte(" "), maxSummaryBytes)...),
	}
	for name, b := range cases {
		if _, err := ParseSummary(bytes.NewReader(b)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// step builds a stress window result with the given verdict.
func step(i int, rate float64, meets, clientLimited bool) WindowResult {
	name := "step0" + string(rune('1'+i))
	w := WindowResult{Window: Window{Name: name, Rate: rate, StartS: float64(i) * 10, EndS: float64(i+1) * 10}, MeetsSLO: meets, ClientLimited: clientLimited}
	if !meets {
		w.Misses = []string{"p95 80.0 ms > 50 ms"}
	}
	return w
}

func TestBreakingPoint(t *testing.T) {
	cases := []struct {
		name      string
		ws        []WindowResult
		found     bool
		confirmed bool
		client    bool
		sustained float64
		firstMiss float64
		transient int
	}{
		{"clean knee", []WindowResult{step(0, 100, true, false), step(1, 200, true, false), step(2, 300, false, false), step(3, 400, false, false)}, true, true, false, 200, 300, 0},
		{"noise before the knee", []WindowResult{step(0, 100, true, false), step(1, 200, false, false), step(2, 300, true, false), step(3, 400, false, false), step(4, 500, false, false)}, true, true, false, 300, 400, 1},
		{"only the last step misses", []WindowResult{step(0, 100, true, false), step(1, 200, false, false)}, true, false, false, 100, 200, 0},
		{"never misses", []WindowResult{step(0, 100, true, false), step(1, 200, true, false)}, false, false, false, 200, 0, 0},
		{"client limited", []WindowResult{step(0, 100, true, false), step(1, 200, false, true), step(2, 300, false, true)}, false, true, true, 100, 200, 0},
		{"first step misses", []WindowResult{step(0, 100, false, false), step(1, 200, false, false)}, true, true, false, 0, 100, 0},
	}
	for _, c := range cases {
		b := breakingPoint(c.ws)
		if b.Found != c.found || b.Confirmed != c.confirmed || b.ClientLimited != c.client || b.SustainedRate != c.sustained || b.FirstMissRate != c.firstMiss || len(b.Transient) != c.transient {
			t.Errorf("%s: got %+v", c.name, b)
		}
		if b.Reason == "" {
			t.Errorf("%s: no reason given", c.name)
		}
	}
}

func TestStressVerdictNeedsAConfirmedBreak(t *testing.T) {
	s := Summary{Kind: "stress"}
	ctx := Context{ServiceAlive: true, ClientUtil: 0.1}
	ok := Judge(s, []WindowResult{step(0, 100, true, false), step(1, 200, false, false), step(2, 300, false, false)}, nil, ctx)
	if !ok.Pass || ok.Break == nil || ok.Break.SustainedRate != 100 {
		t.Fatalf("confirmed break should pass: %+v", ok)
	}
	unconfirmed := Judge(s, []WindowResult{step(0, 100, true, false), step(1, 200, false, false)}, nil, ctx)
	if unconfirmed.Pass {
		t.Fatal("a break seen only on the last step must not pass")
	}
	busyClient := Judge(s, []WindowResult{step(0, 100, true, false), step(1, 200, false, false), step(2, 300, false, false)}, nil, Context{ServiceAlive: true, ClientUtil: 0.95})
	if busyClient.Pass {
		t.Fatal("a load generator above 80% of its CPU cap must fail the run")
	}
}

func TestWindowsFlagsClientLimitedDrops(t *testing.T) {
	slo := SLO{P95Ms: 50, ErrorRate: 0.01, DroppedRate: 0.01}
	s := Summary{Kind: "stress", SLO: slo,
		Windows:   []Window{{Name: "step01", Rate: 100, StartS: 0, EndS: 10}},
		Scenarios: []Scenario{{Name: "step01", Iterations: 900, DroppedIterations: 100, ThresholdsOK: false}}}
	samples := []usage.Sample{{T: 5, Dur: 1, OK: 100}, {T: 6, Dur: 1, OK: 100}}
	ws := Windows(s, samples, Context{})
	if len(ws) != 1 || ws[0].MeetsSLO || !ws[0].ClientLimited || !ws[0].ServerOK {
		t.Fatalf("got %+v", ws)
	}
	samples[0].ShedQueueFull = 5 // the service was shedding: the drops are the service's fault
	if ws := Windows(s, samples, Context{}); ws[0].ClientLimited {
		t.Fatal("drops while the service sheds are not client-limited")
	}
}

func TestRecovery(t *testing.T) {
	var samples []usage.Sample
	for i := 1; i <= 20; i++ {
		s := usage.Sample{T: float64(i), Dur: 1, P95Ms: 5}
		if i > 10 && i <= 13 { // still shedding for 3 s after the peak ends at t=10
			s.ShedQueueFull = 10
		}
		samples = append(samples, s)
	}
	rec, ok := recovery(samples, 10, 50)
	if !ok || rec != 3 {
		t.Fatalf("recovery %v %v, want 3 s", rec, ok)
	}
	if _, ok := recovery(samples[:14], 10, 50); ok {
		t.Fatal("one healthy sample is not a recovery")
	}
}

func TestDrift(t *testing.T) {
	s := Summary{Kind: "soak", Windows: []Window{{Name: "soak", Rate: 10, StartS: 0, EndS: 31}}}
	var flat, growing []usage.Sample
	for i := 1; i <= 31; i++ {
		flat = append(flat, usage.Sample{T: float64(i), Dur: 1, HeapBytes: 1000, Goroutines: 10})
		growing = append(growing, usage.Sample{T: float64(i), Dur: 1, HeapBytes: uint64(1000 * i), Goroutines: 10})
	}
	d, ok := drift(s, flat, Context{})
	if !ok || d.HeapRatio != 1 || d.GoroutinesRatio != 1 {
		t.Fatalf("flat drift %+v %v", d, ok)
	}
	v := Judge(s, nil, growing, Context{ServiceAlive: true})
	if v.Pass || v.Drift == nil || v.Drift.HeapRatio <= MaxDriftRatio {
		t.Fatalf("a growing heap must fail the soak: %+v", v)
	}
}

func TestMarkdownStatesTheSetupAndUSE(t *testing.T) {
	s, _ := loadContract(t)
	ws := Windows(s, nil, Context{})
	r := Report{Kind: "stress", Label: s.Label, SLO: s.SLO, Windows: ws, Client: s.Totals,
		Env:     Env{Procs: 2, MemLimitBytes: 256 << 20, ClientProcs: 4, LogicalCPUs: 20},
		Service: ServiceCfg{Workers: 4, QueueLimit: 64, MaxWaitMs: 100},
		Context: Context{ServiceAlive: true}}
	r.Verdict = Judge(s, ws, nil, r.Context)
	md := Markdown(r)
	for _, want := range []string{"# stress run:", SharedMachineNote, "bound to 127.0.0.1", "## USE method", "| Worker pool (4 workers) |", "Breaking point:", "transient misses at step01"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q", want)
		}
	}
}

func TestMissesNameHostStalls(t *testing.T) {
	slo := SLO{P95Ms: 50, ErrorRate: 0.01, DroppedRate: 0.01}
	s := Summary{Kind: "load", SLO: slo,
		Windows:   []Window{{Name: "load", Rate: 100, StartS: 0, EndS: 10, Gate: true}},
		Scenarios: []Scenario{{Name: "load", Iterations: 1000, Latency: Latency{P95Ms: 90}}}}
	samples := []usage.Sample{{T: 5, Dur: 1, SchedP99Ms: 300, CPUUtil: 0.2}, {T: 6, Dur: 1, SchedP99Ms: 1, CPUUtil: 0.5}}
	ws := Windows(s, samples, Context{})
	if ws[0].MeetsSLO || len(ws[0].Misses) != 2 || !strings.Contains(ws[0].Misses[1], "host stall: 1 s") {
		t.Fatalf("misses %v", ws[0].Misses)
	}
}

func TestSpikeNeedsFastAnswersOnTheClientToo(t *testing.T) {
	s := Summary{Kind: "spike", SLO: SLO{P95Ms: 50, ErrorRate: 0.01, DroppedRate: 0.01}, Totals: Totals{TransportErrors: 900}}
	spike := WindowResult{Window: Window{Name: "spike", Rate: 3200, StartS: 0, EndS: 10},
		Client: Scenario{Name: "spike", Latency: Latency{P99Ms: 2000}}, Server: usage.Window{ShedP99Ms: 100}}
	v := Judge(s, []WindowResult{spike}, nil, Context{ServiceAlive: true, MaxWaitMs: 100})
	for _, c := range v.Checks {
		if strings.HasPrefix(c.Name, "overload is refused fast") {
			if c.Pass || !strings.Contains(c.Detail, "900 client transport errors") {
				t.Fatalf("client-side timeouts must fail the check: %+v", c)
			}
			return
		}
	}
	t.Fatal("no fast-refusal check")
}

func TestSaturatedServiceIsNotClientLimited(t *testing.T) {
	slo := SLO{P95Ms: 50, ErrorRate: 0.01, DroppedRate: 0.01}
	r := WindowResult{Window: Window{Name: "step03", Rate: 800, StartS: 0, EndS: 5},
		Client: Scenario{Name: "step03", Iterations: 3000, DroppedIterations: 990, Latency: Latency{P95Ms: 1376}},
		Server: usage.Window{P95Ms: 3, CPUUtilAvg: 0.91, SchedP99Ms: 1610}, ServerOK: true}
	classify(&r, slo)
	if r.ClientLimited || r.MeetsSLO {
		t.Fatalf("a CPU-saturated service (scheduler p99 1.6 s) was blamed on the client: %+v", r)
	}
	r.Server = usage.Window{P95Ms: 3, CPUUtilAvg: 0.4, SchedP99Ms: 2}
	classify(&r, slo)
	if !r.ClientLimited {
		t.Fatal("drops with an idle, healthy service are client-limited")
	}
}
