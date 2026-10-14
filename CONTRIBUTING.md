# Contributing

Thanks for helping. This is a Go and JavaScript (k6) project; issues and pull requests are welcome.

## Set up and check your change

Needs Go 1.26+, Node.js 24+ and bash with curl (Git Bash on Windows). Caches and downloads stay
in git-ignored folders inside the repo (`.tmp/`, `js/node_modules/`).

```bash
make setup   # npm ci for the type definitions and TypeScript
make lint    # gofmt, go vet, tsc strict check of the k6 scripts
make test    # Go tests (race detector) and Node tests
make audit   # govulncheck and npm audit (as in CI)
make e2e     # quick suite with the pinned k6, about 2 minutes
```

A pull request is ready when `make lint` and `make test` pass and CI is green.

## Guidelines

- Keep each change focused; explain the problem it solves in the pull request.
- Add or update a test for every behaviour change, and update `docs/results/` when numbers change.
- The k6 summary format is a contract: change `js/k6/lib/summary.js`, `go/internal/report/summary.go`
  and `testdata/contract/summary-v1.json` together, and bump the schema id when it breaks.
- Load runs must stay short and bound to loopback; say on every result that client and server
  shared the machine.
- Significant design changes need an ADR in `docs/adr/` (copy `0001` for the format).
- Keep function comments to one short phrase; let names and tests explain the rest.
- Report security problems privately, as described in [SECURITY.md](SECURITY.md).

By contributing you agree that your work is released under the [MIT License](LICENSE).
