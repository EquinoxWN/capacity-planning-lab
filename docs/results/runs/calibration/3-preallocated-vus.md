# stress run: FAIL

stress test: 8 steps of 15 s from 400 to 1800 req/s.

- Started 2026-10-09T13:25:25Z on windows/amd64, 20 logical CPUs; go1.26.8, k6 k6.exe v2.3.0 (commit/e088784614, go1.26.8, windows/amd64).
- Service: bound to 127.0.0.1, GOMAXPROCS=2, memory limit 256.0 MiB, 4 workers, queue 64, max wait 100 ms, 600 SHA-256 rounds and 64 KiB per request.
- Client: k6 with GOMAXPROCS=4. k6 and the service run on the same machine: both draw on the same CPUs, so these are lab numbers for comparison, not production capacity.
- SLO for gated windows: p95 <= 50 ms, errors <= 1%, dropped iterations <= 1%.

| Window | Target req/s | Client p95 ms | Client p99 ms | Failed | Dropped | Server ok/s | Shed | CPU avg | Workers busy | Queue avg/max | Sched p99 ms | Host CPU | SLO |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| step01 | 400 | 5.6 | 29.2 | 0.0% | 0 | 400 | 0.0% | 21.5% | 7.5% | 0.0/35 | 2.62 | 24.2% | meets |
| step02 | 600 | 9.7 | 17.8 | 0.0% | 0 | 601 | 0.0% | 33.0% | 13.6% | 0.0/2 | 5.24 | 21.1% | meets |
| step03 | 800 | 10.2 | 19.6 | 0.0% | 0 | 800 | 0.0% | 45.2% | 17.6% | 0.0/17 | 6.29 | 23.5% | meets |
| step04 | 1000 | 9.8 | 21.9 | 0.0% | 0 | 1000 | 0.0% | 51.9% | 21.5% | 0.0/1 | 5.24 | 28.7% | meets |
| step05 | 1200 | 8.7 | 26.0 | 0.0% | 0 | 1200 | 0.0% | 58.7% | 23.3% | 0.2/55 | 5.24 | 13.3% | meets |
| step06 | 1400 | 12.1 | 34.7 | 0.0% | 0 | 1400 | 0.0% | 66.3% | 28.1% | 0.1/28 | 6.29 | 12.9% | meets |
| step07 | 1600 | 11.2 | 16.3 | 0.0% | 0 | 1600 | 0.0% | 74.5% | 34.8% | 0.0/6 | 6.29 | 12.6% | meets |
| step08 | 1800 | 62.2 | 82.6 | 1.1% | 0 | 1779 | 1.2% | 81.2% | 53.7% | 4.3/64 | 50.33 | 14.1% | misses |

## USE method, window step08 (1800 req/s)

| Resource | Utilization | Saturation | Errors |
|---|---|---|---|
| Service CPU (GOMAXPROCS=2) | avg 81.2%, max 89.0% | Go scheduler latency p99 50.33 ms | none at this level |
| Worker pool (4 workers) | busy avg 53.7%, max 64.9% | queue avg 4.3, max 64 of 64; queue wait p99 32.8 ms | shed: 284 queue full, 0 wait timeout (1.2% of requests) |
| Memory (limit 256.0 MiB) | max 9.7% of the limit (24.8 MiB mapped, heap max 13.5 MiB) | GC CPU 5.9%, 17.1 GC cycles/s | process stayed up, /healthz answered |
| Host CPU (20 logical CPUs, shared with k6 and other work) | avg 14.1%, max 24.7% | not measured (no run-queue counter is read) | n/a |
| Load generator (k6, GOMAXPROCS=4, whole run) | 9.9% of its CPU cap (48.2 CPU-s in 121.7 s) | dropped iterations 0 in this window (0 in the run), VUs max 380 | transport errors 0 in the run |

## Checks

- pass: service alive after the run (GET /healthz after k6 finished)
- pass: load generator under 80% of its CPU cap (k6 used 10% of its CPU cap)
- FAIL: breaking point found within the steps and confirmed by a higher step (from step08 at 1800 req/s every step misses the SLO: p95 62.2 ms > 50 ms, errors 1.10% > 1%; only the last step missed, so this is unconfirmed)

Breaking point: from step08 at 1800 req/s every step misses the SLO: p95 62.2 ms > 50 ms, errors 1.10% > 1%; only the last step missed, so this is unconfirmed. Highest step that met the SLO: 1600 req/s.
