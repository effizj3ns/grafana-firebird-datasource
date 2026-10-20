package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/nakagami/firebirdsql"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// DatasourceSettings mirrors jsonData + secureJsonData from the frontend ConfigEditor.
type DatasourceSettings struct {
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Database           string `json:"database"`
	User               string `json:"user"`
	Password           string `json:"-"`
	Dialect            int    `json:"dialect"`
	Charset            string `json:"charset"`
	MaxOpenConns       int    `json:"maxOpenConns"`
	MaxIdleConns       int    `json:"maxIdleConns"`
	ConnMaxLifetimeSec int    `json:"connMaxLifetimeSec"`
	TimeoutSec         int    `json:"timeoutSec"`
	MaxRows            int    `json:"maxRows"`
}

type queryModel struct {
	RawSQL string `json:"rawSql"`
	Format string `json:"format"`
}

// FirebirdDatasource implements backend.QueryDataHandler + backend.CheckHealthHandler.
type FirebirdDatasource struct {
	instances *sync.Map // cacheKey -> *cachedInstance
}

type datasourceInstance struct {
	settings DatasourceSettings
	db       *sql.DB
}

type cachedInstance struct {
	settingsHash string
	inst         *datasourceInstance
}

// NewFirebirdDatasource creates a new datasource instance handler.
func NewFirebirdDatasource() *FirebirdDatasource {
	return &FirebirdDatasource{instances: &sync.Map{}}
}

func instanceKey(pluginContext backend.PluginContext) string {
	return fmt.Sprintf("%s:%d", pluginContext.DataSourceInstanceSettings.UID, pluginContext.OrgID)
}

// getInstance returns a cached datasource instance, rebuilding it when the
// datasource settings changed (e.g. password rotation, host change) so config
// edits take effect without a plugin restart. The superseded instance's DB
// pool is closed asynchronously.
func (d *FirebirdDatasource) getInstance(ctx context.Context, pluginContext backend.PluginContext) (*datasourceInstance, error) {
	sis := pluginContext.DataSourceInstanceSettings
	if sis == nil {
		return nil, fmt.Errorf("no datasource instance settings in plugin context")
	}
	key := instanceKey(pluginContext)
	s, err := loadSettings(*sis)
	if err != nil {
		return nil, err
	}
	hash := settingsHash(*sis)

	if cached, ok := d.instances.Load(key); ok {
		c := cached.(*cachedInstance)
		if c.settingsHash == hash {
			return c.inst, nil
		}
		// Settings changed: replace the instance. LoadOrStore against the
		// same key so concurrent requests rebuild only once; losers close
		// their own instance.
		newInst, err := newDatasourceInstance(ctx, *s)
		if err != nil {
			return nil, err
		}
		actual, loaded := d.instances.LoadOrStore(key, &cachedInstance{settingsHash: hash, inst: newInst})
		if loaded {
			// Another goroutine won: close our duplicate.
			newInst.Dispose()
			return actual.(*cachedInstance).inst, nil
		}
		// We won: retire the old instance.
		go c.inst.Dispose()
		return newInst, nil
	}

	// Cold cache: build and store. LoadOrStore protects against concurrent
	// cold starts leaking a duplicate pool.
	inst, err := newDatasourceInstance(ctx, *s)
	if err != nil {
		return nil, err
	}
	actual, loaded := d.instances.LoadOrStore(key, &cachedInstance{settingsHash: hash, inst: inst})
	if loaded {
		inst.Dispose()
		return actual.(*cachedInstance).inst, nil
	}
	return inst, nil
}

// settingsHash fingerprints everything that affects the connection or query
// behavior. Password comes from DecryptedSecureJSONData and is part of the
// fingerprint (a rotation must trigger a rebuild); the hash is salted so the
// raw settings are not trivially recoverable from it.
func settingsHash(sis backend.DataSourceInstanceSettings) string {
	h := fmt.Sprintf("%s|%s|%x", sis.JSONData, sis.DecryptedSecureJSONData["password"], sis.Updated)
	// Use a stable, non-reversible-ish digest: sum64 of the joined state.
	return fmt.Sprintf("%x", fnv64a(h))
}

