# ADR 0004: The breaking point is the start of the persistent SLO misses

- **Status:** Accepted

## Context

The stress test raises the arrival rate in steps and checks each step against the SLO (p95 at most
50 ms, errors at most 1%, dropped iterations at most 1%). The first version of the analysis called
the first step that missed the SLO the breaking point. On this shared laptop that was wrong: in a
full run the 600 req/s step missed (p95 35 ms, p99 127 ms, 126 dropped iterations, Go scheduler
latency p99 21 ms, host CPU up from 37% to 48%) while 800, 1000, 1200 and 1400 req/s all passed.
Something else on the machine took the CPU for a few seconds. Calling 400 req/s the capacity would
have under-sized the service by more than three times.

## Decision

Load only rises during a stress test, so a real capacity limit keeps failing. The breaking point
is the first step of the trailing run of misses; the sustained rate is the step before it. Misses
followed by a passing step are reported as transient. A breaking point counts as confirmed only
when a higher step also missed. The suite derives the planned load only from a confirmed breaking
point that was not client-limited.

## Consequences

- One noisy step no longer decides the capacity, and the noise stays visible in the report.
- Every stress plan needs at least one step above the breaking point, which costs one more step of
  load (15 s at full scale).
- A service that degrades and then recovers at a higher rate (for example after a cache warms) is
  treated as noise before the last miss. That pattern is not expected from this sample service; a
  real service may need longer steps or repeated runs.
- Repeatability is checked by running the stress test twice and comparing the sustained rates.
