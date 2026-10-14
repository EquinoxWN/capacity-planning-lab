//go:build linux

package usage

import (
	"os"
	"syscall"
	"time"
)

// ProcessCPU returns the user plus system CPU time used by this process.
func ProcessCPU() (time.Duration, error) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, err
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano()), nil
}

// HostCPU returns busy and total CPU time summed over all logical processors since boot.
func HostCPU() (busy, total time.Duration, err error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, err := f.Read(buf)
	if err != nil {
		return 0, 0, err
	}
	return parseProcStat(buf[:n], 100)
}