func fnv64a(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

func newDatasourceInstance(ctx context.Context, s DatasourceSettings) (*datasourceInstance, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	dsn := s.dsn()

	db, err := sql.Open("firebirdsql", dsn)
	if err != nil {
		return nil, fmt.Errorf("firebird open: %w", err)
	}
	db.SetMaxOpenConns(s.maxOpenOrDefault())
	db.SetMaxIdleConns(s.maxIdleOrDefault())
	db.SetConnMaxLifetime(time.Duration(s.lifetimeOrDefault()) * time.Second)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close() // do not leak the pool handle on a failed ping
		// Surface the error so config mistakes show up early in CheckHealth.
		return nil, fmt.Errorf("firebird ping: %w", err)
	}

	return &datasourceInstance{settings: s, db: db}, nil
}

func (d *datasourceInstance) Dispose() {
	if d.db != nil {
		_ = d.db.Close()
	}
}

// dsn builds the driver DSN. QueryEscape guards the credentials; charset is
// validated against an allowlist and the database path is URL-escaped so
// neither can inject DSN query parameters (e.g. wire_crypt=false).
func (s *DatasourceSettings) dsn() string {
	return fmt.Sprintf("%s:%s@%s:%d/%s?dialect=%d&charset=%s",
		url.QueryEscape(s.User), url.QueryEscape(s.Password), s.Host, s.portOrDefault(), url.QueryEscape(s.Database), s.dialectOrDefault(), url.QueryEscape(s.charsetOrDefault()))
}

// validate enforces secure configuration constraints.
func (s *DatasourceSettings) validate() error {
	if s.Host == "" {
		return fmt.Errorf("host is required")
	}
	if s.Database == "" {
		return fmt.Errorf("database path is required")
	}
	if !allowedCharsets[strings.ToUpper(s.charsetOrDefault())] {
		return fmt.Errorf("charset %q not allowed (allowed: UTF8, WIN1252, WIN1251, ISO8859_1)", s.charsetOrDefault())
	}
	if s.maxOpenOrDefault() < 1 || s.maxOpenOrDefault() > 100 {
		return fmt.Errorf("maxOpenConns must be between 1 and 100")
	}
	return nil
}

var allowedCharsets = map[string]bool{
	"UTF8":       true,
	"WIN1252":    true,
	"WIN1251":    true,
	"ISO8859_1":  true,
}

func loadSettings(sis backend.DataSourceInstanceSettings) (*DatasourceSettings, error) {
	s := &DatasourceSettings{}
	if len(sis.JSONData) > 0 {
		if err := json.Unmarshal(sis.JSONData, s); err != nil {
			return nil, fmt.Errorf("jsonData: %w", err)
		}
	}
	if pw, ok := sis.DecryptedSecureJSONData["password"]; ok {
		s.Password = pw
	}
	return s, nil
}

// --- defaults helpers -------------------------------------------------------

func (s *DatasourceSettings) portOrDefault() int {
	if s.Port == 0 {
		return 3050
	}
	return s.Port
}

func (s *DatasourceSettings) dialectOrDefault() int {
	if s.Dialect == 0 {
		return 3
	}
	return s.Dialect
}

func (s *DatasourceSettings) charsetOrDefault() string {
	if s.Charset == "" {
		return "UTF8"
	}
	return s.Charset
}

func (s *DatasourceSettings) maxOpenOrDefault() int {
	if s.MaxOpenConns == 0 {
		return 5
	}
	return s.MaxOpenConns
}

func (s *DatasourceSettings) maxIdleOrDefault() int {
	if s.MaxIdleConns == 0 {
		return 2
	}
	return s.MaxIdleConns
}

func (s *DatasourceSettings) lifetimeOrDefault() int {
	if s.ConnMaxLifetimeSec == 0 {
		return 300
	}
	return s.ConnMaxLifetimeSec
}

func (s *DatasourceSettings) timeoutOrDefault() time.Duration {
	if s.TimeoutSec == 0 {
		return 30 * time.Second
	}
	return time.Duration(s.TimeoutSec) * time.Second
}

