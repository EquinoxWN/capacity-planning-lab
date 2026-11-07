package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary stand in for k6: with CAPLAB_FAKE_K6=hang it answers
// `version` and then never finishes a run.
func TestMain(m *testing.M) {
	if os.Getenv("CAPLAB_FAKE_K6") == "hang" {
		if len(os.Args) > 1 && os.Args[1] == "version" {
			os.Stdout.WriteString("k6 v0.0.0-fake\n")
			os.Exit(0)
		}
		time.Sleep(10 * time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestK6RunIsKilledAtItsTimeout(t *testing.T) {
	t.Setenv("CAPLAB_FAKE_K6", "hang")
	dir := t.TempDir()
	k := K6{Bin: os.Args[0], Scripts: dir, Procs: 1, Home: filepath.Join(dir, "home"), Timeout: time.Second}
	start := time.Now()
	_, err := k.Load(context.Background(), "load", "http://127.0.0.1:1", filepath.Join(dir, "load.k6.json"), Params{Scale: "quick"})
	if err == nil || !strings.Contains(err.Error(), "did not finish within 1s") {
		t.Fatalf("err %v", err)
	}
	if took := time.Since(start); took > 30*time.Second {
		t.Fatalf("the hung k6 was stopped only after %s", took)
	}
}

func TestReportsAreReplacedWholeWithNoTemporaryFilesLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "load.json")
	for _, body := range []string{"first, longer content\n", "second\n"} {
		if err := writeFileAtomic(path, []byte(body)); err != nil {
			t.Fatal(err)
		}
		if b, err := os.ReadFile(path); err != nil || string(b) != body {
			t.Fatalf("read %q %v, want %q", b, err, body)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries %v %v: a temporary file was left", entries, err)
	}
	if err := writeFileAtomic(filepath.Join(dir, "missing", "x.json"), []byte("x")); err == nil {
		t.Fatal("a write into a missing folder must fail")
	}
}

func TestRunFailsWhenAStaleSummaryCannotBeRemoved(t *testing.T) {
	var calls []Params
	o := opts(t)
	// A non-empty directory where the summary goes cannot be removed.
	if err := os.MkdirAll(filepath.Join(o.OutDir, "load.k6.json", "x"), 0o750); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), o, fakeLoad(t, &calls))
	if err == nil || !strings.Contains(err.Error(), "remove stale k6 summary") {
		t.Fatalf("err %v", err)
	}
	if len(calls) != 0 {
		t.Fatal("load ran although the stale summary could still be read")
	}
}
