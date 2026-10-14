# load run: FAIL

load test: 960 req/s for 60 s after a 10 s warm-up.

- Started 2026-10-09T13:44:51Z on windows/amd64, 20 logical CPUs; go1.26.8, k6 v2.3.0 (commit/e088784614, go1.26.8, windows/amd64).
- Service: bound to 127.0.0.1, GOMAXPROCS=2, memory limit 256.0 MiB, 4 workers, queue 64, max wait 100 ms, 600 SHA-256 rounds and 64 KiB per request.
- Client: k6 with GOMAXPROCS=4. k6 and the service run on the same machine: both draw on the same CPUs, so these are lab numbers for comparison, not production capacity.
- SLO for gated windows: p95 <= 50 ms, errors <= 1%, dropped iterations <= 1%.

| Window | Target req/s | Client p95 ms | Client p99 ms | Failed | Dropped | Server ok/s | Shed | CPU avg | Workers busy | Queue avg/max | Sched p99 ms | Host CPU | Stalled s | SLO |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| warmup | 960 | 3.9 | 7.3 | 0.0% | 0 | 585 | 0.0% | 32.8% | 10.9% | 0.0/0 | 2.62 | 17.1% | 0 | meets |
| load | 960 | 131.6 | 278.8 | 2.0% | 115 | 938 | 2.1% | 53.6% | 24.9% | 0.9/64 | 100.66 | 20.7% | 8 | misses, gated |

## USE method, window load (960 req/s)

| Resource | Utilization | Saturation | Errors |
|---|---|---|---|
| Service CPU (GOMAXPROCS=2) | avg 53.6%, max 84.3% | Go scheduler latency p99 100.66 ms | none at this level |
| Worker pool (4 workers) | busy avg 24.9%, max 69.4% | queue avg 0.9, max 64 of 64; queue wait p99 35.7 ms | shed: 1071 queue full, 97 wait timeout (2.1% of requests) |
| Memory (limit 256.0 MiB) | max 12.8% of the limit (32.8 MiB mapped, heap max 17.7 MiB) | GC CPU 3.2%, 7.2 GC cycles/s | process stayed up, /healthz answered |
| Host CPU (20 logical CPUs, shared with k6 and other work) | avg 20.7%, max 57.5% | run queue not read; 8 s stalled (Go scheduler p99 > 50 ms while the service used < 70% of its CPU cap) | n/a |
| Load generator (k6, GOMAXPROCS=4, whole run) | 11.2% of its CPU cap (32.0 CPU-s in 71.4 s) | dropped iterations 115 in this window (115 in the run), VUs max 520 | transport errors 0 in the run |

## Checks

- pass: service alive after the run (GET /healthz after k6 finished)
- pass: load generator under 80% of its CPU cap (k6 used 11% of its CPU cap)
- FAIL: window load (960 req/s) meets the SLO (misses: p95 131.6 ms > 50 ms; errors 2.00% > 1%; host stall: 8 s with Go scheduler p99 > 50 ms while the service used < 70% of its CPU cap)
