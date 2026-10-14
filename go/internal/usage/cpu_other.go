//go:build !windows && !linux

package usage

import "time"

// ProcessCPU is not implemented on this platform.
func ProcessCPU() (time.Duration, error) { return 0, ErrUnsupported }

// HostCPU is not implemented on this platform.
func HostCPU() (busy, total time.Duration, err error) { return 0, 0, ErrUnsupported }
