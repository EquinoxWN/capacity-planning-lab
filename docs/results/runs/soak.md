# soak run: FAIL

shortened soak: 960 req/s for 180 s on a shared laptop (a production soak runs for hours).

- Started 2026-10-09T13:47:03Z on windows/amd64, 20 logical CPUs; go1.26.8, k6 v2.3.0 (commit/e088784614, go1.26.8, windows/amd64).
- Service: bound to 127.0.0.1, GOMAXPROCS=2, memory limit 256.0 MiB, 4 workers, queue 64, max wait 100 ms, 600 SHA-256 rounds and 64 KiB per request.
- Client: k6 with GOMAXPROCS=4. k6 and the service run on the same machine: both draw on the same CPUs, so these are lab numbers for comparison, not production capacity.
- SLO for gated windows: p95 <= 50 ms, errors <= 1%, dropped iterations <= 1%.

| Window | Target req/s | Client p95 ms | Client p99 ms | Failed | Dropped | Server ok/s | Shed | CPU avg | Workers busy | Queue avg/max | Sched p99 ms | Host CPU | Stalled s | SLO |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| warmup | 960 | 8.2 | 15.5 | 0.0% | 0 | 543 | 0.0% | 31.2% | 13.2% | 0.0/8 | 3.67 | 13.5% | 0 | meets |
| soak | 960 | 120.5 | 359.7 | 3.1% | 1705 | 921 | 3.1% | 49.7% | 23.9% | 1.6/64 | 83.89 | 20.2% | 18 | misses, gated |

## USE method, window soak (960 req/s)

| Resource | Utilization | Saturation | Errors |
|---|---|---|---|
| Service CPU (GOMAXPROCS=2) | avg 49.7%, max 80.6% | Go scheduler latency p99 83.89 ms | none at this level |
| Worker pool (4 workers) | busy avg 23.9%, max 79.5% | queue avg 1.6, max 64 of 64; queue wait p99 50.5 ms | shed: 4200 queue full, 1022 wait timeout (3.1% of requests) |
| Memory (limit 256.0 MiB) | max 12.7% of the limit (32.5 MiB mapped, heap max 18.6 MiB) | GC CPU 2.6%, 7.7 GC cycles/s | process stayed up, /healthz answered |
| Host CPU (20 logical CPUs, shared with k6 and other work) | avg 20.2%, max 67.0% | run queue not read; 18 s stalled (Go scheduler p99 > 50 ms while the service used < 70% of its CPU cap) | n/a |
| Load generator (k6, GOMAXPROCS=4, whole run) | 10.1% of its CPU cap (76.8 CPU-s in 191.1 s) | dropped iterations 1705 in this window (1705 in the run), VUs max 520 | transport errors 0 in the run |

## Checks

- pass: service alive after the run (GET /healthz after k6 finished)
- pass: load generator under 80% of its CPU cap (k6 used 10% of its CPU cap)
- FAIL: window soak (960 req/s) meets the SLO (misses: p95 120.5 ms > 50 ms; errors 3.10% > 1%; host stall: 18 s with Go scheduler p99 > 50 ms while the service used < 70% of its CPU cap)
- pass: heap does not grow (last third / first third) (ratio 0.83, limit 1.25)
- pass: goroutines do not grow (last third / first third) (ratio 0.53, limit 1.25)

Drift (last third / first third): heap x0.83, goroutines x0.53, mean per-second server p95 3.8 -> 7.3 ms.
