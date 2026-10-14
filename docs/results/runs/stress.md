# stress run: PASS

stress test: 8 steps of 15 s from 400 to 2500 req/s.

- Started 2026-10-09T13:42:48Z on windows/amd64, 20 logical CPUs; go1.26.8, k6 v2.3.0 (commit/e088784614, go1.26.8, windows/amd64).
- Service: bound to 127.0.0.1, GOMAXPROCS=2, memory limit 256.0 MiB, 4 workers, queue 64, max wait 100 ms, 600 SHA-256 rounds and 64 KiB per request.
- Client: k6 with GOMAXPROCS=4. k6 and the service run on the same machine: both draw on the same CPUs, so these are lab numbers for comparison, not production capacity.
- SLO for gated windows: p95 <= 50 ms, errors <= 1%, dropped iterations <= 1%.

| Window | Target req/s | Client p95 ms | Client p99 ms | Failed | Dropped | Server ok/s | Shed | CPU avg | Workers busy | Queue avg/max | Sched p99 ms | Host CPU | Stalled s | SLO |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| step01 | 400 | 219.0 | 370.6 | 3.3% | 53 | 394 | 1.8% | 34.6% | 23.6% | 2.0/64 | 67.11 | 38.7% | 3 | misses |
| step02 | 700 | 144.5 | 303.1 | 2.1% | 12 | 683 | 2.4% | 49.5% | 26.3% | 1.5/64 | 117.44 | 33.0% | 3 | misses |
| step03 | 1000 | 151.1 | 264.0 | 3.9% | 48 | 951 | 4.5% | 56.7% | 25.0% | 2.7/64 | 117.44 | 25.7% | 2 | misses |
| step04 | 1300 | 6.3 | 11.2 | 0.0% | 0 | 1300 | 0.0% | 65.6% | 25.2% | 0.0/6 | 3.67 | 17.1% | 0 | meets |
| step05 | 1600 | 10.1 | 18.4 | 0.0% | 0 | 1600 | 0.0% | 78.1% | 32.3% | 0.0/13 | 6.29 | 19.2% | 0 | meets |
| step06 | 1900 | 67.9 | 95.6 | 1.2% | 0 | 1874 | 1.4% | 87.7% | 50.7% | 3.1/64 | 58.72 | 20.4% | 0 | misses |
| step07 | 2200 | 142.6 | 183.1 | 10.6% | 0 | 1979 | 9.8% | 92.9% | 59.1% | 4.4/64 | 134.22 | 25.9% | 0 | misses |
| step08 | 2500 | 196.8 | 259.8 | 25.0% | 775 | 1899 | 23.1% | 89.1% | 62.3% | 7.0/64 | 201.33 | 22.2% | 0 | misses |

## USE method, window step06 (1900 req/s)

| Resource | Utilization | Saturation | Errors |
|---|---|---|---|
| Service CPU (GOMAXPROCS=2) | avg 87.7%, max 91.4% | Go scheduler latency p99 58.72 ms | none at this level |
| Worker pool (4 workers) | busy avg 50.7%, max 59.2% | queue avg 3.1, max 64 of 64; queue wait p99 30.0 ms | shed: 352 queue full, 0 wait timeout (1.4% of requests) |
| Memory (limit 256.0 MiB) | max 16.1% of the limit (41.1 MiB mapped, heap max 23.9 MiB) | GC CPU 5.8%, 9.8 GC cycles/s | process stayed up, /healthz answered |
| Host CPU (20 logical CPUs, shared with k6 and other work) | avg 20.4%, max 26.5% | run queue not read; 0 s stalled (Go scheduler p99 > 50 ms while the service used < 70% of its CPU cap) | n/a |
| Load generator (k6, GOMAXPROCS=4, whole run) | 14.8% of its CPU cap (72.6 CPU-s in 122.7 s) | dropped iterations 0 in this window (888 in the run), VUs max 800 | transport errors 0 in the run |

## Checks

- pass: service alive after the run (GET /healthz after k6 finished)
- pass: load generator under 80% of its CPU cap (k6 used 15% of its CPU cap)
- pass: breaking point found within the steps and confirmed by a higher step (from step06 at 1900 req/s every step misses the SLO: p95 67.9 ms > 50 ms, errors 1.20% > 1% (transient misses at step01, step02, step03, followed by passing steps))

Breaking point: from step06 at 1900 req/s every step misses the SLO: p95 67.9 ms > 50 ms, errors 1.20% > 1% (transient misses at step01, step02, step03, followed by passing steps). Highest step that met the SLO: 1600 req/s.
