# ADR 0003: Drive load with arrival-rate executors (open model)

- **Status:** Accepted

## Context

k6 can generate load two ways. With a fixed number of virtual users (closed model, `constant-vus`
or `ramping-vus`) each user waits for its response before sending again, so when the service slows
down the offered load drops with it. That hides the overload the stress and spike tests are meant
to find (coordinated omission): the latency recorded is for fewer requests than real users would
send. With arrival-rate executors (open model) k6 starts iterations at a fixed rate whatever the
response time, as independent users on the internet do.

## Decision

Every window uses `constant-arrival-rate` (warm-ups use `ramping-arrival-rate`). Each window is its
own k6 scenario, so k6 tags every request with the window name and evaluates SLO thresholds per
window. VU pools are sized from the rate (enough for about 250 ms per request, plus 20, capped at
400 for memory) and every VU is started before the test, because k6 drops iterations while it
starts extra VUs mid-run.

## Consequences

- The service sees the planned rate even when it is overloaded, so stress and spike tests show the
  real queueing, shedding and latency.
- When all VUs are busy, k6 drops iterations instead of sending them late. Dropped iterations are
  therefore part of the SLO (at most 1% per window) and are reported for every window.
- A drop can come from the service (slow responses hold VUs) or from the client (VUs not started
  yet, CPU starvation). The analysis calls a window client-limited only when the service was not
  shedding and its own p95 was within the SLO.
- The calibration runs showed exactly this: with a small pre-allocated pool that k6 grew on
  demand, k6 dropped 133 iterations at 800 req/s while the service sat at 23% CPU, and in a full
  stress run 2,867 iterations over the steps from 1000 req/s up. With every VU started up front the
  next full run dropped none (the machine was also less busy then; see docs/results/m1.md).
- A pool sized for 100 ms per request was still too small: in two load runs at 960 req/s, short
  slowdowns pushed latency past 100 ms (the queue's own max wait), all 116 VUs were busy, and k6
  dropped 741 and 617 iterations ("Insufficient VUs" in its log). The pool must cover the
  service's designed worst case (100 ms wait plus the work) with margin, hence 250 ms.
- Higher rates need more VUs and memory than a closed model would; the cap of 400 VUs bounds it.
