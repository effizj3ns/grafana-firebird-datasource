# Firebird data source plugin for Grafana

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Grafana](https://img.shields.io/badge/Grafana-10.4%2B-orange)](https://grafana.com)
[![Firebird](https://img.shields.io/badge/Firebird-2.5%20%7C%203.0%20%7C%204.0%20%7C%205.0-red)](#compatibility)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8)](https://go.dev)

Read-only SQL queries from Grafana dashboards directly against **Firebird**
databases — no ODBC driver, no fbclient, no ETL middleware. The plugin ships
a pure-Go Firebird wire-protocol driver
([nakagami/firebirdsql](https://github.com/nakagami/firebirdsql)) behind the
Grafana backend plugin SDK, so the only requirement on the Grafana host is
network reachability to the database server.

<a id="compatibility"></a>
## Compatibility

Verified against live servers, full feature matrix (type mapping, SQL macros
including overflow-safe time bucketing, `maxRows` cap, read-only validation):

| Firebird | Architecture | Status |
|----------|--------------|--------|
| 2.5.9 (Classic, dialect 1) | Windows/Linux | ✅ verified |
| 3.0.11 | Linux | ✅ verified |
| 4.0.7 | Linux | ✅ verified |
| 5.0.4 | Linux | ✅ verified |

The matrix test lives in [`pkg/plugin/matrix_test.go`](pkg/plugin/matrix_test.go)
and can be re-run against your own servers:

```bash
FB_MATRIX='v25@db-host:3050:/db/legacy.fdb:SYSDBA:pw,v50@db-host:3051:/db/new.fdb:SYSDBA:pw' \
  go test -race -run TestMatrix -v ./pkg/plugin/
```

Grafana 10.4+ (backend + frontend plugin, alerting-capable).

## Features

- **SQL query editor** with table / time series output formats
- **Server-side macro expansion** — dashboards, Explore and alert rules
  evaluate identical SQL; macros are case-insensitive and quote-aware
  (occurrences inside string literals are never touched):

  | Macro | Expansion |
  |-------|-----------|
  | `$__timeFilter(col)` | `col >= 'from' AND col <= 'to'` (Firebird TIMESTAMP literals) |
  | `$__timeFilter(col, bias)` | same, with `bias` minutes subtracted — for servers storing local time with a fixed offset |
  | `$__timeFrom()` / `$__timeTo()` | the range bounds as TIMESTAMP literals |
  | `$__unixEpochFilter(col)` | `col >= from_epoch AND col <= to_epoch` (seconds) |
  | `$__unixEpochNanoFilter(col)` | same in nanoseconds |
  | `$__unixEpochFrom()` / `$__unixEpochTo()` | the bounds as epoch numbers |
  | `$__interval` | bucket width in **seconds** (numeric) |
  | `$__interval_ms` | bucket width in milliseconds |
  | `$__range(col)` | readable alias of `$__timeFilter(col)` |
  | `$__unixEpochGroup(col)` / `$__timeGroup[Alias](col)` | epoch-aligned GROUP BY bucket for time series (the equivalent of MySQL's `$__timeGroupAlias`) |

  Time-series example:

  ```sql
  SELECT $__unixEpochGroup(ORDERDATE, $__interval) AS bucket, COUNT(*) AS orders
  FROM TBLAUFTRAG
  WHERE $__timeFilter(ORDERDATE)
  GROUP BY 1 ORDER BY 1
  ```

  On Firebird the bucket expression is composed via `DATEDIFF(MINUTE)`/`DATEDIFF(SECOND)`
  so it does **not** overflow Firebird's 32-bit `DATEDIFF` return for dates far
  from the epoch (a real-world bug this plugin explicitly protects against).

- **Template variables** (Query type): the variable query returns either one
  column (value = label) or two columns (first = value, second = display text).
  Variable queries run through the same backend path — macros, `maxRows` and
  read-only checks apply.
- **Dashboard annotations**: annotation query with a mandatory `time` column
  and optional `text` and `tags` (comma-separated) columns; filter with
  `$__timeFilter(time)`.
- **Alerting**: alert rules evaluate the query on their schedule with the
  same timeout and macro semantics as dashboards.
- **Read-only enforcement** (defense in depth):
  - plugin level: only `SELECT`/`WITH` statements; no multi-statements, no
    `FOR UPDATE` / `WITH LOCK`; comment-stripping defeats comment-based
    smuggling
  - database level (recommended): connect with a SELECT-only user — see
    [below](#read-only-user). Note that selectable stored procedures can
    execute DML server-side even when called from `SELECT`, so grants matter
    more than statement checks.
- **Result cap** (`maxRows`, default 100000): protects the Grafana process
  from unbounded result sets on large tables.
- **Connection pool tuning** (max open/idle connections, lifetime, query
  timeout) with Firebird Classic awareness (Classic spawns one OS process
  per connection — keep pools small).

## Type mapping

| Firebird type | Grafana field |
|-----------------------------|----------------|
| SMALLINT / INTEGER / BIGINT | number (int64) |
| NUMERIC / DECIMAL (scale≠0) | number (float64) |
| FLOAT / DOUBLE / DECFLOAT | number (float64) |
| TIMESTAMP / DATE | time (UTC) |
| TIME | string |
| CHAR / VARCHAR / BLOB | string |

Timestamps are interpreted as UTC (Firebird has no timezone type). If your
server stores local time, either query with `$__timeFilter(col, bias)` or
account for the offset yourself.

## Installation

The CI pipeline produces a ready-to-use zip (`plugin.json`, frontend bundle,
backend binary with the required `gpx_<id>_<goos>_<goarch>` platform suffix).
To build it yourself:

```bash
npm ci && npm run build          # frontend -> dist/
GOOS=linux GOARCH=amd64 go build -o dist/gpx_grafana-firebird-datasource_linux_amd64 ./pkg
```

1. Extract/copy the plugin into the Grafana plugin directory:

   ```
   /var/lib/grafana/plugins/grafana-firebird-datasource/   # Linux
   ```

2. Allow loading the unsigned plugin (env on the Grafana container):

   ```
   GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=grafana-firebird-datasource
   ```

3. Restart Grafana and confirm the plugin is loaded:

   ```bash
   docker logs grafana 2>&1 | grep -i firebird   # "Plugin registered"
   ```

Rollback: remove the plugin directory and restart Grafana.

### Docker Compose example

```yaml
services:
  grafana:
    image: grafana/grafana:latest
    environment:
      - GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=grafana-firebird-datasource
    volumes:
      - ./grafana-firebird-datasource:/var/lib/grafana/plugins/grafana-firebird-datasource
```

## Configuration

| Field | Description | Default |
|----------------|--------------------------------------------------------------|---------|
| Host | Firebird server hostname/IP | — |
| Port | Firebird TCP port | 3050 |
| Database path | `.fdb` path **as seen by the server** (e.g. `D:/db/x.fdb` or `/data/x.fdb`) | — |
| User | use a SELECT-only account (see below) | — |
| Password | stored encrypted (secureJsonData) | — |
| Dialect | Firebird SQL dialect | 3 |
| Charset | one of `UTF8` (default), `WIN1252`, `WIN1251`, `ISO8859_1` | UTF8 |
| Max connections | keep low on Classic servers | 5 |
| Query timeout | per-query timeout in seconds | 30 |
| Max rows | result cap per query | 100000 |

<a id="read-only-user"></a>
## Recommended: read-only database user

Connect as SYSDBA once and create a dedicated SELECT-only user:

```sql
CREATE USER grafana_ro PASSWORD '<strong-password>';
GRANT SELECT ON MYTABLE TO grafana_ro;
-- or via a role:
CREATE ROLE GRAFANA_RO_ROLE;
GRANT SELECT ON MYTABLE TO GRAFANA_RO_ROLE;
GRANT GRAFANA_RO_ROLE TO grafana_ro;
```

Verify the grants (should list only `S`):

```sql
SELECT RDB$RELATION_NAME, RDB$PRIVILEGE FROM RDB$USER_PRIVILEGES
WHERE RDB$USER = 'GRAFANA_RO';
```

Do not grant the plugin's user any executable stored procedures that perform
DML.

## Performance notes

- Always constrain queries: `SELECT FIRST n ...` plus
  `WHERE $__timeFilter(date_column)`. Full scans over multi-million-row
  tables hurt both Grafana and the database server.
- Keep *Max connections* low; Firebird Classic creates an OS process per
  connection.
- Use indexed columns in `WHERE` clauses and inspect the query plan
  (`SET PLAN ON` in isql) for slow panels.

## Troubleshooting

| Symptom | Cause / fix |
|---------|-------------|
| `User unknown` in health check | wrong user, or auth plugin mismatch — Firebird 2.x defaults to `Legacy_Auth` |
| Plugin not listed after install | `plugin.json` missing from the plugin directory, or allowlist env not set |
| `cannot unmarshal object into ... backend of type bool` | stale `plugin.json` — `backend` must be a **bool**, `executable` on top level |
| `Refusing to initialize plugin because it's using Angular` | frontend bundle embeds `@grafana/*` (false-positive Angular detection) — build with webpack `externals` as in this repo |
| `fork/exec .../gpx_<id>_linux_amd64: no such file or directory` | unsigned backend plugins need the binary named with the platform suffix, e.g. `gpx_grafana-firebird-datasource_linux_amd64` |
| `Token unknown - $` (SQL error -104) | an un-expanded Grafana macro reached Firebird — macros are expanded server-side since 0.2.0; redeploy if you run an older build |
| Datasource missing from the **panel** datasource picker (but visible in Explore / config UI) | `plugin.json` needs `"metrics": true` — Grafana hides datasource plugins whose meta has none of `metrics`/`annotations`/`tracing`/`logs`/`alerting` |
| `Could not load plugin` on the datasource settings page | frontend bundle is not an AMD module — build with `library.type: 'amd'` and standard externals (`externalsType: 'window'` breaks: Grafana has no `window["@grafana/*"]` globals) |
| `result exceeded maxRows limit` | refine the query with `FIRST n` / `$__timeFilter` |
| `charset ... not allowed` | use one of the allowlisted charsets |
| Connection refused | wrong port, or Firebird not reachable from the Grafana host |
| Garbled text | charset mismatch between database content and configured charset |
| `numeric value is out of range` on time-series buckets | historical bug — bucket expressions now use `DATEDIFF(MINUTE)`; redeploy if you run a build older than 0.2.0 |

## Development

```bash
npm ci                # frontend deps
npm run build         # build frontend -> dist/
npm test              # Jest
npx tsc --noEmit      # typecheck
go test -race ./...   # backend unit tests (no DB needed)
```

Integration/matrix tests against live Firebird servers:

```bash
# single server (env-based)
FB_HOST=db-host FB_DATABASE=/db/x.fdb FB_USER=SYSDBA FB_PASSWORD=... \
  go test -race -run TestIntegration -v ./pkg/plugin/

# version matrix (2.5 / 3.0 / 4.0 / 5.0)
FB_MATRIX='v25@host:3050:/db/a.fdb:SYSDBA:pw,v50@host:3051:/db/b.fdb:SYSDBA:pw' \
  go test -race -run TestMatrix -v ./pkg/plugin/
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for details.

## License

Apache-2.0 — see [LICENSE](LICENSE). The Firebird logo is used under
"copyrighted free use" from the Firebird Foundation and is kept separate
from the code license — see [NOTICE](NOTICE).
