# stress run: FAIL

stress test: 8 steps of 15 s from 400 to 1800 req/s.

- Started 2026-10-09T13:22:39Z on windows/amd64, 20 logical CPUs; go1.26.8, k6 k6.exe v2.3.0 (commit/e088784614, go1.26.8, windows/amd64).
- Service: bound to 127.0.0.1, GOMAXPROCS=2, memory limit 256.0 MiB, 4 workers, queue 64, max wait 100 ms, 600 SHA-256 rounds and 64 KiB per request.
- Client: k6 with GOMAXPROCS=4. k6 and the service run on the same machine: both draw on the same CPUs, so these are lab numbers for comparison, not production capacity.
- SLO for gated windows: p95 <= 50 ms, errors <= 1%, dropped iterations <= 1%.

| Window | Target req/s | Client p95 ms | Client p99 ms | Failed | Dropped | Server ok/s | Shed | CPU avg | Workers busy | Queue avg/max | Sched p99 ms | Host CPU | SLO |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| step01 | 400 | 3.5 | 8.1 | 0.0% | 0 | 400 | 0.0% | 22.5% | 8.1% | 0.0/0 | 1.57 | 33.2% | meets |
| step02 | 600 | 5.4 | 17.3 | 0.0% | 0 | 600 | 0.0% | 32.7% | 12.7% | 0.0/0 | 2.62 | 43.8% | meets |
| step03 | 800 | 2.4 | 4.8 | 0.0% | 0 | 800 | 0.0% | 47.9% | 17.4% | 0.0/0 | 1.57 | 46.9% | meets |
| step04 | 1000 | 10.5 | 36.6 | 0.0% | 255 | 982 | 0.0% | 52.6% | 22.1% | 0.0/27 | 5.24 | 40.2% | misses (client-limited) |
| step05 | 1200 | 98.0 | 187.5 | 1.0% | 637 | 1136 | 1.1% | 60.2% | 32.3% | 2.4/64 | 50.33 | 41.2% | misses |
| step06 | 1400 | 115.9 | 258.6 | 3.4% | 917 | 1336 | 2.6% | 67.3% | 39.9% | 3.2/64 | 67.11 | 43.2% | misses |
| step07 | 1600 | 24.9 | 59.3 | 0.0% | 681 | 1551 | 0.0% | 75.7% | 34.5% | 0.4/60 | 16.78 | 40.5% | misses (client-limited) |
| step08 | 1800 | 66.9 | 112.1 | 0.8% | 377 | 1752 | 0.9% | 79.9% | 40.7% | 1.0/64 | 58.72 | 37.9% | misses |

## USE method, window step04 (1000 req/s)

| Resource | Utilization | Saturation | Errors |
|---|---|---|---|
| Service CPU (GOMAXPROCS=2) | avg 52.6%, max 57.9% | Go scheduler latency p99 5.24 ms | none at this level |
| Worker pool (4 workers) | busy avg 22.1%, max 28.1% | queue avg 0.0, max 27 of 64; queue wait p99 0.0 ms | shed: 0 queue full, 0 wait timeout (0.0% of requests) |
| Memory (limit 256.0 MiB) | max 4.6% of the limit (11.8 MiB mapped, heap max 5.3 MiB) | GC CPU 3.2%, 23.5 GC cycles/s | process stayed up, /healthz answered |
| Host CPU (20 logical CPUs, shared with k6 and other work) | avg 40.2%, max 76.0% | not measured (no run-queue counter is read) | n/a |
| Load generator (k6, GOMAXPROCS=4, whole run) | 12.7% of its CPU cap (61.6 CPU-s in 121.3 s) | dropped iterations 255 in this window (2867 in the run), VUs max 495 | transport errors 0 in the run |

## Checks

- pass: service alive after the run (GET /healthz after k6 finished)
- pass: load generator under 80% of its CPU cap (k6 used 13% of its CPU cap)
- FAIL: breaking point found within the steps and confirmed by a higher step (the load generator could not deliver 1000 req/s while the service was healthy; capacity is at least 800 req/s)

Breaking point: the load generator could not deliver 1000 req/s while the service was healthy; capacity is at least 800 req/s. Highest step that met the SLO: 800 req/s.
