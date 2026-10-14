package service

import (
	"sync"
	"time"

	"github.com/EquinoxWN/capacity-planning-lab/go/internal/hist"
)

// Outcome is how a request to /work ended.
type Outcome int

// Request outcomes counted by Stats.
const (
	OK Outcome = iota
	ShedQueueFull
	ShedWaitTimeout
	Canceled
	numOutcomes
)

// outcomeNames are the label values used in /metrics and reports.
var outcomeNames = [numOutcomes]string{"ok", "shed_queue_full", "shed_wait_timeout", "canceled"}

// String returns the outcome's label.
func (o Outcome) String() string { return outcomeNames[o] }

// Interval is what the service observed between two calls to Stats.Cut.
type Interval struct {
	Duration     time.Duration
	Outcomes     [numOutcomes]uint64
	Latency      hist.Histogram // time in the handler for OK requests, queue wait included
	ShedLatency  hist.Histogram // time in the handler for shed requests
	Wait         hist.Histogram // queue wait of admitted requests
	BusySeconds  float64        // integral of busy workers over time
	QueueSeconds float64        // integral of waiting requests over time
	QueueMax     int
}

// Stats tracks request outcomes, latency and time-weighted pool occupancy.
type Stats struct {
	mu      sync.Mutex
	now     func() time.Time
	last    time.Time
	start   time.Time
	busy    int
	waiting int
	first   time.Time
	cur     Interval
	totals  [numOutcomes]uint64
}

// NewStats starts tracking at the clock's current time.
func NewStats(now func() time.Time) *Stats {
	t := now()
	return &Stats{now: now, last: t, start: t}
}

// advanceLocked accumulates occupancy up to t; the caller holds mu.
func (s *Stats) advanceLocked(t time.Time) {
	if dt := t.Sub(s.last).Seconds(); dt > 0 {
		s.cur.BusySeconds += float64(s.busy) * dt
		s.cur.QueueSeconds += float64(s.waiting) * dt
		s.last = t
	}
}

// arrivedLocked notes the first request time; the caller holds mu.
func (s *Stats) arrivedLocked(t time.Time) {
	if s.first.IsZero() {
		s.first = t
	}
}

// startDirect records a request that got a worker without waiting.
func (s *Stats) startDirect() {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.now()
	s.arrivedLocked(t)
	s.advanceLocked(t)
	s.busy++
	s.cur.Wait.Record(0)
}

// tryEnqueue registers a waiting request unless limit are already waiting.
func (s *Stats) tryEnqueue(limit int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.now()
	s.arrivedLocked(t)
	if s.waiting >= limit {
		return false
	}
	s.advanceLocked(t)
	s.waiting++
	s.cur.QueueMax = max(s.cur.QueueMax, s.waiting)
	return true
}

// dequeue removes a waiting request; admitted ones become busy and record their wait.
func (s *Stats) dequeue(admitted bool, wait time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceLocked(s.now())
	s.waiting--
	if admitted {
		s.busy++
		s.cur.Wait.Record(wait)
	}
}

// done records the end of a request; wasBusy says whether it held a worker.
func (s *Stats) done(o Outcome, latency time.Duration, wasBusy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceLocked(s.now())
	if wasBusy {
		s.busy--
	}
	s.cur.Outcomes[o]++
	s.totals[o]++
	switch o {
	case OK:
		s.cur.Latency.Record(latency)
	case ShedQueueFull, ShedWaitTimeout:
		s.cur.ShedLatency.Record(latency)
	}
}

// Cut returns the interval since the previous Cut and starts a new one.
func (s *Stats) Cut() Interval {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.now()
	s.advanceLocked(t)
	out := s.cur
	out.Duration = t.Sub(s.start)
	s.start = t
	s.cur = Interval{QueueMax: s.waiting}
	return out
}

// FirstRequest returns when the first /work request arrived, or the zero time.
func (s *Stats) FirstRequest() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.first
}

// Gauges returns the current busy workers, waiting requests and outcome totals.
func (s *Stats) Gauges() (busy, waiting int, totals [numOutcomes]uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy, s.waiting, s.totals
}
