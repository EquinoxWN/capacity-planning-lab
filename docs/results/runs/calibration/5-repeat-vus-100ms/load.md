# load run: FAIL

load test: 960 req/s for 60 s after a 10 s warm-up.

- Started 2026-10-09T13:40:35Z on windows/amd64, 20 logical CPUs; go1.26.8, k6 v2.3.0 (commit/e088784614, go1.26.8, windows/amd64).
- Service: bound to 127.0.0.1, GOMAXPROCS=2, memory limit 256.0 MiB, 4 workers, queue 64, max wait 100 ms, 600 SHA-256 rounds and 64 KiB per request.
- Client: k6 with GOMAXPROCS=4. k6 and the service run on the same machine: both draw on the same CPUs, so these are lab numbers for comparison, not production capacity.
- SLO for gated windows: p95 <= 50 ms, errors <= 1%, dropped iterations <= 1%.

| Window | Target req/s | Client p95 ms | Client p99 ms | Failed | Dropped | Server ok/s | Shed | CPU avg | Workers busy | Queue avg/max | Sched p99 ms | Host CPU | Stalled s | SLO |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| warmup | 960 | 173.8 | 464.6 | 6.7% | 115 | 486 | 8.1% | 41.7% | 28.3% | 9.0/64 | 83.89 | 33.6% | 3 | misses |
| load | 960 | 35.2 | 182.6 | 0.6% | 617 | 943 | 0.6% | 52.5% | 23.0% | 0.8/64 | 16.78 | 20.0% | 1 | misses, gated |

## USE method, window load (960 req/s)

| Resource | Utilization | Saturation | Errors |
|---|---|---|---|
| Service CPU (GOMAXPROCS=2) | avg 52.5%, max 82.2% | Go scheduler latency p99 16.78 ms | none at this level |
| Worker pool (4 workers) | busy avg 23.0%, max 62.6% | queue avg 0.8, max 64 of 64; queue wait p99 32.8 ms | shed: 322 queue full, 33 wait timeout (0.6% of requests) |
| Memory (limit 256.0 MiB) | max 6.9% of the limit (17.7 MiB mapped, heap max 8.3 MiB) | GC CPU 3.3%, 15.6 GC cycles/s | process stayed up, /healthz answered |
| Host CPU (20 logical CPUs, shared with k6 and other work) | avg 20.0%, max 58.9% | run queue not read; 1 s stalled (Go scheduler p99 > 50 ms while the service used < 70% of its CPU cap) | n/a |
| Load generator (k6, GOMAXPROCS=4, whole run) | 10.2% of its CPU cap (28.8 CPU-s in 70.9 s) | dropped iterations 617 in this window (732 in the run), VUs max 232 | transport errors 0 in the run |

## Checks

- pass: service alive after the run (GET /healthz after k6 finished)
- pass: load generator under 80% of its CPU cap (k6 used 10% of its CPU cap)
- FAIL: window load (960 req/s) meets the SLO (misses: client dropped 617 iterations > 576; host stall: 1 s with Go scheduler p99 > 50 ms while the service used < 70% of its CPU cap)