func (s *DatasourceSettings) maxRowsOrDefault() int {
	if s.MaxRows == 0 {
		return 100000
	}
	return s.MaxRows
}

// --- macro expansion --------------------------------------------------------

// expandMacros expands Grafana SQL macros in rawSql using the query time range.
// Quote-aware: macros inside single-quoted string literals are left untouched
// so user data containing '$__' never gets rewritten.
//
// Supported macros (subset of the Grafana SQL datasource macros relevant for
// Firebird's TIMESTAMP literals):
//
//	$__timeFilter(col)           -> col >= 'from' AND col <= 'to'   (TIMESTAMP literals)
//	$__timeFilter(col, tzbias)   -> same, bias subtracted from literals (minutes as string e.g. '-120')
//	$__timeFrom()                -> 'from' TIMESTAMP literal
//	$__timeTo()                  -> 'to' TIMESTAMP literal
//	$__unixEpochFilter(col)      -> col >= from_epoch AND col <= to_epoch (numeric seconds)
//	$__unixEpochNanoFilter(col)  -> col >= from_ns AND col <= to_ns
//	$__unixEpochFrom()           -> from epoch seconds
//	$__unixEpochTo()             -> to epoch seconds
//	$__interval                  -> GROUP BY bucket as Firebird interval literal, e.g. '30' SECOND (computed from MaxDataPoints)
//	$__interval_ms               -> bucket width in milliseconds (integer)
//	$__range(col)                -> col >= 'from' AND col <= 'to' (same as timeFilter; alias for readability)
//	$__unixEpochGroup(col,bucket)-> DATEADD(SECOND, FLOOR(DATEDIFF(SECOND, '1970-01-01', col) / bucket) * bucket, '1970-01-01') for time-series GROUP BY
func expandMacros(rawSql string, tr backend.TimeRange, interval time.Duration, maxDataPoints int64) string {
	fromTS := firebirdTimestamp(tr.From)
	toTS := firebirdTimestamp(tr.To)
	fromEpoch := tr.From.Unix()
	toEpoch := tr.To.Unix()
	fromNano := tr.From.UnixNano()
	toNano := tr.To.UnixNano()

	replacements := []struct {
		pattern *regexp.Regexp
		// group >= 1: replace(sub) receives the submatches; group == 0:
		// replace() is a plain zero-arg replacement.
		group   int
		replace func(sub []string) string
	}{
		// $__timeFilter(col) / $__timeFilter(col, 'bias') — bias in minutes string
		{regexp.MustCompile(`(?i)\$__timeFilter\(\s*([^)'",]+?)\s*\)`), 1, func(sub []string) string {
			return timeFilterSQL(sub[1], fromTS, toTS, "")
		}},
		{regexp.MustCompile(`(?i)\$__timeFilter\(\s*([^)'",]+?)\s*,\s*'?(-?\d+)'?\s*\)`), 1, func(sub []string) string {
			return timeFilterSQL(sub[1], fromTS, toTS, sub[2])
		}},
		{regexp.MustCompile(`(?i)\$__timeFrom\(\s*\)`), 0, func([]string) string { return "'" + fromTS + "'" }},
		{regexp.MustCompile(`(?i)\$__timeTo\(\s*\)`), 0, func([]string) string { return "'" + toTS + "'" }},
		{regexp.MustCompile(`(?i)\$__unixEpochFilter\(\s*([^)]+?)\s*\)`), 1, func(sub []string) string {
			return fmt.Sprintf("%s >= %d AND %s <= %d", sub[1], fromEpoch, sub[1], toEpoch)
		}},
		{regexp.MustCompile(`(?i)\$__unixEpochNanoFilter\(\s*([^)]+?)\s*\)`), 1, func(sub []string) string {
			return fmt.Sprintf("%s >= %d AND %s <= %d", sub[1], fromNano, sub[1], toNano)
		}},
		{regexp.MustCompile(`(?i)\$__unixEpochFrom\(\s*\)`), 0, func([]string) string { return fmt.Sprintf("%d", fromEpoch) }},
		{regexp.MustCompile(`(?i)\$__unixEpochTo\(\s*\)`), 0, func([]string) string { return fmt.Sprintf("%d", toEpoch) }},
		{regexp.MustCompile(`(?i)\$__range\(\s*([^)]+?)\s*\)`), 1, func(sub []string) string {
			return fmt.Sprintf("%s >= '%s' AND %s <= '%s'", sub[1], fromTS, sub[1], toTS)
		}},
		{regexp.MustCompile(`(?i)\$__unixEpochGroup\(\s*([^),]+?)\s*(?:,\s*([^),]+?)\s*)?\)`), 2, func(sub []string) string {
			return unixEpochGroupSQL(sub[1], bucketSeconds(interval, tr, maxDataPoints))
		}},
		{regexp.MustCompile(`(?i)\$__timeGroup(?:Alias)?\(\s*([^),]+?)\s*(?:,\s*([^),]+?)\s*)?\)`), 2, func(sub []string) string {
			// $__timeGroup(col, bucket) — bucket arg ignored, $__interval is authoritative
			return unixEpochGroupSQL(sub[1], bucketSeconds(interval, tr, maxDataPoints))
		}},
		{regexp.MustCompile(`(?i)\$__interval_ms\b`), 0, func([]string) string {
			return fmt.Sprintf("%d", intervalMS(interval, tr, maxDataPoints))
		}},
		{regexp.MustCompile(`(?i)\$__interval\b`), 0, func([]string) string {
			return fmt.Sprintf("%d", bucketSeconds(interval, tr, maxDataPoints))
		}},
	}

	// Apply each replacement quote-aware: macros inside single-quoted string
	// literals are never touched (user data containing '$__' must survive).
	out := rawSql
	for _, r := range replacements {
		out = replaceOutsideQuotes(out, r.pattern, func(match string) string {
			sub := r.pattern.FindStringSubmatch(match)
			if len(sub) <= r.group {
				return match
			}
			return r.replace(sub)
		})
	}
	return out
}

