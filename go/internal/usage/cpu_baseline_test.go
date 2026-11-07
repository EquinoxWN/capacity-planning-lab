package usage

import (
	"errors"
	"testing"
	"time"
)

// A failed CPU reading must not turn the next successful one into a delta over the wrong interval.
func TestCPUUtilNeedsTwoReadingsInARow(t *testing.T) {
	readings := []error{errors.New("counter unavailable"), nil, nil, errors.New("counter unavailable"), nil, nil}
	cpu := time.Duration(0)
	i := 0
	processCPU = func() (time.Duration, error) {
		err := readings[min(i, len(readings)-1)]
		i++
		cpu += 10 * time.Second // far more than any real interval: shows up if used as a delta
		return cpu, err
	}
	t.Cleanup(func() { processCPU = ProcessCPU })
	s := NewSampler(&fakeCutter{}, 1, 1) // reading 0 fails
	for range 5 {
		time.Sleep(5 * time.Millisecond)
		s.Tick() // readings 1 to 5
	}
	got, _ := s.Samples()
	if len(got) != 5 {
		t.Fatalf("%d samples", len(got))
	}
	want := []bool{false, true, false, false, true} // CPUUtil set only after two good readings in a row
	for k, smp := range got {
		if (smp.CPUUtil != 0) != want[k] {
			t.Errorf("sample %d: CPUUtil %.1f, want set=%v", k, smp.CPUUtil, want[k])
		}
	}
}
