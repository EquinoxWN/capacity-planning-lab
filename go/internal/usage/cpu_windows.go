//go:build windows

package usage

import (
	"syscall"
	"time"
	"unsafe"
)

// procGetSystemTimes is kernel32's GetSystemTimes, which the syscall package does not wrap.
var procGetSystemTimes = syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemTimes")

// filetimeDuration converts a FILETIME interval (100 ns units) to a duration.
func filetimeDuration(ft syscall.Filetime) time.Duration {
	return time.Duration(uint64(ft.HighDateTime)<<32|uint64(ft.LowDateTime)) * 100
}

// ProcessCPU returns the user plus kernel CPU time used by this process.
func ProcessCPU() (time.Duration, error) {
	h, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0, err
	}
	var creation, exit, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return 0, err
	}
	return filetimeDuration(kernel) + filetimeDuration(user), nil
}

// HostCPU returns busy and total CPU time summed over all logical processors since boot.
func HostCPU() (busy, total time.Duration, err error) {
	var idle, kernel, user syscall.Filetime
	r, _, callErr := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if r == 0 {
		return 0, 0, callErr
	}
	// Kernel time includes idle time on Windows.
	total = filetimeDuration(kernel) + filetimeDuration(user)
	return total - filetimeDuration(idle), total, nil
}