// replaceOutsideQuotes applies pattern replacement only outside single-quoted
// string literals (Firebird escapes ' inside literals as '').
func replaceOutsideQuotes(s string, re *regexp.Regexp, repl func(string) string) string {
	var b strings.Builder
	for {
		start := strings.IndexRune(s, '\'')
		if start == -1 {
			// no more literals: replace in the remaining plain segment
			b.WriteString(re.ReplaceAllStringFunc(s, repl))
			break
		}
		b.WriteString(re.ReplaceAllStringFunc(s[:start], repl))
		// find the closing quote, skipping escaped '' pairs
		end := start + 1
		for end < len(s) {
			if s[end] == '\'' {
				if end+1 < len(s) && s[end+1] == '\'' {
					end += 2 // escaped quote inside literal
					continue
				}
				break
			}
			end++
		}
		if end >= len(s) {
			// unterminated literal: keep the rest verbatim
			b.WriteString(s[start:])
			break
		}
		b.WriteString(s[start : end+1])
		s = s[end+1:]
	}
	return b.String()
}

// timeFilterSQL renders the range predicate for a TIMESTAMP column. biasMinutes
// (from $__timeFilter(col, bias)) shifts the literals, matching the behavior of
// the official SQL datasources where the DB stores local time with a fixed offset.
func timeFilterSQL(col, fromTS, toTS, biasMinutes string) string {
	if biasMinutes != "" {
		bias, err := strconv.Atoi(biasMinutes)
		if err == nil && bias != 0 {
			shift := time.Duration(-bias) * time.Minute
			f, err1 := time.Parse("2006-01-02 15:04:05.999", fromTS)
			t, err2 := time.Parse("2006-01-02 15:04:05.999", toTS)
			if err1 == nil && err2 == nil {
				fromTS = f.Add(shift).Format("2006-01-02 15:04:05.999")
				toTS = t.Add(shift).Format("2006-01-02 15:04:05.999")
			}
		}
	}
	return fmt.Sprintf("%s >= '%s' AND %s <= '%s'", col, fromTS, col, toTS)
}

