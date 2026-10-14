# spike run: PASS

spike test: 960 -> 3200 req/s for 10 s -> 960 req/s.

- Started 2026-10-09T13:31:12Z on windows/amd64, 20 logical CPUs; go1.26.8, k6 k6.exe v2.3.0 (commit/e088784614, go1.26.8, windows/amd64).
- Service: bound to 127.0.0.1, GOMAXPROCS=2, memory limit 256.0 MiB, 4 workers, queue 64, max wait 100 ms, 600 SHA-256 rounds and 64 KiB per request.
- Client: k6 with GOMAXPROCS=4. k6 and the service run on the same machine: both draw on the same CPUs, so these are lab numbers for comparison, not production capacity.
- SLO for gated windows: p95 <= 50 ms, errors <= 1%, dropped iterations <= 1%.

| Window | Target req/s | Client p95 ms | Client p99 ms | Failed | Dropped | Server ok/s | Shed | CPU avg | Workers busy | Queue avg/max | Sched p99 ms | Host CPU | SLO |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| before | 960 | 2.9 | 9.2 | 0.0% | 0 | 960 | 0.0% | 49.8% | 18.5% | 0.0/12 | 1.84 | 33.6% | meets, gated |
| spike | 3200 | 127.4 | 1998.8 | 38.3% | 1950 | 1887 | 18.2% | 90.9% | 59.5% | 8.4/64 | 117.44 | 38.2% | misses |
| after | 960 | 2.1 | 4.5 | 0.0% | 0 | 960 | 0.0% | 54.5% | 18.6% | 0.0/0 | 1.57 | 31.8% | meets, gated |

## USE method, window spike (3200 req/s)

| Resource | Utilization | Saturation | Errors |
|---|---|---|---|
| Service CPU (GOMAXPROCS=2) | avg 90.9%, max 95.3% | Go scheduler latency p99 117.44 ms | none at this level |
| Worker pool (4 workers) | busy avg 59.5%, max 74.2% | queue avg 8.4, max 64 of 64; queue wait p99 39.0 ms | shed: 3351 queue full, 0 wait timeout (18.2% of requests) |
| Memory (limit 256.0 MiB) | max 11.2% of the limit (28.7 MiB mapped, heap max 14.4 MiB) | GC CPU 6.0%, 17.9 GC cycles/s | process stayed up, /healthz answered |
| Host CPU (20 logical CPUs, shared with k6 and other work) | avg 38.2%, max 48.7% | not measured (no run-queue counter is read) | n/a |
| Load generator (k6, GOMAXPROCS=4, whole run) | 15.6% of its CPU cap (38.0 CPU-s in 61.0 s) | dropped iterations 1950 in this window (1950 in the run), VUs max 456 | transport errors 6907 in the run |

## Checks

- pass: service alive after the run (GET /healthz after k6 finished)
- pass: load generator under 80% of its CPU cap (k6 used 16% of its CPU cap)
- pass: window before (960 req/s) meets the SLO (meets the SLO)
- pass: window after (960 req/s) meets the SLO (meets the SLO)
- pass: overload is refused fast (server shed p99) (shed p99 0.5 ms, limit 150 ms; 3351 shed of 18449)
- pass: back within the SLO within 10 s of the peak ending (recovered after 0.2 s)

Recovery after the spike: 0.2 s.
