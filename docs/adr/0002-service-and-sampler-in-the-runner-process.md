# ADR 0002: Run the service and the USE sampler inside the runner process

- **Status:** Accepted

## Context

A run needs three things at once: the service under test, a sampler that reads its utilization,
saturation and errors every second, and k6. The machine is a shared laptop with other builds and a
GPU job running, so a run must never leave a process behind, must not collide on ports with other
work, and must line up server samples with k6's timeline. The usual layout is three processes: the
service, an exporter (for example Prometheus scraping `/metrics`) and k6.

## Decision

`caplab run` starts the service inside its own process (the runner) on `127.0.0.1:0` (a free port chosen by the OS),
samples it in-process from the service's own counters, the Go runtime (`runtime/metrics`) and the
OS CPU counters, and starts k6 as its only child. GOMAXPROCS and the soft memory limit cap the
whole process. The first `/work` request marks time zero for aligning k6's windows with the samples.
`caplab serve` still runs the service alone, with a Prometheus-format `/metrics`, for use with
other tools.

## Consequences

- When the runner exits, the service exits with it; k6 is killed through its context. The only
  process to check after a run is k6, and the runner waits for it.
- No port conflicts with other work on the machine, and no scrape interval to tune.
- The sampler's own CPU (one tick per second) counts toward the service's CPU numbers. It is small
  next to the request load, but it is not zero, and the results say the CPU figure is per process.
- Alignment is exact to the first request rather than to a scrape timestamp; k6's per-window
  boundaries can still be off by its scheduling jitter, so each window's first second is skipped.
- A production setup would scrape a separate process; M2 adds profiles, and Prometheus or Grafana
  remain an option through `caplab serve`.
