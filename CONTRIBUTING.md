# Contributing

## Development setup

Requirements: Node 22+, Go 1.26+, Git.

```bash
git clone https://github.com/effizj3ns/grafana-firebird-datasource.git
cd grafana-firebird-datasource
npm install
go mod download
```

## Build & test

```bash
# Backend
cd pkg && go build ./... && go vet ./... && go test -race ./...

# Integration tests (needs a reachable Firebird; skipped without env)
FB_HOST=... FB_DATABASE=... FB_USER=... FB_PASSWORD=... go test -run TestIntegration ./pkg/plugin/

# Frontend
npm run build
npm test

# Full plugin artifact (dist/)
npm run build && cd pkg && go build -o ../dist/gpx_grafana-firebird-datasource .
```

## Commit conventions

Use Conventional Commits: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `ci:`, `chore:`.

## Code style

- Go: gofmt + go vet must pass; error paths must never swallow errors silently.
- TypeScript: strict mode, no `any` unless interfacing with Grafana's untyped areas.
- Tests accompany every behavior change; `rowsToFrame` type-mapping changes
  require both unit tests and, where applicable, integration-test coverage.

## CI

All CI jobs (lint, test, build, package) must be green before merge.
The `package:plugin` job enforces that the built artifact is loadable
(plugin.json + module.js + img/ + backend binary).
