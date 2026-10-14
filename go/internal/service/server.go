package service

import (
	"fmt"
	"net"
	"net/http"
	"time"
)

// CheckLoopback refuses any listen address that is not on the loopback interface.
func CheckLoopback(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("listen address %q: %w", addr, err)
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
		return fmt.Errorf("listen address %q: bad port", addr)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("listen address %q is not loopback; this lab only binds 127.0.0.1, ::1 or localhost", addr)
	}
	return nil
}

// Listen opens a loopback-only TCP listener.
func Listen(addr string) (net.Listener, error) {
	if err := CheckLoopback(addr); err != nil {
		return nil, err
	}
	return net.Listen("tcp", addr)
}

// NewServer wraps h with timeouts and header limits suitable for a load-test target, HTTP/1.1 only.
func NewServer(h http.Handler) *http.Server {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	return &http.Server{
		Protocols:         &protocols,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
}
