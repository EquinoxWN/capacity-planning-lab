# soak run: PASS

shortened soak: 960 req/s for 180 s on a shared laptop (a production soak runs for hours).

- Started 2026-10-09T13:32:13Z on windows/amd64, 20 logical CPUs; go1.26.8, k6 k6.exe v2.3.0 (commit/e088784614, go1.26.8, windows/amd64).
- Service: bound to 127.0.0.1, GOMAXPROCS=2, memory limit 256.0 MiB, 4 workers, queue 64, max wait 100 ms, 600 SHA-256 rounds and 64 KiB per request.
- Client: k6 with GOMAXPROCS=4. k6 and the service run on the same machine: both draw on the same CPUs, so these are lab numbers for comparison, not production capacity.
- SLO for gated windows: p95 <= 50 ms, errors <= 1%, dropped iterations <= 1%.

| Window | Target req/s | Client p95 ms | Client p99 ms | Failed | Dropped | Server ok/s | Shed | CPU avg | Workers busy | Queue avg/max | Sched p99 ms | Host CPU | SLO |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| warmup | 960 | 2.2 | 3.7 | 0.0% | 0 | 553 | 0.0% | 31.3% | 10.3% | 0.0/0 | 1.31 | 28.6% | meets |
| soak | 960 | 4.8 | 87.7 | 0.2% | 435 | 956 | 0.2% | 52.0% | 19.5% | 0.2/64 | 2.62 | 23.5% | meets, gated |

## USE method, window soak (960 req/s)

| Resource | Utilization | Saturation | Errors |
|---|---|---|---|
| Service CPU (GOMAXPROCS=2) | avg 52.0%, max 87.5% | Go scheduler latency p99 2.62 ms | none at this level |
| Worker pool (4 workers) | busy avg 19.5%, max 74.9% | queue avg 0.2, max 64 of 64; queue wait p99 0.0 ms | shed: 284 queue full, 54 wait timeout (0.2% of requests) |
| Memory (limit 256.0 MiB) | max 9.2% of the limit (23.4 MiB mapped, heap max 13.4 MiB) | GC CPU 2.0%, 11.3 GC cycles/s | process stayed up, /healthz answered |
| Host CPU (20 logical CPUs, shared with k6 and other work) | avg 23.5%, max 65.7% | not measured (no run-queue counter is read) | n/a |
| Load generator (k6, GOMAXPROCS=4, whole run) | 9.0% of its CPU cap (68.7 CPU-s in 191.0 s) | dropped iterations 435 in this window (435 in the run), VUs max 232 | transport errors 0 in the run |

## Checks

- pass: service alive after the run (GET /healthz after k6 finished)
- pass: load generator under 80% of its CPU cap (k6 used 9% of its CPU cap)
- pass: window soak (960 req/s) meets the SLO (meets the SLO)
- pass: heap does not grow (last third / first third) (ratio 0.98, limit 1.25)
- pass: goroutines do not grow (last third / first third) (ratio 0.53, limit 1.25)

Drift (last third / first third): heap x0.98, goroutines x0.53, server p95 1.7 -> 1.7 ms.