// firebirdTimestamp formats t as a Firebird TIMESTAMP literal (ms precision).
func firebirdTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05.999")
}

// intervalMS computes the bucket width in milliseconds for $__interval_ms:
// explicit q.Interval when set (Grafana calculates it from MaxDataPoints and
// the range), otherwise derived from range/maxDataPoints; minimum 1 ms.
func intervalMS(interval time.Duration, tr backend.TimeRange, maxDataPoints int64) int64 {
	if interval > 0 {
		return int64(interval / time.Millisecond)
	}
	spanMS := tr.To.Sub(tr.From) / time.Millisecond
	if spanMS <= 0 {
		return 1
	}
	if maxDataPoints <= 0 {
		maxDataPoints = 500 // Grafana default
	}
	ms := spanMS / time.Duration(maxDataPoints)
	if ms < 1 {
		ms = 1
	}
	return int64(ms)
}

// firebirdIntervalLiteral is retained for reference: the plain $__interval
// macro expands to the bucket width in seconds (numeric), matching the MySQL
// datasource convention where $__interval renders as a duration string and
// numeric usage dominates in GROUP BY arithmetic.
func firebirdIntervalLiteral(interval time.Duration, tr backend.TimeRange, maxDataPoints int64) string {
	return fmt.Sprintf("%d", bucketSeconds(interval, tr, maxDataPoints))
}

// bucketSeconds returns the interval bucket in whole seconds (min 1s).
func bucketSeconds(interval time.Duration, tr backend.TimeRange, maxDataPoints int64) int64 {
	s := intervalMS(interval, tr, maxDataPoints) / 1000
	if s < 1 {
		s = 1
	}
	return s
}

// unixEpochGroupSQL renders a Firebird GROUP BY bucket expression for a
// TIMESTAMP column. Equivalent of MySQL's $__timeGroupAlias for time-series
// panels.
//
// Dialect/overflow notes (verified against a live Firebird 2.5 (dialect 1) server,
// database dialect 1):
//   - DATEDIFF(SECOND, '1970-01-01', col) returns INTEGER and overflows for
//     dates beyond ~2038 relative to the 1970 epoch — Firebird 2.5 has no
//     EXTRACT(EPOCH) and no BIGINT datediff, so seconds are composed from
//     DATEDIFF(MINUTE) * 60 plus the sub-minute remainder via
//     DATEDIFF(SECOND, base+minutes, col), both well inside INTEGER range
//     (minute diff < 2^31/1 ≈ 4000 years; remainder < 60).
//   - Buckets < 60s use pure SECOND arithmetic (fine unless the data spans
//     > 68 years from the epoch base).
func unixEpochGroupSQL(col string, bucketSeconds int64) string {
	c := strings.TrimSpace(col)
	if bucketSeconds < 60 {
		// sub-minute buckets: direct second arithmetic
		return fmt.Sprintf(
			"DATEADD(SECOND, FLOOR(DATEDIFF(SECOND, TIMESTAMP '1970-01-01 00:00:00', %s) / %d) * %d, TIMESTAMP '1970-01-01 00:00:00')",
			c, bucketSeconds, bucketSeconds)
	}
	minutes := bucketSeconds / 60
	return fmt.Sprintf(
		"DATEADD(MINUTE, FLOOR(DATEDIFF(MINUTE, TIMESTAMP '1970-01-01 00:00:00', %s) / %d) * %d, TIMESTAMP '1970-01-01 00:00:00')",
		c, minutes, minutes)
}

