.PHONY: setup lint test audit build k6 bench e2e ci

EXE := $(if $(filter Windows_NT,$(OS)),.exe,)
CAPLAB := .tmp/bin/caplab$(EXE)
K6 := .tmp/bin/k6$(EXE)

setup:
	cd js && npm ci

lint:
	@files="$$(cd go && gofmt -l .)"; test -z "$$files" || (echo "$$files"; echo "run gofmt -w go"; exit 1)
	cd go && go vet ./...
	cd js && npm run lint

test:
	cd go && go test -race -count=1 ./...
	cd js && npm test

# Known vulnerabilities in the Go code paths actually called (Go 1.26+, as in CI) and in npm packages.
audit:
	cd go && go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
	cd js && npm audit --audit-level=low

build:
	cd go && go build -trimpath -o ../$(CAPLAB) ./cmd/caplab

# Pinned k6 release into .tmp/bin, checked against pinned SHA-256 values.
k6:
	bash scripts/get-k6.sh

# The full suite on this machine: stress, then load, spike and a shortened soak (about 8 minutes).
bench: build k6
	$(CAPLAB) suite -k6 $(K6) -out docs/results/runs

# The quick suite CI runs on every push (about 2 minutes). Exit 2 (a tool or measurement error,
# including no breaking point) fails the job; exit 1 (a run missed its checks on the shared runner)
# is reported as ::warning annotations, see docs/results/m1.md.
e2e: build k6
	$(CAPLAB) suite -k6 $(K6) -scale quick -procs 1 -client-procs 2 -out .tmp/runs-quick -annotate || [ $$? -eq 1 ]

ci: setup lint test
