# load run: FAIL

load test: 960 req/s for 60 s after a 10 s warm-up.

- Started 2026-10-09T13:30:01Z on windows/amd64, 20 logical CPUs; go1.26.8, k6 k6.exe v2.3.0 (commit/e088784614, go1.26.8, windows/amd64).
- Service: bound to 127.0.0.1, GOMAXPROCS=2, memory limit 256.0 MiB, 4 workers, queue 64, max wait 100 ms, 600 SHA-256 rounds and 64 KiB per request.
- Client: k6 with GOMAXPROCS=4. k6 and the service run on the same machine: both draw on the same CPUs, so these are lab numbers for comparison, not production capacity.
- SLO for gated windows: p95 <= 50 ms, errors <= 1%, dropped iterations <= 1%.

| Window | Target req/s | Client p95 ms | Client p99 ms | Failed | Dropped | Server ok/s | Shed | CPU avg | Workers busy | Queue avg/max | Sched p99 ms | Host CPU | SLO |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| warmup | 960 | 3.3 | 5.6 | 0.0% | 0 | 561 | 0.0% | 28.9% | 10.4% | 0.0/3 | 1.84 | 18.6% | meets |
| load | 960 | 24.3 | 165.8 | 0.3% | 741 | 945 | 0.3% | 52.4% | 22.6% | 0.7/64 | 10.49 | 31.7% | misses, gated |

## USE method, window load (960 req/s)

| Resource | Utilization | Saturation | Errors |
|---|---|---|---|
| Service CPU (GOMAXPROCS=2) | avg 52.4%, max 68.0% | Go scheduler latency p99 10.49 ms | none at this level |
| Worker pool (4 workers) | busy avg 22.6%, max 60.1% | queue avg 0.7, max 64 of 64; queue wait p99 30.0 ms | shed: 136 queue full, 18 wait timeout (0.3% of requests) |
| Memory (limit 256.0 MiB) | max 8.6% of the limit (22.0 MiB mapped, heap max 10.5 MiB) | GC CPU 3.1%, 12.2 GC cycles/s | process stayed up, /healthz answered |
| Host CPU (20 logical CPUs, shared with k6 and other work) | avg 31.7%, max 90.0% | not measured (no run-queue counter is read) | n/a |
| Load generator (k6, GOMAXPROCS=4, whole run) | 7.9% of its CPU cap (22.3 CPU-s in 70.7 s) | dropped iterations 741 in this window (741 in the run), VUs max 232 | transport errors 0 in the run |

## Checks

- pass: service alive after the run (GET /healthz after k6 finished)
- pass: load generator under 80% of its CPU cap (k6 used 8% of its CPU cap)
- FAIL: window load (960 req/s) meets the SLO (misses: [client dropped 741 iterations > 576])
