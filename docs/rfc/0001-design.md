# RFC 0001: capacity-planning-lab design

- **Status:** Accepted for M1
- **Author:** EquinoxWN
- **Created:** 2026-10-08

## Problem

"How many servers will we need?" is usually answered from a guess, a vendor sizing sheet or the
last outage. The evidence that should back the answer is a load test pushed to the service's
breaking point, with the reason it broke (which resource ran out) measured rather than assumed.
Teams skip it because a load test that is run wrong gives a confident wrong number: closed-model
load that slows down with the service, a load generator that is itself the bottleneck, a noisy
neighbour on the machine, or a "max throughput" with no latency target attached.

This project builds that evidence end to end for a sample service: k6 runs load, stress, spike
and soak tests against it, and every run reports the USE method (utilization, saturation, errors)
of each resource next to the client-side latency and errors.

**Users:** backend and platform engineers who must size a service or defend a capacity number,
and people learning how to load test without fooling themselves.

## Define: critical-to-quality measures (CTQs)

| CTQ | Target | Measured by |
|---|---|---|
| SLO of the sample service | p95 <= 50 ms, errors <= 1%, dropped k6 iterations <= 1%, per window | k6 thresholds tagged per scenario |
| Breaking point found | stress test finds a step after which every higher step misses the SLO, confirmed by at least one higher step | `report.breakingPoint` |
| Breaking point is repeatable | two stress runs on the same machine give sustained rates within one step (300 req/s) | two recorded runs |
| Planned load holds | load at 60% of the sustained rate (40% headroom) meets the SLO | gated `load` window |
| Overload is refused fast | during a spike to twice the sustained rate, shed responses p99 <= 1.5 x the queue's max wait (150 ms) and the process stays up | server shed latency histogram, `/healthz` |
| Recovery | back within the SLO (3 healthy one-second samples) within 10 s after the spike ends | server samples |
| No drift | shortened soak: heap and goroutines in the last third at most 1.25 x the first third | server samples |
| Measurement is valid | k6 under 80% of its CPU cap; windows where only k6 dropped iterations are flagged client-limited, not counted as service failures | process CPU of k6, analysis |
| Contained | service and k6 only on loopback; every process stopped after a run; nothing written outside the repo | code checks, tests, manual check |

## Goals (M1)

- k6 runs load, stress, spike and soak tests against a sample service.
- Resources are checked with the USE method: utilization, saturation and errors, for the service
  CPU, its worker pool, its memory, the host CPU and the load generator.

## Non-goals

- Running as a hosted production service, or testing anything outside the loopback interface.
- Distributed load generation or k6 Cloud.
- Profiling and fixing bottlenecks (M2), Little's Law sizing of pools and replicas (M2), growth
  forecasts and a written capacity plan with cost (M3).
- Absolute capacity numbers that transfer to other hardware: client and server share one laptop.

## Design

```
caplab run|suite  (one Go process, GOMAXPROCS and GOMEMLIMIT capped)
 ├─ service      127.0.0.1:<free port>  GET /work  -> bounded worker pool -> SHA-256 + allocation
 │                                       503 when the queue is full or the wait exceeds 100 ms
 ├─ sampler      every 1 s: outcomes, latency histograms, busy-worker and queue integrals,
 │               process CPU, host CPU, runtime/metrics (memory, GC CPU, scheduler latency)
 └─ k6 child     js/k6/{load,stress,spike,soak}.js, GOMAXPROCS capped, usage report off
                 -> versioned summary JSON (caplab.k6-summary.v1)
report: join k6 windows with server samples -> verdict, breaking point, recovery, drift -> JSON + Markdown
```

- **Sample service** (`go/internal/service`): each request hashes a 1 KiB block 600 times and
  allocates 64 KiB: about 1 ms of CPU per request including HTTP handling (52% of two
  cores at 1000 req/s in a calibration run). Four workers, a queue of 64 and a 100 ms maximum wait;
  beyond that the request is shed with `503` and `Retry-After: 1`, so overload produces fast
  errors instead of unbounded queues. It refuses non-loopback listen addresses.
