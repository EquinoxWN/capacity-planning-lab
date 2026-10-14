# stress run: PASS

stress test: 8 steps of 15 s from 400 to 2500 req/s.

- Started 2026-10-09T13:38:33Z on windows/amd64, 20 logical CPUs; go1.26.8, k6 v2.3.0 (commit/e088784614, go1.26.8, windows/amd64).
- Service: bound to 127.0.0.1, GOMAXPROCS=2, memory limit 256.0 MiB, 4 workers, queue 64, max wait 100 ms, 600 SHA-256 rounds and 64 KiB per request.
- Client: k6 with GOMAXPROCS=4. k6 and the service run on the same machine: both draw on the same CPUs, so these are lab numbers for comparison, not production capacity.
- SLO for gated windows: p95 <= 50 ms, errors <= 1%, dropped iterations <= 1%.

| Window | Target req/s | Client p95 ms | Client p99 ms | Failed | Dropped | Server ok/s | Shed | CPU avg | Workers busy | Queue avg/max | Sched p99 ms | Host CPU | Stalled s | SLO |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| step01 | 400 | 6.0 | 10.4 | 0.0% | 0 | 400 | 0.0% | 21.9% | 7.8% | 0.0/0 | 2.62 | 11.1% | 0 | meets |
| step02 | 700 | 7.3 | 14.1 | 0.0% | 0 | 700 | 0.0% | 37.3% | 14.4% | 0.0/3 | 3.15 | 12.2% | 0 | meets |
| step03 | 1000 | 7.1 | 13.2 | 0.0% | 0 | 1000 | 0.0% | 48.9% | 20.2% | 0.0/16 | 3.67 | 12.7% | 0 | meets |
| step04 | 1300 | 5.6 | 11.1 | 0.0% | 0 | 1300 | 0.0% | 67.1% | 25.5% | 0.0/18 | 3.15 | 18.5% | 0 | meets |
| step05 | 1600 | 9.7 | 17.3 | 0.0% | 0 | 1600 | 0.0% | 75.2% | 33.4% | 0.0/22 | 6.29 | 18.4% | 0 | meets |
| step06 | 1900 | 55.9 | 74.8 | 0.7% | 0 | 1883 | 0.7% | 84.0% | 49.3% | 1.9/64 | 50.33 | 19.2% | 0 | misses |
| step07 | 2200 | 117.0 | 148.7 | 16.5% | 47 | 1829 | 16.6% | 85.5% | 62.9% | 8.8/64 | 100.66 | 18.8% | 0 | misses |
| step08 | 2500 | 158.4 | 295.0 | 29.1% | 1962 | 1696 | 29.2% | 87.0% | 64.3% | 12.4/64 | 134.22 | 26.4% | 0 | misses |

## USE method, window step06 (1900 req/s)

| Resource | Utilization | Saturation | Errors |
|---|---|---|---|
| Service CPU (GOMAXPROCS=2) | avg 84.0%, max 89.8% | Go scheduler latency p99 50.33 ms | none at this level |
| Worker pool (4 workers) | busy avg 49.3%, max 58.9% | queue avg 1.9, max 64 of 64; queue wait p99 25.3 ms | shed: 169 queue full, 0 wait timeout (0.7% of requests) |
| Memory (limit 256.0 MiB) | max 11.3% of the limit (29.0 MiB mapped, heap max 15.8 MiB) | GC CPU 6.0%, 14.5 GC cycles/s | process stayed up, /healthz answered |
| Host CPU (20 logical CPUs, shared with k6 and other work) | avg 19.2%, max 27.3% | run queue not read; 0 s stalled (Go scheduler p99 > 50 ms while the service used < 70% of its CPU cap) | n/a |
| Load generator (k6, GOMAXPROCS=4, whole run) | 12.9% of its CPU cap (62.6 CPU-s in 121.3 s) | dropped iterations 0 in this window (2009 in the run), VUs max 510 | transport errors 0 in the run |

## Checks

- pass: service alive after the run (GET /healthz after k6 finished)
- pass: load generator under 80% of its CPU cap (k6 used 13% of its CPU cap)
- pass: breaking point found within the steps and confirmed by a higher step (from step06 at 1900 req/s every step misses the SLO: p95 55.9 ms > 50 ms)

Breaking point: from step06 at 1900 req/s every step misses the SLO: p95 55.9 ms > 50 ms. Highest step that met the SLO: 1600 req/s.
