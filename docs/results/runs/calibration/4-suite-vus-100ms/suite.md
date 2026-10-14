# Suite

Stress sustained 1600 req/s within the SLO; planned load 960 req/s (60% of it), spike peak 3200 req/s.

| Run | Verdict | Detail |
|---|---|---|
| [stress](stress.md) | PASS | from step06 at 1900 req/s every step misses the SLO: p95 54.2 ms > 50 ms |
| [load](load.md) | FAIL | p95 24.3 ms, errors 0.30% at 960 req/s |
| [spike](spike.md) | PASS | recovered 0.2 s after the peak |
| [soak](soak.md) | PASS | heap x0.98, goroutines x0.53 from first to last third |

k6 and the service run on the same machine: both draw on the same CPUs, so these are lab numbers for comparison, not production capacity.
