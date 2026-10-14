# stress run: FAIL

stress test: 8 steps of 15 s from 400 to 1800 req/s.

- Started 2026-10-08T14:20:03Z on windows/amd64, 20 logical CPUs; go1.26.8, k6 k6.exe v2.3.0 (commit/e088784614, go1.26.8, windows/amd64).
- Service: bound to 127.0.0.1, GOMAXPROCS=2, memory limit 256.0 MiB, 4 workers, queue 64, max wait 100 ms, 600 SHA-256 rounds and 64 KiB per request.
- Client: k6 with GOMAXPROCS=4. k6 and the service run on the same machine: both draw on the same CPUs, so these are lab numbers for comparison, not production capacity.
- SLO for gated windows: p95 <= 50 ms, errors <= 1%, dropped iterations <= 1%.

| Window | Target req/s | Client p95 ms | Client p99 ms | Failed | Dropped | Server ok/s | Shed | CPU avg | Workers busy | Queue avg/max | Sched p99 ms | Host CPU | SLO |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| step01 | 400 | 2.8 | 4.8 | 0.0% | 0 | 400 | 0.0% | 24.9% | 8.0% | 0.0/0 | 1.57 | 37.0% | meets |
| step02 | 600 | 35.3 | 126.7 | 0.0% | 126 | 591 | 0.0% | 33.2% | 19.1% | 0.2/33 | 20.97 | 47.9% | misses (client-limited) |
| step03 | 800 | 14.4 | 37.4 | 0.0% | 18 | 798 | 0.0% | 44.2% | 18.7% | 0.1/23 | 6.29 | 41.5% | meets |
| step04 | 1000 | 12.9 | 44.3 | 0.0% | 71 | 994 | 0.0% | 51.0% | 22.8% | 0.2/34 | 6.29 | 40.3% | meets |
| step05 | 1200 | 5.6 | 25.8 | 0.0% | 9 | 1200 | 0.0% | 63.7% | 23.3% | 0.0/0 | 1.84 | 33.1% | meets |
| step06 | 1400 | 13.2 | 32.3 | 0.0% | 7 | 1399 | 0.0% | 75.2% | 29.7% | 0.0/41 | 7.34 | 33.0% | meets |
| step07 | 1600 | 68.7 | 103.2 | 0.6% | 914 | 1523 | 0.5% | 79.7% | 41.6% | 1.7/64 | 58.72 | 33.8% | misses |
| step08 | 1800 | 73.1 | 110.5 | 1.6% | 1326 | 1681 | 1.8% | 85.1% | 47.3% | 1.8/64 | 58.72 | 32.8% | misses |

## USE method, window step02 (600 req/s)

| Resource | Utilization | Saturation | Errors |
|---|---|---|---|
| Service CPU (GOMAXPROCS=2) | avg 33.2%, max 39.9% | Go scheduler latency p99 20.97 ms | none at this level |
| Worker pool (4 workers) | busy avg 19.1%, max 49.4% | queue avg 0.2, max 33 of 64; queue wait p99 11.6 ms | shed: 0 queue full, 0 wait timeout (0.0% of requests) |
| Memory (limit 256.0 MiB) | max 3.9% of the limit (10.1 MiB mapped, heap max 3.8 MiB) | GC CPU 4.2%, 17.3 GC cycles/s | process stayed up, /healthz answered |
| Host CPU (20 logical CPUs, shared with k6 and other work) | avg 47.9%, max 59.1% | not measured (no run-queue counter is read) | n/a |
| Load generator (k6, GOMAXPROCS=4, whole run) | 13.2% of its CPU cap (64.0 CPU-s in 120.9 s) | dropped iterations 126 in this window (2471 in the run), VUs max 389 | transport errors 0 in the run |

## Checks

- pass: service alive after the run (GET /healthz after k6 finished)
- pass: load generator under 80% of its CPU cap (k6 used 13% of its CPU cap)
- FAIL: breaking point found within the steps (the load generator could not deliver 600 req/s while the service was healthy; capacity is at least 400 req/s)

Breaking point: the load generator could not deliver 600 req/s while the service was healthy; capacity is at least 400 req/s. Highest step that met the SLO: 400 req/s.