- **Profiles** (`js/k6/lib/profiles.js`): every window is a k6 scenario with an arrival-rate
  executor (open model, ADR 0003) and SLO thresholds tagged with its name. Stress: 8 steps of 15 s
  from 400 to 2500 req/s, 300 req/s apart. Load: 10 s ramp, 60 s hold. Spike: 20 s base, 10 s at the peak, 30 s
  base. Soak: 180 s, labelled as shortened. A `quick` scale shortens each for CI. Inputs are
  validated and a plan over 600 s is refused.
- **USE sampler** (`go/internal/usage`): utilization from time-weighted busy workers and process
  CPU over GOMAXPROCS; saturation from queue length, queue wait, Go scheduler latency and GC CPU;
  errors from shed counts and k6 transport errors. Host CPU from `GetSystemTimes` (Windows) or
  `/proc/stat` (Linux).
- **Analysis** (`go/internal/report`): parses the k6 summary strictly, aligns windows on the
  first request, trims the first second of each window, and judges each kind. The breaking point
  is the start of the persistent misses (ADR 0004).
- **Suite** (`caplab suite`): stress first; then load and soak at 60% of the sustained rate and a
  spike to twice the sustained rate.

## Alternatives considered

| Option | Why not (yet) |
|---|---|
| Closed-model load (`constant-vus`, a fixed number of users) | The offered load falls as the service slows, hiding overload (coordinated omission). Open-model arrival rates keep the planned rate and expose queueing; the cost is VU memory and drops to track (ADR 0003). |
| Prometheus scraping `/metrics` plus Grafana for USE numbers | Adds two long-running processes on a shared laptop, a scrape interval to align, and leftovers to clean up. The in-process sampler gives one-second samples aligned to k6's first request (ADR 0002); `caplab serve` keeps `/metrics` for this option later. |
| Service in a container with CPU and memory limits | The usual way to cap resources, but Docker is not available here; GOMAXPROCS and GOMEMLIMIT cap the Go process instead. GOMEMLIMIT is a soft limit, so memory is reported against it rather than enforced by the kernel. |
| Load generator on a second machine | The right setup for production numbers; not available. Instead k6 is capped with GOMAXPROCS, its CPU use is measured, and every result states that both share the machine. |
| Calling the first SLO miss the breaking point | Simple, but one noisy step on a shared laptop under-sized the service more than three times in a real run (ADR 0004). |

## Measurement plan

- `caplab suite` on the author's laptop (Intel Core i7-13700H, 20 logical CPUs, Windows 11) with
  the service at GOMAXPROCS=2 and a 256 MiB soft memory limit, k6 v2.3.0 at GOMAXPROCS=4. A second
  stress run checks repeatability.
- For every window: client p95, p99, error rate and dropped iterations from k6; server throughput,
  shed rate, CPU, worker utilization, queue length, queue wait, scheduler latency, memory, GC CPU
  and host CPU from the sampler.
- Results, root causes of defects and the controls that hold them go into `docs/results/m1.md`
  (Measure, Analyze, Improve, Control). CI re-runs the quick suite on every push.

## Milestones

- **M1:** k6 runs load, stress, spike and soak tests against a sample service. Resources are checked with the USE method.
- **M2:** Profilers locate the top bottlenecks, and each fix is re-measured. Little's Law links throughput, latency and concurrency to size pools and replicas.
- **M3:** A growth forecast estimates when current capacity runs out. A written plan states limits, headroom, cost and recommended changes.

## Risks and open questions

- Client and server share CPUs and memory with each other and with unrelated work; numbers vary
  between runs. Mitigated by caps, measured client CPU, host CPU in every window, transient-miss
  detection and a repeatability run, not removed.
- The sampler runs in the measured process and adds a little CPU of its own.
- Host CPU saturation (run-queue length) is not read on Windows; only host utilization is.
- A soak of a few minutes cannot show slow leaks that take hours; it is labelled as shortened.
