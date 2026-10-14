// Command caplab serves the sample service, or runs k6 load, stress, spike and soak tests
// against it while sampling USE signals, and writes the results.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/EquinoxWN/capacity-planning-lab/go/internal/report"
	"github.com/EquinoxWN/capacity-planning-lab/go/internal/runner"
	"github.com/EquinoxWN/capacity-planning-lab/go/internal/service"
)

const usageText = `usage:
  caplab serve [flags]                       run the sample service until Ctrl+C
  caplab run   [flags] load|stress|spike|soak run one k6 test and write <out>/<kind>.{json,md}
  caplab suite [flags]                       stress first, then load, spike and soak at rates derived from it
  caplab rejudge [-out dir] [-annotate]      re-apply the current verdict rules to stored reports
Run "caplab <command> -h" for the flags.`

// main dispatches the subcommand and maps outcomes to exit codes.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// common holds the flags every subcommand shares.
type common struct {
	procs, memMiB int
	cfg           service.Config
}

// register adds the shared flags to fs.
func (c *common) register(fs *flag.FlagSet) {
	d := service.DefaultConfig()
	fs.IntVar(&c.procs, "procs", 2, "GOMAXPROCS for the service (CPU cap)")
	fs.IntVar(&c.memMiB, "mem-mib", 256, "soft memory limit for the service in MiB (GOMEMLIMIT)")
	fs.IntVar(&c.cfg.Workers, "workers", d.Workers, "requests served at once")
	fs.IntVar(&c.cfg.QueueLimit, "queue", d.QueueLimit, "requests allowed to wait for a worker")
	fs.DurationVar(&c.cfg.MaxWait, "max-wait", d.MaxWait, "longest wait before a request is shed with 503")
	fs.IntVar(&c.cfg.WorkIters, "work-iters", d.WorkIters, "SHA-256 rounds per request")
	fs.IntVar(&c.cfg.WorkKB, "work-kb", d.WorkKB, "KiB allocated per request")
}

// apply caps this process's CPUs and memory.
func (c *common) apply() error {
	if c.procs < 1 || c.procs > runtime.NumCPU() {
		return fmt.Errorf("-procs must be 1..%d", runtime.NumCPU())
	}
	if c.memMiB < 32 || c.memMiB > 4096 {
		return errors.New("-mem-mib must be 32..4096")
	}
	runtime.GOMAXPROCS(c.procs)
	debug.SetMemoryLimit(int64(c.memMiB) << 20)
	return c.cfg.Validate()
}

// run executes the command line and returns the exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usageText)
		return 2
	}
	var err error
	pass := true
	switch args[0] {
	case "serve":
		err = serve(ctx, args[1:], stderr)
	case "run", "suite":
		pass, err = test(ctx, args[0], args[1:], stdout, stderr)
	case "rejudge":
		pass, err = rejudge(args[1:], stdout, stderr)
	default:
		fmt.Fprintln(stderr, usageText)
		return 2
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "caplab:", err)
		return 2
	}
	if !pass {
		return 1
	}
	return 0
}

