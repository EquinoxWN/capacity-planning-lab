# stress run: PASS

stress test: 8 steps of 15 s from 400 to 2500 req/s.

- Started 2026-10-09T13:28:00Z on windows/amd64, 20 logical CPUs; go1.26.8, k6 k6.exe v2.3.0 (commit/e088784614, go1.26.8, windows/amd64).
- Service: bound to 127.0.0.1, GOMAXPROCS=2, memory limit 256.0 MiB, 4 workers, queue 64, max wait 100 ms, 600 SHA-256 rounds and 64 KiB per request.
- Client: k6 with GOMAXPROCS=4. k6 and the service run on the same machine: both draw on the same CPUs, so these are lab numbers for comparison, not production capacity.
- SLO for gated windows: p95 <= 50 ms, errors <= 1%, dropped iterations <= 1%.

| Window | Target req/s | Client p95 ms | Client p99 ms | Failed | Dropped | Server ok/s | Shed | CPU avg | Workers busy | Queue avg/max | Sched p99 ms | Host CPU | SLO |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| step01 | 400 | 3.4 | 7.9 | 0.0% | 0 | 399 | 0.0% | 22.5% | 7.4% | 0.0/0 | 2.62 | 22.1% | meets |
| step02 | 700 | 3.2 | 5.1 | 0.0% | 0 | 700 | 0.0% | 38.0% | 13.3% | 0.0/0 | 2.10 | 17.9% | meets |
| step03 | 1000 | 3.5 | 6.9 | 0.0% | 0 | 1000 | 0.0% | 56.3% | 20.4% | 0.0/0 | 2.10 | 28.6% | meets |
| step04 | 1300 | 11.9 | 54.1 | 0.0% | 0 | 1300 | 0.0% | 66.0% | 26.3% | 0.0/32 | 5.24 | 29.6% | meets |
| step05 | 1600 | 16.3 | 26.8 | 0.0% | 0 | 1598 | 0.0% | 77.2% | 33.6% | 0.0/34 | 8.39 | 24.1% | meets |
| step06 | 1900 | 54.2 | 72.4 | 0.5% | 0 | 1889 | 0.6% | 88.9% | 45.2% | 1.4/64 | 50.33 | 22.8% | misses |
| step07 | 2200 | 131.7 | 164.2 | 17.9% | 251 | 1783 | 18.3% | 81.8% | 63.8% | 10.1/64 | 117.44 | 15.4% | misses |
| step08 | 2500 | 143.7 | 185.0 | 23.5% | 894 | 1874 | 23.7% | 87.4% | 61.8% | 12.3/64 | 117.44 | 28.9% | misses |

## USE method, window step06 (1900 req/s)

| Resource | Utilization | Saturation | Errors |
|---|---|---|---|
| Service CPU (GOMAXPROCS=2) | avg 88.9%, max 100.0% | Go scheduler latency p99 50.33 ms | none at this level |
| Worker pool (4 workers) | busy avg 45.2%, max 62.1% | queue avg 1.4, max 64 of 64; queue wait p99 25.3 ms | shed: 147 queue full, 0 wait timeout (0.6% of requests) |
| Memory (limit 256.0 MiB) | max 11.5% of the limit (29.5 MiB mapped, heap max 16.1 MiB) | GC CPU 5.8%, 14.5 GC cycles/s | process stayed up, /healthz answered |
| Host CPU (20 logical CPUs, shared with k6 and other work) | avg 22.8%, max 35.9% | not measured (no run-queue counter is read) | n/a |
| Load generator (k6, GOMAXPROCS=4, whole run) | 13.7% of its CPU cap (66.5 CPU-s in 121.2 s) | dropped iterations 0 in this window (1145 in the run), VUs max 510 | transport errors 0 in the run |

## Checks

- pass: service alive after the run (GET /healthz after k6 finished)
- pass: load generator under 80% of its CPU cap (k6 used 14% of its CPU cap)
- pass: breaking point found within the steps and confirmed by a higher step (from step06 at 1900 req/s every step misses the SLO: p95 54.2 ms > 50 ms)

Breaking point: from step06 at 1900 req/s every step misses the SLO: p95 54.2 ms > 50 ms. Highest step that met the SLO: 1600 req/s.
