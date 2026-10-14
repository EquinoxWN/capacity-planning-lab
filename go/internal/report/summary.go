// Package report reads the k6 summary, joins it with the server-side USE samples and
// decides what each run shows: SLO met or missed, breaking point, recovery and drift.
package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
)

// Schema is the only summary version this package accepts.
const Schema = "caplab.k6-summary.v1"

// maxSummaryBytes bounds how much of a summary file is read.
const maxSummaryBytes = 1 << 20

// SLO is the service-level objective a gated window must meet.
type SLO struct {
	P95Ms       float64 `json:"p95Ms"`
	ErrorRate   float64 `json:"errorRate"`
	DroppedRate float64 `json:"droppedRate"`
}

// Window is one k6 scenario on the test timeline.
type Window struct {
	Name   string  `json:"name"`
	Rate   float64 `json:"rate"`
	StartS float64 `json:"startS"`
	EndS   float64 `json:"endS"`
	Gate   bool    `json:"gate"`
}

// Latency holds client-side latency percentiles in milliseconds.
type Latency struct {
	P50Ms float64 `json:"p50Ms"`
	P95Ms float64 `json:"p95Ms"`
	P99Ms float64 `json:"p99Ms"`
	MaxMs float64 `json:"maxMs"`
}

// Totals are whole-run client numbers.
type Totals struct {
	Requests          float64 `json:"requests"`
	FailedRate        float64 `json:"failedRate"`
	DroppedIterations float64 `json:"droppedIterations"`
	ShedResponses     float64 `json:"shedResponses"`
	TransportErrors   float64 `json:"transportErrors"`
	VUsMax            float64 `json:"vusMax"`
	Latency
}

// Scenario is the client view of one window.
type Scenario struct {
	Name              string  `json:"name"`
	Iterations        float64 `json:"iterations"`
	FailedRate        float64 `json:"failedRate"`
	DroppedIterations float64 `json:"droppedIterations"`
	Latency
	ThresholdsOK bool `json:"thresholdsOk"`
}

// Summary is the versioned JSON written by js/k6/lib/summary.js.
type Summary struct {
	Schema            string     `json:"schema"`
	Kind              string     `json:"kind"`
	Scale             string     `json:"scale"`
	Label             string     `json:"label"`
	SLO               SLO        `json:"slo"`
	Windows           []Window   `json:"windows"`
	TestRunDurationMs int64      `json:"testRunDurationMs"`
	Totals            Totals     `json:"totals"`
	Scenarios         []Scenario `json:"scenarios"`
}

// windowName limits scenario names to what profiles.js produces.
var windowName = regexp.MustCompile(`^[a-z0-9]{1,16}$`)

// ParseSummary decodes and validates a summary; unknown fields are an error.
func ParseSummary(r io.Reader) (Summary, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxSummaryBytes+1))
	if err != nil {
		return Summary{}, err
	}
	if len(b) > maxSummaryBytes {
		return Summary{}, fmt.Errorf("summary larger than %d bytes", maxSummaryBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var s Summary
	if err := dec.Decode(&s); err != nil {
		return Summary{}, fmt.Errorf("summary: %w", err)
	}
	if dec.More() {
		return Summary{}, errors.New("summary: trailing data")
	}
	return s, s.Validate()
}

// nonNeg checks that every value is finite and not negative.
func nonNeg(vals map[string]float64) error {
	for k, v := range vals {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return fmt.Errorf("summary: %s = %v, want a finite value >= 0", k, v)
		}
	}
	return nil
}

// fraction checks that v lies in [0, 1].
func fraction(name string, v float64) error {
	if !(v >= 0 && v <= 1) {
		return fmt.Errorf("summary: %s = %v, want 0..1", name, v)
	}
	return nil
}

// Validate checks the schema, the timeline and every number.
func (s Summary) Validate() error {
	if s.Schema != Schema {
		return fmt.Errorf("summary schema %q, want %q", s.Schema, Schema)
	}
	switch s.Kind {
	case "load", "stress", "spike", "soak":
	default:
		return fmt.Errorf("summary kind %q unknown", s.Kind)
	}
	if s.Scale != "full" && s.Scale != "quick" {
		return fmt.Errorf("summary scale %q unknown", s.Scale)
	}
	if s.SLO.P95Ms <= 0 || s.SLO.ErrorRate <= 0 || s.SLO.ErrorRate >= 1 || s.SLO.DroppedRate <= 0 || s.SLO.DroppedRate >= 1 {
		return fmt.Errorf("summary SLO %+v out of range", s.SLO)
	}
	if len(s.Windows) == 0 || len(s.Windows) > 16 || len(s.Scenarios) != len(s.Windows) {
		return fmt.Errorf("summary has %d windows and %d scenarios", len(s.Windows), len(s.Scenarios))
	}
	end := 0.0
	seen := map[string]bool{}
	for i, w := range s.Windows {
		if !windowName.MatchString(w.Name) || seen[w.Name] {
			return fmt.Errorf("summary window %d name %q invalid or repeated", i, w.Name)
		}
		seen[w.Name] = true
		if w.Rate <= 0 || w.StartS < end || w.EndS <= w.StartS {
			return fmt.Errorf("summary window %q: rate %v, %v..%v s out of order", w.Name, w.Rate, w.StartS, w.EndS)
		}
		end = w.EndS
		sc := s.Scenarios[i]
		if sc.Name != w.Name {
			return fmt.Errorf("summary scenario %d is %q, window is %q", i, sc.Name, w.Name)
		}
		if err := nonNeg(map[string]float64{"iterations": sc.Iterations, "dropped": sc.DroppedIterations, "p50": sc.P50Ms, "p95": sc.P95Ms, "p99": sc.P99Ms, "max": sc.MaxMs}); err != nil {
			return err
		}
		if err := fraction(w.Name+" failedRate", sc.FailedRate); err != nil {
			return err
		}
	}
	t := s.Totals
	if err := nonNeg(map[string]float64{"requests": t.Requests, "dropped": t.DroppedIterations, "shed": t.ShedResponses, "transport": t.TransportErrors, "vusMax": t.VUsMax, "p50": t.P50Ms, "p95": t.P95Ms, "p99": t.P99Ms, "max": t.MaxMs}); err != nil {
		return err
	}
	if s.TestRunDurationMs < 0 {
		return errors.New("summary: negative test duration")
	}
	return fraction("totals failedRate", t.FailedRate)
}
