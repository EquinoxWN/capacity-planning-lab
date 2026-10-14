// Package service is the sample service under test: a CPU- and memory-bound /work endpoint
// behind a bounded worker pool that sheds load instead of queueing without limit.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// Config sizes the worker pool and the cost of one request.
type Config struct {
	Workers    int           // requests served at once
	QueueLimit int           // requests allowed to wait for a worker
	MaxWait    time.Duration // longest a request may wait before it is shed
	WorkIters  int           // SHA-256 rounds over a 1 KiB block per request
	WorkKB     int           // KiB allocated and touched per request
}

// DefaultConfig is the configuration used for the recorded M1 runs.
func DefaultConfig() Config {
	return Config{Workers: 4, QueueLimit: 64, MaxWait: 100 * time.Millisecond, WorkIters: 600, WorkKB: 64}
}

// Validate rejects configurations outside safe bounds.
func (c Config) Validate() error {
	switch {
	case c.Workers < 1 || c.Workers > 1024:
		return fmt.Errorf("workers must be 1..1024, got %d", c.Workers)
	case c.QueueLimit < 0 || c.QueueLimit > 100_000:
		return fmt.Errorf("queue limit must be 0..100000, got %d", c.QueueLimit)
	case c.MaxWait < time.Millisecond || c.MaxWait > 10*time.Second:
		return fmt.Errorf("max wait must be 1ms..10s, got %v", c.MaxWait)
	case c.WorkIters < 0 || c.WorkIters > 100_000:
		return fmt.Errorf("work iterations must be 0..100000, got %d", c.WorkIters)
	case c.WorkKB < 0 || c.WorkKB > 4096:
		return fmt.Errorf("work KiB must be 0..4096, got %d", c.WorkKB)
	}
	return nil
}

// Service serves /work through a bounded pool and counts every outcome.
type Service struct {
	cfg   Config
	slots chan struct{}
	stats *Stats
	seq   atomic.Uint64
}

// New builds a service; now is the clock used for statistics.
func New(cfg Config, now func() time.Time) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Service{cfg: cfg, slots: make(chan struct{}, cfg.Workers), stats: NewStats(now)}, nil
}

// Config returns the service configuration.
func (s *Service) Config() Config { return s.cfg }

// Stats returns the live statistics.
func (s *Service) Stats() *Stats { return s.stats }

// errShed is returned by admit when the request must be refused.
var errShed = errors.New("shed")

// admit waits for a worker; it reports the outcome when the request is refused.
func (s *Service) admit(ctx context.Context) (Outcome, error) {
	select {
	case s.slots <- struct{}{}:
		s.stats.startDirect()
		return OK, nil
	default:
	}
	if !s.stats.tryEnqueue(s.cfg.QueueLimit) {
		return ShedQueueFull, errShed
	}
	start := time.Now()
	timer := time.NewTimer(s.cfg.MaxWait)
	defer timer.Stop()
	select {
	case s.slots <- struct{}{}:
		s.stats.dequeue(true, time.Since(start))
		return OK, nil
	case <-timer.C:
		s.stats.dequeue(false, 0)
		return ShedWaitTimeout, errShed
	case <-ctx.Done():
		s.stats.dequeue(false, 0)
		return Canceled, ctx.Err()
	}
}

// Handler returns the HTTP routes: /work, /healthz and /metrics.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /work", s.work)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /metrics", s.metrics)
	return mux
}

// work serves one unit of work or sheds the request with 503.
func (s *Service) work(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	outcome, err := s.admit(r.Context())
	if err != nil {
		if outcome != Canceled {
			w.Header().Set("Retry-After", "1")
			w.Header().Set("X-Shed-Reason", outcome.String())
			http.Error(w, "overloaded, retry later", http.StatusServiceUnavailable)
		}
		s.stats.done(outcome, time.Since(start), false)
		return
	}
	digest := Work(s.seq.Add(1), s.cfg.WorkIters, s.cfg.WorkKB)
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, "{\"digest\":%q}\n", hex.EncodeToString(digest[:8]))
	// Count the worker as free before freeing it, so busy never exceeds the pool size.
	s.stats.done(OK, time.Since(start), true)
	<-s.slots
}

// metrics writes counters and gauges in the Prometheus text format.
func (s *Service) metrics(w http.ResponseWriter, _ *http.Request) {
	busy, waiting, totals := s.stats.Gauges()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	buf := []byte("# HELP caplab_requests_total Requests to /work by outcome.\n# TYPE caplab_requests_total counter\n")
	for o := range numOutcomes {
		buf = fmt.Appendf(buf, "caplab_requests_total{outcome=%q} %d\n", o.String(), totals[o])
	}
	gauge := func(name, help string, v int) {
		buf = fmt.Appendf(buf, "# HELP %s %s\n# TYPE %s gauge\n%s %s\n", name, help, name, name, strconv.Itoa(v))
	}
	gauge("caplab_workers_busy", "Workers serving a request now.", busy)
	gauge("caplab_workers", "Size of the worker pool.", s.cfg.Workers)
	gauge("caplab_queue_waiting", "Requests waiting for a worker now.", waiting)
	gauge("caplab_queue_limit", "Requests allowed to wait before new ones are shed.", s.cfg.QueueLimit)
	_, _ = w.Write(buf)
}

// Work hashes a 1 KiB block iters times and allocates kb KiB, returning the final digest.
func Work(seed uint64, iters, kb int) [sha256.Size]byte {
	block := make([]byte, max(kb, 1)*1024)
	for off := 0; off+8 <= len(block); off += 512 {
		binary.LittleEndian.PutUint64(block[off:], seed+uint64(off))
	}
	sum := sha256.Sum256(block)
	h := sha256.New()
	for range iters {
		h.Reset()
		_, _ = h.Write(sum[:])
		_, _ = h.Write(block[:1024])
		h.Sum(sum[:0])
	}
	return sum
}
