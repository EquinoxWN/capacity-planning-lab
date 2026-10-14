package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// k6ThresholdsFailed is k6's exit code when a threshold failed; the verdict decides what that means.
const k6ThresholdsFailed = 99

// K6 runs the k6 binary with telemetry off and its config and cache kept in Home.
type K6 struct {
	Bin     string    // path to the k6 executable
	Scripts string    // directory holding load.js, stress.js, spike.js and soak.js
	Procs   int       // GOMAXPROCS for k6, to cap its CPU use
	Home    string    // directory used as k6's config and cache home
	Out     io.Writer // receives k6's console output as well as the log file
}

// overridden are environment variables k6 must not inherit, compared upper-case.
var overridden = []string{"GOMAXPROCS", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"}

// Env returns the environment for k6: inherited, minus K6_* and the overridden keys, plus ours.
func (k K6) Env(inherited []string) []string {
	out := make([]string, 0, len(inherited)+8)
	for _, kv := range inherited {
		key, _, _ := strings.Cut(kv, "=")
		up := strings.ToUpper(key)
		drop := strings.HasPrefix(up, "K6_")
		for _, o := range overridden {
			drop = drop || up == o
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return append(out,
		"K6_NO_USAGE_REPORT=true",
		"K6_AUTO_EXTENSION_RESOLUTION=false",
		"GOMAXPROCS="+strconv.Itoa(max(k.Procs, 1)),
		"APPDATA="+filepath.Join(k.Home, "config"),
		"LOCALAPPDATA="+filepath.Join(k.Home, "cache"),
		"XDG_CONFIG_HOME="+filepath.Join(k.Home, "config"),
		"XDG_CACHE_HOME="+filepath.Join(k.Home, "cache"),
	)
}

// Args returns the k6 command line for one test.
func (k K6) Args(kind, baseURL, summaryPath string, p Params) []string {
	args := []string{"run", "--no-usage-report", "--quiet", "-e", "BASE_URL=" + baseURL, "-e", "SUMMARY_PATH=" + summaryPath}
	env := p.Env()
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "-e", key+"="+env[key])
	}
	return append(args, filepath.Join(k.Scripts, kind+".js"))
}

// prepare creates the k6 home folders.
func (k K6) prepare() error {
	if k.Home == "" {
		return errors.New("k6 home directory is required")
	}
	for _, d := range []string{"config", "cache"} {
		if err := os.MkdirAll(filepath.Join(k.Home, d), 0o750); err != nil {
			return err
		}
	}
	return nil
}

// Version returns the first line of `k6 version`.
func (k K6) Version(ctx context.Context) (string, error) {
	if err := k.prepare(); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, k.Bin, "version")
	cmd.Env = k.Env(os.Environ())
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("k6 version: %w", err)
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	// "k6.exe v2.3.0 (commit/..., go1.26.8, windows/amd64)": drop the binary name.
	if i := strings.Index(line, " v"); i >= 0 {
		line = line[i+1:]
	}
	return line, nil
}

// Load runs one k6 test; it is a LoadFunc.
func (k K6) Load(ctx context.Context, kind, baseURL, summaryPath string, p Params) (ClientResult, error) {
	version, err := k.Version(ctx)
	if err != nil {
		return ClientResult{}, err
	}
	logf, err := os.Create(strings.TrimSuffix(summaryPath, ".json") + ".log")
	if err != nil {
		return ClientResult{}, err
	}
	defer logf.Close()
	var out io.Writer = logf
	if k.Out != nil {
		out = io.MultiWriter(logf, k.Out)
	}
	cmd := exec.CommandContext(ctx, k.Bin, k.Args(kind, baseURL, summaryPath, p)...)
	cmd.Env = k.Env(os.Environ())
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = 5 * time.Second
	runErr := cmd.Run()
	res := ClientResult{Version: version}
	if cmd.ProcessState != nil {
		res.CPUSeconds = (cmd.ProcessState.UserTime() + cmd.ProcessState.SystemTime()).Seconds()
	}
	var exit *exec.ExitError
	if runErr != nil && !(errors.As(runErr, &exit) && exit.ExitCode() == k6ThresholdsFailed) {
		return res, fmt.Errorf("k6 %s: %w", kind, runErr)
	}
	return res, nil
}
