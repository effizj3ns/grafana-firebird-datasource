# Changelog

All notable changes to this project will be documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2026-10-19

### Added
- **Server-side SQL macro expansion** (backend, quote-aware, case-insensitive):
  `$__timeFilter(col)`, `$__timeFilter(col, tzBiasMinutes)`, `$__timeFrom()`,
  `$__timeTo()`, `$__unixEpochFilter(col)`, `$__unixEpochNanoFilter(col)`,
  `$__unixEpochFrom()`, `$__unixEpochTo()`, `$__interval` (seconds),
  `$__interval_ms`, `$__range(col)`, `$__unixEpochGroup(col)` /
  `$__timeGroup[Alias](col)` (epoch-aligned GROUP BY buckets for time series).
- **Template variable support**: `metricFindQuery` — one column = value+label,
  two columns = value + display text. Runs through the backend (macros,
  maxRows, read-only checks apply).
- **Annotation support**: annotation query with mandatory `time` column plus
  optional `text` and comma-separated `tags` columns.
- Official Firebird phoenix logo (Wikimedia, copyrighted free use).

### Fixed
- Datasource invisible in the **panel** datasource picker: plugin.json now
  sets `metrics: true` (Grafana filters DS plugins without any of
  metrics/annotations/tracing/logs/alerting meta).
- `Could not load plugin` on settings page: frontend bundle is now a proper
  AMD module with standard externals (was `externalsType: 'window'`).
- Panel queries with `$__timeFilter` returned 500 `Token unknown - $`:
  macros are now expanded in the Go backend (backend plugins receive rawSql
  verbatim; client-side interpolation does not apply).
- ConfigEditor label wrapping (3+2 field layout, correct labelWidths).

## [0.1.0] - 2026-10-18

### Added
- Native Grafana datasource plugin for Firebird.
- SQL editor with `format` selector (table / time series).
- Read-only enforcement: SELECT/WITH-only, multi-statement block,
  `FOR UPDATE` / `WITH LOCK` blocked; comment-stripping prevents
  comment-based smuggling.
- Connection pool settings (max open/idle, lifetime, query timeout).
- `maxRows` result cap (default 100000) to protect Grafana from
  unbounded result sets on large tables.
- Type mapping verified against a live Firebird 2.x server:
  SHORT/LONG/INT64 -> int64, scaled NUMERIC/DECIMAL -> float64,
  TIMESTAMP/DATE -> time (UTC), CHAR/VARCHAR/BLOB -> string.
- ConfigEditor: host, port, database path, user, encrypted password,
  pool + timeout settings.
- GitLab CI: lint, test (go -race + jest), build, package (zip artifact
  with loadability smoke gate).

### Security
- Credentials only in encrypted secureJsonData; never echoed to logs or
  error messages.
- DSN injection hardening: charset allowlist (UTF8, WIN1252, WIN1251,
  ISO8859_1), URL-escaped user/password/database path.