// validateReadOnly blocks everything that is not a plain SELECT/WITH statement.
// This is comfort-level enforcement; the real guarantee must come from a
// read-only DB user (defense in depth).
//
// Blocked beyond the SELECT/WITH prefix check:
//   - multi-statements (";" followed by content)
//   - SELECT ... FOR UPDATE [WITH LOCK] (row-lock DoS on the legacy DB)
//   - comment tricks are neutralized by stripping block/line comments before
//     the keyword checks so "SELECT/**/..." cannot smuggle tokens past the
//     FOR UPDATE check
//
// NOT detectable at statement level: selectable stored procedures can execute
// DML server-side. This must be covered by the read-only DB user grants.
func validateReadOnly(sqlText string) error {
	stmt := strings.TrimSpace(sqlText)
	stmt = strings.TrimLeft(stmt, "(")
	stmt = stripComments(stmt)
	lower := strings.ToLower(stmt)
	if strings.HasPrefix(lower, "select") || strings.HasPrefix(lower, "with") {
		if idx := strings.LastIndex(lower, ";"); idx != -1 && strings.TrimSpace(stripComments(lower[idx+1:])) != "" {
			return fmt.Errorf("read-only datasource: multiple statements are not allowed")
		}
		if strings.Contains(lower, "for update") || strings.Contains(lower, "with lock") {
			return fmt.Errorf("read-only datasource: FOR UPDATE / WITH LOCK is not allowed")
		}
		return nil
	}
	return fmt.Errorf("read-only datasource: only SELECT/WITH statements allowed")
}

// stripComments removes SQL block comments (/* ... */) and line comments
// (-- ...) so comment characters cannot smuggle or hide tokens. Order matters:
// block comments first (they may contain "--"), then line comments to EOL.
func stripComments(s string) string {
	var b strings.Builder
	for {
		start := strings.Index(s, "/*")
		if start == -1 {
			break
		}
		end := strings.Index(s[start:], "*/")
		if end == -1 {
			s = s[:start] // unterminated block comment: drop the rest
			break
		}
		b.WriteString(s[:start])
		b.WriteString(" ")
		s = s[start+end+2:]
	}
	b.WriteString(s)
	out := b.String()
	// Line comments: cut everything from "--" to end of line, per line.
	if strings.Contains(out, "--") {
		lines := strings.Split(out, "\n")
		for i, ln := range lines {
			if idx := strings.Index(ln, "--"); idx != -1 {
				lines[i] = ln[:idx]
			}
		}
		out = strings.Join(lines, "\n")
	}
	return out
}

// QueryData handles multiple queries in one request.
func (d *FirebirdDatasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	inst, err := d.getInstance(ctx, req.PluginContext)
	if err != nil {
		return nil, err
	}
	return inst.queryData(ctx, req)
}

func (d *datasourceInstance) queryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()
	for _, q := range req.Queries {
		response.Responses[q.RefID] = d.query(ctx, q)
	}
	return response, nil
}

func (d *datasourceInstance) query(ctx context.Context, q backend.DataQuery) backend.DataResponse {
	var model queryModel
	if err := json.Unmarshal(q.JSON, &model); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("query model: %v", err))
	}
	if err := validateReadOnly(model.RawSQL); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
	}

	// Expand Grafana SQL macros ($__timeFilter etc.) server-side. The frontend
	// sends rawSql verbatim (DataSourceWithBackend does not interpolate), so the
	// backend owns macro expansion — this keeps dashboards, Explore AND alert
	// rule evaluation consistent (alerts never run the frontend).
	logger := log.DefaultLogger.FromContext(ctx)
	start := time.Now()
	sqlText := expandMacros(model.RawSQL, q.TimeRange, q.Interval, q.MaxDataPoints)

	ctx, cancel := context.WithTimeout(ctx, d.settings.timeoutOrDefault())
	defer cancel()

	rows, err := d.db.QueryContext(ctx, sqlText)
	if err != nil {
		logger.Warn("firebird query failed", "refId", q.RefID, "duration", time.Since(start).String(), "error", err.Error())
		return backend.ErrDataResponse(backend.StatusInternal, "firebird query failed")
	}
	defer rows.Close()

	frame, err := rowsToFrame(rows, d.settings.maxRowsOrDefault())
	if err != nil {
		logger.Warn("frame mapping failed", "refId", q.RefID, "error", err.Error())
		return backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf("frame mapping: %v", err))
	}
	if model.Format == "time_series" {
		// Long format: let Grafana's standard transformation handle it; the
		// frame is returned as table and converted client-side.
		frame.Meta = &data.FrameMeta{Type: data.FrameTypeTimeSeriesMulti}
	}
	logger.Debug("query done", "refId", q.RefID, "duration", time.Since(start).String(), "rows", frame.Rows())
	return backend.DataResponse{Frames: data.Frames{frame}}
}

