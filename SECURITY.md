# Security policy

## Reporting a vulnerability

Please report security problems privately through GitHub:
**Security > Report a vulnerability** on this repository
(`https://github.com/EquinoxWN/capacity-planning-lab/security/advisories/new`). Do not open a public issue.

Include what you found, how to reproduce it, and the impact you expect. You will get an answer
within 7 days. Fixes are released as soon as they are ready, and you are credited unless you ask
not to be.

## Supported versions

This project is pre-1.0. Only the latest commit on `main` receives fixes.

## Scope

- **In scope:** the sample service or the k6 scripts reaching beyond the loopback interface, the
  runner reading files or running programs other than the ones it was given, unbounded memory or
  time use from crafted summaries or flags, and leaks of the user's environment (for example k6
  cloud tokens) into k6 runs.
- **Out of scope:** load you point at your own services with these scripts, and performance
  numbers that differ on other machines.

## How this repository protects itself

- The service and the k6 scripts refuse any address that is not 127.0.0.1, ::1 or localhost.
- The runner starts k6 without inherited `K6_*` variables, with usage reports and automatic
  extension downloads off, and with its config and cache inside the repository.
- k6 is downloaded from the official release and checked against pinned SHA-256 values.
- The k6 summary is size-limited, schema-versioned and decoded with unknown fields refused.
- Every GitHub Action is pinned to a full commit SHA, and workflows run with read-only
  permissions and without persisted credentials.
- Dependabot update pull requests are paused (`open-pull-requests-limit: 0`) so the repository keeps a single `main` branch; dependencies are updated by hand and checked by the CI audit.
- CI runs lint, tests and a known-vulnerability check (govulncheck, npm audit) on every push and pull request.
