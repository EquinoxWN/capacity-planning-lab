# Suite

Stress sustained 1600 req/s within the SLO; planned load 960 req/s (60% of it), spike peak 3200 req/s.

| Run | Verdict | Detail |
|---|---|---|
| [stress](stress.md) | PASS | from step06 at 1900 req/s every step misses the SLO: p95 67.9 ms > 50 ms, errors 1.20% > 1% (transient misses at step01, step02, step03, followed by passing steps) |
| [load](load.md) | FAIL | p95 131.6 ms, errors 2.00% at 960 req/s |
| [spike](spike.md) | FAIL | recovered 0.0 s after the peak |
| [soak](soak.md) | FAIL | heap x0.83, goroutines x0.53 from first to last third |

k6 and the service run on the same machine: both draw on the same CPUs, so these are lab numbers for comparison, not production capacity.
