package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/EquinoxWN/capacity-planning-lab/go/internal/report"
)

// maxReportBytes bounds how much of a stored report is read.
const maxReportBytes = 64 << 20

// Rejudge re-applies the current verdict rules to the reports stored in dir and rewrites them,
// so an analysis fix never needs the load to be run again. suite.md is rewritten when all four exist.
func Rejudge(dir string) ([]report.Report, error) {
	var out []report.Report
	for _, kind := range Kinds {
		r, err := readReport(filepath.Join(dir, kind+".json"))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return out, err
		}
		if r.Kind != kind {
			return out, fmt.Errorf("%s.json holds a %q report", kind, r.Kind)
		}
		report.Rejudge(&r)
		if err := write(dir, r); err != nil {
			return out, err
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no reports in %s", dir)
	}
	if len(out) == len(Kinds) && out[0].Verdict.Break != nil {
		s := SuiteResult{Reports: out, Sustained: out[0].Verdict.Break.SustainedRate}
		if f := out[1].Focus(); f != nil {
			s.PlannedRate = int(f.Window.Rate)
		}
		if f := out[2].Focus(); f != nil {
			s.PeakRate = int(f.Window.Rate)
		}
		return out, os.WriteFile(filepath.Join(dir, "suite.md"), []byte(SuiteMarkdown(s)), 0o640)
	}
	return out, nil
}

// readReport decodes one stored report, refusing oversized files.
func readReport(path string) (report.Report, error) {
	f, err := os.Open(path)
	if err != nil {
		return report.Report{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxReportBytes+1))
	if err != nil {
		return report.Report{}, err
	}
	if len(b) > maxReportBytes {
		return report.Report{}, fmt.Errorf("%s is larger than %d bytes", path, maxReportBytes)
	}
	var r report.Report
	if err := json.Unmarshal(b, &r); err != nil {
		return report.Report{}, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}
