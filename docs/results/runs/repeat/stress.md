# stress run: PASS

stress test: 8 steps of 15 s from 400 to 2500 req/s.

- Started 2026-10-09T13:50:14Z on windows/amd64, 20 logical CPUs; go1.26.8, k6 v2.3.0 (commit/e088784614, go1.26.8, windows/amd64).
- Service: bound to 127.0.0.1, GOMAXPROCS=2, memory limit 256.0 MiB, 4 workers, queue 64, max wait 100 ms, 600 SHA-256 rounds and 64 KiB per request.
- Client: k6 with GOMAXPROCS=4. k6 and the service run on the same machine: both draw on the same CPUs, so these are lab numbers for comparison, not production capacity.
- SLO for gated windows: p95 <= 50 ms, errors <= 1%, dropped iterations <= 1%.

| Window | Target req/s | Client p95 ms | Client p99 ms | Failed | Dropped | Server ok/s | Shed | CPU avg | Workers busy | Queue avg/max | Sched p99 ms | Host CPU | Stalled s | SLO |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| step01 | 400 | 75.9 | 385.2 | 0.0% | 0 | 400 | 0.0% | 30.7% | 15.4% | 0.6/60 | 33.55 | 26.2% | 3 | misses |
| step02 | 700 | 9.9 | 41.3 | 0.0% | 0 | 700 | 0.0% | 43.9% | 16.2% | 0.0/0 | 5.24 | 22.0% | 0 | meets |
| step03 | 1000 | 182.2 | 413.6 | 5.9% | 198 | 916 | 6.8% | 55.5% | 26.9% | 3.2/64 | 117.44 | 30.1% | 2 | misses |
| step04 | 1300 | 6.3 | 19.1 | 0.0% | 0 | 1300 | 0.0% | 65.3% | 25.0% | 0.0/0 | 3.15 | 17.6% | 0 | meets |
| step05 | 1600 | 165.3 | 958.8 | 3.9% | 1618 | 1439 | 3.2% | 76.8% | 41.2% | 1.2/64 | 117.44 | 28.5% | 2 | misses |
| step06 | 1900 | 54.2 | 73.9 | 0.4% | 0 | 1894 | 0.4% | 86.0% | 43.4% | 0.6/64 | 41.94 | 19.6% | 0 | misses |
| step07 | 2200 | 270.5 | 366.5 | 18.8% | 1828 | 1697 | 17.4% | 91.6% | 63.0% | 6.4/64 | 268.44 | 34.2% | 0 | misses |
| step08 | 2500 | 333.7 | 513.7 | 30.2% | 4752 | 1560 | 28.8% | 89.7% | 63.1% | 9.3/64 | 268.44 | 34.2% | 0 | misses |

## USE method, window step05 (1600 req/s)

| Resource | Utilization | Saturation | Errors |
|---|---|---|---|
| Service CPU (GOMAXPROCS=2) | avg 76.8%, max 89.1% | Go scheduler latency p99 117.44 ms | none at this level |
| Worker pool (4 workers) | busy avg 41.2%, max 78.6% | queue avg 1.2, max 64 of 64; queue wait p99 39.0 ms | shed: 627 queue full, 0 wait timeout (3.2% of requests) |
| Memory (limit 256.0 MiB) | max 16.0% of the limit (41.0 MiB mapped, heap max 21.8 MiB) | GC CPU 5.3%, 7.9 GC cycles/s | process stayed up, /healthz answered |
| Host CPU (20 logical CPUs, shared with k6 and other work) | avg 28.5%, max 71.2% | run queue not read; 2 s stalled (Go scheduler p99 > 50 ms while the service used < 70% of its CPU cap) | n/a |
| Load generator (k6, GOMAXPROCS=4, whole run) | 15.2% of its CPU cap (73.9 CPU-s in 121.6 s) | dropped iterations 1618 in this window (8396 in the run), VUs max 800 | transport errors 0 in the run |

## Checks

- pass: service alive after the run (GET /healthz after k6 finished)
- pass: load generator under 80% of its CPU cap (k6 used 15% of its CPU cap)
- pass: breaking point found within the steps and confirmed by a higher step (from step05 at 1600 req/s every step misses the SLO: p95 165.3 ms > 50 ms, errors 3.90% > 1%, client dropped 1618 iterations > 240, host stall: 2 s with Go scheduler p99 > 50 ms while the service used < 70% of its CPU cap (transient misses at step01, step03, followed by passing steps))

Breaking point: from step05 at 1600 req/s every step misses the SLO: p95 165.3 ms > 50 ms, errors 3.90% > 1%, client dropped 1618 iterations > 240, host stall: 2 s with Go scheduler p99 > 50 ms while the service used < 70% of its CPU cap (transient misses at step01, step03, followed by passing steps). Highest step that met the SLO: 1300 req/s.