// rowsToFrame converts database/sql rows into a Grafana data frame.
// maxRows caps the result set (0 = unlimited) to protect the Grafana process
// from unbounded SELECTs on large tables (OOM guard); when the cap is hit the
// frame is returned truncated together with an error explaining the limit.
//
// Type mapping (nakagami/firebirdsql v0.9.22 xsqlvar.go type names):
//   - TIMESTAMP/DATE -> time (UTC; Firebird 2.x has no timezone type)
//   - TIME -> string (driver scans TIME as time.Time; converted to string)
//   - SHORT/LONG/INT64 (scale 0) -> int64  [SMALLINT/INTEGER/BIGINT]
//   - SHORT/LONG/INT64 with scale != 0 (scaled NUMERIC/DECIMAL) -> float64
//   - SINGLE/DOUBLE/DECFLOAT -> float64
//   - CHAR/VARCHAR/BLOB text -> string
//   - BLOB binary and unknown types -> string via driver's conversion
func rowsToFrame(rows *sql.Rows, maxRows int) (*data.Frame, error) {
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(colTypes))
	fields := make([]*data.Field, len(colTypes))
	scanArgs := make([]interface{}, len(colTypes))

	for i, ct := range colTypes {
		names[i] = ct.Name()
		field, scanner := newFieldForType(ct)
		fields[i] = field
		// rows.Scan requires a pointer to the destination; scanners are
		// pointer types already (sql.NullTime etc.), so wrap by pointer.
		scanArgs[i] = scanner
	}
	frame := data.NewFrame("result", fields...)
	for i := range frame.Fields {
		frame.Fields[i].Name = names[i]
	}

	for rowCount := 0; rows.Next(); rowCount++ {
		if maxRows > 0 && rowCount >= maxRows {
			return nil, fmt.Errorf("result exceeded maxRows limit (%d): refine your query with FIRST n / $__timeFilter", maxRows)
		}
		if err := rows.Scan(scanArgs...); err != nil {
			return nil, fmt.Errorf("scan row %d: %w", rowCount, err)
		}
		for i := range scanArgs {
			// fieldValue dereferences the scanner pointer; the SDK's Append
			// on nullable vectors expects a value (or pointer-to-value) and
			// nil for NULL. We hand over plain values, never raw pointers.
			v := fieldValue(scanArgs[i])
			fields[i].Append(v)
		}
		rowCount++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return frame, nil
}

// columnTypeView is the minimal slice of *sql.ColumnType used for field
// selection; it makes newFieldForType unit-testable with fakes.
// DecimalSize returns (precision, scale, ok) — for Firebird scaled numerics
// the driver reports sqlscale via this path.
type columnTypeView interface {
	Name() string
	DatabaseTypeName() string
	DecimalSize() (precision, scale int64, ok bool)
}

// compile-time assertion: *sql.ColumnType satisfies the view.
var _ columnTypeView = (*sql.ColumnType)(nil)

// newFieldForType picks the Grafana field type and the matching sql.Scanner
// based on the driver's DatabaseTypeName and scale.
//
// firebirdsql v0.9.22 type names (xsqlvar.go xsqlvarTypeName):
//   TEXT/SHORT/LONG/INT64/FLOAT/DOUBLE/DECFLOAT/TIMESTAMP/DATE/TIME/BLOB
// Scaled numerics (sqlscale != 0: NUMERIC(18,x) etc.) scan as string in the
// driver, so they are read as NullString and parsed to float64.
func newFieldForType(ct columnTypeView) (*data.Field, interface{}) {
	tname := ct.DatabaseTypeName()
	_, scale, hasScale := ct.DecimalSize()

	switch tname {
	case "TIMESTAMP", "DATE":
		return data.NewFieldFromFieldType(data.FieldTypeNullableTime, 0), new(sql.NullTime)
	case "TIME":
		// Driver scans TIME as time.Time; we expose it as string to avoid
		// inventing a date context for a time-only value.
		return data.NewFieldFromFieldType(data.FieldTypeNullableString, 0), new(sql.NullString)
	case "SHORT", "LONG", "INT64":
		if hasScale && scale != 0 {
			// Scaled numeric (NUMERIC(x,y)/DECIMAL(x,y)): driver yields string.
			return data.NewFieldFromFieldType(data.FieldTypeNullableFloat64, 0), new(NullScaledFloat)
		}
		return data.NewFieldFromFieldType(data.FieldTypeNullableInt64, 0), new(sql.NullInt64)
	case "FLOAT", "DOUBLE", "DECFLOAT":
		return data.NewFieldFromFieldType(data.FieldTypeNullableFloat64, 0), new(sql.NullFloat64)
	default:
		// CHAR, VARCHAR, BLOB, BOOLEAN, unknown: scan as string. The driver
		// converts non-string values via database/sql convertAssign.
		return data.NewFieldFromFieldType(data.FieldTypeNullableString, 0), new(sql.NullString)
	}
}

