# spike run: FAIL

spike test: 960 -> 3200 req/s for 10 s -> 960 req/s.

- Started 2026-10-09T13:46:02Z on windows/amd64, 20 logical CPUs; go1.26.8, k6 v2.3.0 (commit/e088784614, go1.26.8, windows/amd64).
- Service: bound to 127.0.0.1, GOMAXPROCS=2, memory limit 256.0 MiB, 4 workers, queue 64, max wait 100 ms, 600 SHA-256 rounds and 64 KiB per request.
- Client: k6 with GOMAXPROCS=4. k6 and the service run on the same machine: both draw on the same CPUs, so these are lab numbers for comparison, not production capacity.
- SLO for gated windows: p95 <= 50 ms, errors <= 1%, dropped iterations <= 1%.

| Window | Target req/s | Client p95 ms | Client p99 ms | Failed | Dropped | Server ok/s | Shed | CPU avg | Workers busy | Queue avg/max | Sched p99 ms | Host CPU | Stalled s | SLO |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| before | 960 | 97.0 | 228.9 | 1.5% | 19 | 944 | 1.6% | 49.8% | 24.8% | 0.8/64 | 67.11 | 19.2% | 0 | misses, gated |
| spike | 3200 | 199.7 | 2000.0 | 50.0% | 5228 | 1424 | 3.0% | 81.4% | 48.4% | 3.8/64 | 134.22 | 34.2% | 1 | misses |
| after | 960 | 115.9 | 369.3 | 4.1% | 209 | 911 | 4.4% | 53.6% | 23.9% | 2.3/64 | 83.89 | 25.4% | 2 | misses, gated |

## USE method, window spike (3200 req/s)

| Resource | Utilization | Saturation | Errors |
|---|---|---|---|
| Service CPU (GOMAXPROCS=2) | avg 81.4%, max 89.9% | Go scheduler latency p99 134.22 ms | none at this level |
| Worker pool (4 workers) | busy avg 48.4%, max 65.1% | queue avg 3.8, max 64 of 64; queue wait p99 55.1 ms | shed: 291 queue full, 56 wait timeout (3.0% of requests) |
| Memory (limit 256.0 MiB) | max 11.4% of the limit (29.3 MiB mapped, heap max 16.0 MiB) | GC CPU 5.4%, 11.1 GC cycles/s | process stayed up, /healthz answered |
| Host CPU (20 logical CPUs, shared with k6 and other work) | avg 34.2%, max 57.2% | run queue not read; 1 s stalled (Go scheduler p99 > 50 ms while the service used < 70% of its CPU cap) | n/a |
| Load generator (k6, GOMAXPROCS=4, whole run) | 17.5% of its CPU cap (42.7 CPU-s in 60.9 s) | dropped iterations 5228 in this window (5456 in the run), VUs max 660 | transport errors 12925 in the run |

## Checks

- pass: service alive after the run (GET /healthz after k6 finished)
- pass: load generator under 80% of its CPU cap (k6 used 18% of its CPU cap)
- FAIL: window before (960 req/s) meets the SLO (misses: p95 97.0 ms > 50 ms; errors 1.50% > 1%)
- FAIL: window after (960 req/s) meets the SLO (misses: p95 115.9 ms > 50 ms; errors 4.10% > 1%; host stall: 2 s with Go scheduler p99 > 50 ms while the service used < 70% of its CPU cap)
- FAIL: overload is refused fast (client p99 and server shed p99) (client p99 2000.0 ms, server shed p99 127.3 ms, limit 150 ms; the service shed 347 and answered 11385; 12925 client transport errors in the run (connection refused, or no answer within the 2 s timeout))
- pass: back within the SLO within 10 s of the peak ending (recovered after 0.0 s)

Recovery after the spike: 0.0 s.
