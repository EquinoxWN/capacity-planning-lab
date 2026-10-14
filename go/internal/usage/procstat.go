package usage

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// ErrUnsupported means the platform offers no CPU counter this package can read.
var ErrUnsupported = errors.New("cpu counters not supported on this platform")

// parseProcStat reads the aggregate "cpu" line of /proc/stat; hz is USER_HZ (ticks per second).
func parseProcStat(b []byte, hz int64) (busy, total time.Duration, err error) {
	line, _, _ := bytes.Cut(b, []byte("\n"))
	fields := bytes.Fields(line)
	if len(fields) < 5 || string(fields[0]) != "cpu" {
		return 0, 0, fmt.Errorf("/proc/stat: unexpected first line %q", line)
	}
	var ticks [10]int64
	for i, f := range fields[1:min(len(fields), 11)] {
		v, err := strconv.ParseInt(string(f), 10, 64)
		if err != nil || v < 0 {
			return 0, 0, fmt.Errorf("/proc/stat: bad field %q", f)
		}
		ticks[i] = v
	}
	// user nice system idle iowait irq softirq steal; guest time is already inside user.
	var sum int64
	for _, v := range ticks[:8] {
		sum += v
	}
	idle := ticks[3] + ticks[4]
	tick := time.Second / time.Duration(hz)
	return time.Duration(sum-idle) * tick, time.Duration(sum) * tick, nil
}