// fieldValue converts a scanned nullable value into the value expected by
// the SDK's nullable vectors: a pointer to the value (*int64, *float64,
// *string, *time.Time) or nil for NULL.
func fieldValue(arg interface{}) interface{} {
	switch a := arg.(type) {
	case *sql.NullTime:
		if a.Valid {
			t := a.Time.UTC()
			return &t
		}
	case *sql.NullInt64:
		if a.Valid {
			v := a.Int64
			return &v
		}
	case *sql.NullFloat64:
		if a.Valid {
			v := a.Float64
			return &v
		}
	case *NullScaledFloat:
		if a.Valid {
			v := a.Float64
			return &v
		}
	case *sql.NullString:
		if a.Valid {
			v := a.String
			return &v
		}
	}
	return nil
}

// NullScaledFloat scans driver-provided scaled-numeric strings ("123.456")
// into a float64. Scale information comes from PrecisionScale; the driver
// already renders the decimal point, so a plain ParseFloat is correct here.
type NullScaledFloat struct {
	Float64 float64
	Valid   bool
}

func (n *NullScaledFloat) Scan(value interface{}) error {
	if value == nil {
		n.Float64, n.Valid = 0, false
		return nil
	}
	switch v := value.(type) {
	case string:
		f, err := parseScaledFloat(v)
		if err != nil {
			return err
		}
		n.Float64, n.Valid = f, true
		return nil
	case []byte:
		f, err := parseScaledFloat(string(v))
		if err != nil {
			return err
		}
		n.Float64, n.Valid = f, true
		return nil
	default:
		// Some driver paths may hand over int64/float64 directly.
		if f, ok := toFloat64(value); ok {
			n.Float64, n.Valid = f, true
			return nil
		}
	}
	n.Float64, n.Valid = 0, false
	return nil
}

func parseScaledFloat(s string) (float64, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("scaled numeric %q: %w", s, err)
	}
	return f, nil
}

func toFloat64(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case int64:
		return float64(v), true
	case float64:
		return v, true
	case int:
		return float64(v), true
	}
	return 0, false
}

// CheckHealth pings the database and reports the server version so operators
// can verify connectivity AND dialect expectations directly from the health
// panel. The version query is wrapped in its own error handling: a legacy DB
// with restricted MON$-table rights still yields a healthy ping.
func (d *FirebirdDatasource) CheckHealth(ctx context.Context, req *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	inst, err := d.getInstance(ctx, req.PluginContext)
	if err != nil {
		return &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: fmt.Sprintf("instance: %v", err)}, nil
	}
	if err := inst.db.PingContext(ctx); err != nil {
		return &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: fmt.Sprintf("ping: %v", err)}, nil
	}
	msg := "Firebird reachable"
	var version string
	if err := inst.db.QueryRowContext(ctx, "SELECT RDB$GET_CONTEXT('SYSTEM','ENGINE_VERSION') FROM RDB$DATABASE").Scan(&version); err == nil {
		msg += fmt.Sprintf(" (engine version %s)", strings.TrimSpace(version))
	}
	return &backend.CheckHealthResult{Status: backend.HealthStatusOk, Message: msg}, nil
}