// serve runs the service on a loopback address until ctx ends.
func serve(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var c common
	c.register(fs)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address (loopback only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := c.apply(); err != nil {
		return err
	}
	svc, err := service.New(c.cfg, time.Now)
	if err != nil {
		return err
	}
	ln, err := service.Listen(*addr)
	if err != nil {
		return err
	}
	srv := service.NewServer(svc.Handler())
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	fmt.Fprintf(stderr, "caplab: serving on http://%s (GOMAXPROCS=%d, memory limit %d MiB); Ctrl+C stops\n", ln.Addr(), c.procs, c.memMiB)
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// parseSteps reads a comma-separated list of rates.
func parseSteps(s string) ([]int, error) {
	if s == "" {
		return nil, nil
	}
	var out []int
	for _, p := range strings.Split(s, ",") {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || v < 1 {
			return nil, fmt.Errorf("-steps: bad rate %q", p)
		}
		out = append(out, v)
	}
	return out, nil
}

// test runs one k6 test or the suite and reports whether every check passed.
func test(ctx context.Context, cmd string, args []string, stdout, stderr io.Writer) (bool, error) {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var c common
	c.register(fs)
	k6 := runner.K6{Out: stdout}
	fs.StringVar(&k6.Bin, "k6", "k6", "path to the k6 binary")
	fs.StringVar(&k6.Scripts, "scripts", "js/k6", "directory with the k6 scripts")
	fs.StringVar(&k6.Home, "k6-home", ".tmp/k6home", "directory k6 uses for config and cache")
	fs.IntVar(&k6.Procs, "client-procs", 4, "GOMAXPROCS for k6 (caps the load generator)")
	out := fs.String("out", ".tmp/runs", "directory for the reports")
	var p runner.Params
	fs.StringVar(&p.Scale, "scale", "full", "full, or quick for CI")
	fs.IntVar(&p.Rate, "rate", 0, "planned rate in req/s for load, soak and the spike baseline (0 = script default)")
	fs.IntVar(&p.Peak, "peak", 0, "spike peak in req/s (0 = twice the rate)")
	fs.IntVar(&p.HoldS, "hold-s", 0, "main hold in seconds (0 = script default)")
	steps := fs.String("steps", "", "stress step rates, comma separated (empty = script default)")
	annotate := fs.Bool("annotate", false, "print GitHub Actions ::notice (pass) / ::warning (fail) lines")
	if err := fs.Parse(args); err != nil {
		return false, err
	}
	var err error
	if p.Steps, err = parseSteps(*steps); err != nil {
		return false, err
	}
	if err := c.apply(); err != nil {
		return false, err
	}
	o := runner.Options{
		Params: p, Service: c.cfg, Addr: "127.0.0.1:0", OutDir: *out, ClientProcs: k6.Procs,
		Log: func(f string, a ...any) { fmt.Fprintf(stderr, "caplab: "+f+"\n", a...) },
	}
	var reports []report.Report
	if cmd == "suite" {
		if fs.NArg() != 0 {
			return false, errors.New("suite takes no arguments")
		}
		res, err := runner.Suite(ctx, o, k6.Load)
		reports = res.Reports
		if err != nil {
			printVerdicts(stdout, reports, *annotate)
			return false, err
		}
	} else {
		if fs.NArg() != 1 {
			return false, errors.New("run needs exactly one kind: load, stress, spike or soak")
		}
		o.Kind = fs.Arg(0)
		r, err := runner.Run(ctx, o, k6.Load)
		if err != nil {
			return false, err
		}
		reports = []report.Report{r}
	}
	return printVerdicts(stdout, reports, *annotate), nil
}

// rejudge rewrites stored reports with the current verdict rules.
func rejudge(args []string, stdout, stderr io.Writer) (bool, error) {
	fs := flag.NewFlagSet("rejudge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", ".tmp/runs", "directory holding <kind>.json reports")
	annotate := fs.Bool("annotate", false, "print GitHub Actions ::notice (pass) / ::warning (fail) lines")
	if err := fs.Parse(args); err != nil {
		return false, err
	}
	reports, err := runner.Rejudge(*out)
	if err != nil {
		return false, err
	}
	return printVerdicts(stdout, reports, *annotate), nil
}

// printVerdicts prints one line per report and returns whether all passed.
func printVerdicts(w io.Writer, reports []report.Report, annotate bool) bool {
	all := true
	for _, r := range reports {
		line := fmt.Sprintf("%s: %s", r.Kind, verdictWord(r.Verdict.Pass))
		for _, c := range r.Verdict.Checks {
			if !c.Pass {
				line += fmt.Sprintf("; failed %q (%s)", c.Name, c.Detail)
			}
		}
		if b := r.Verdict.Break; b != nil {
			line += fmt.Sprintf("; sustained %.0f req/s, %s", b.SustainedRate, b.Reason)
		}
		if f := r.Focus(); f != nil {
			s := f.Server
			line += fmt.Sprintf("; USE at %s: CPU %.0f%%, workers %.0f%%, queue max %d, shed %.1f%%", f.Window.Name, s.CPUUtilAvg*100, s.WorkerUtilAvg*100, s.QueueMax, s.ShedRate*100)
		}
		fmt.Fprintln(w, line)
		if annotate {
			level := "notice"
			if !r.Verdict.Pass {
				level = "warning"
			}
			fmt.Fprintf(w, "::%s title=caplab %s::%s\n", level, r.Kind, strings.ReplaceAll(line, "\n", " "))
		}
		all = all && r.Verdict.Pass
	}
	return all
}

// verdictWord spells a verdict.
func verdictWord(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
}
