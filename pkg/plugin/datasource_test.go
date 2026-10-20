package plugin

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"database/sql/driver"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

func TestValidateReadOnly(t *testing.T) {
	valid := []string{
		"SELECT * FROM TBLAUFTRAG",
		"  select FIRST 10 * FROM TBLTOUR",
		"WITH cte AS (SELECT AUFTRAGNUMMER FROM TBLAUFTRAG) SELECT * FROM cte",
		"with cte as (select 1 from rdb$database) select * from cte",
		"(SELECT 1 FROM RDB$DATABASE)",
		"SELECT /* comment */ * FROM T",
		"SELECT A -- trailing comment\nFROM T",
	}
	for _, q := range valid {
		if err := validateReadOnly(q); err != nil {
			t.Errorf("expected valid: %q, got: %v", q, err)
		}
	}

	invalid := []struct {
		name string
		q    string
	}{
		{"delete", "DELETE FROM TBLAUFTRAG"},
		{"drop", "DROP TABLE TBLAUFTRAG"},
		{"insert", "INSERT INTO T VALUES (1)"},
		{"update", "UPDATE T SET A=1"},
		{"execute block", "EXECUTE BLOCK AS BEGIN INSERT INTO T VALUES (1); END"},
		{"multi statement", "SELECT 1 FROM RDB$DATABASE; DROP TABLE T"},
		{"for update", "SELECT * FROM T FOR UPDATE"},
		{"for update with lock", "SELECT * FROM T FOR UPDATE WITH LOCK"},
		{"with lock hidden in comment space", "SELECT * FROM T /*x*/ FOR UPDATE"},
		{"trailing semicolon with content", "SELECT 1 FROM RDB$DATABASE; DROP TABLE T"},
		{"leading semicolon", "; SELECT 1 FROM RDB$DATABASE"},
	}
	for _, tc := range invalid {
		if err := validateReadOnly(tc.q); err == nil {
			t.Errorf("expected reject (%s): %q", tc.name, tc.q)
		}
	}
}

func TestStripComments(t *testing.T) {
	cases := []struct{ in, want string }{
		{"SELECT /* x */ 1", "SELECT   1"},
		{"SELECT 1 -- foo", "SELECT 1 "},
		{"SELECT /* -- nested */ 1", "SELECT   1"},
		{"SELECT /* unterminated", "SELECT "},
	}
	for _, c := range cases {
		if got := stripComments(c.in); got != c.want {
			t.Errorf("stripComments(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- fakeRows: minimal driver.Rows driver over in-memory rows ---------------

type fakeColumn struct {
	name    string
	typName string
	prec    int64
	scale   int64
	hasPS   bool
}

type fakeRows struct {
	cols   []fakeColumn
	values [][]driverValue
	pos    int
}

type driverValue = driver.Value

var errFakeClosed = errors.New("fake rows closed")

func (r *fakeRows) Columns() []string {
	out := make([]string, len(r.cols))
	for i, c := range r.cols {
		out[i] = c.name
	}
	return out
}
func (r *fakeRows) Close() error { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.pos])
	r.pos++
	return nil
}

// fakeColumnType wraps a driver.ColumnType for our fake rows.
// database/sql.Rows.ColumnTypes() requires a real *sql.Rows built from a
// driver connection, so instead of driving sql.Rows directly we test the
// field-selection logic (newFieldForType) separately from the row-scanning
// loop (via NullScaledFloat + fieldValue). Integration test covers the real
// driver path end-to-end.
func TestNewFieldForTypeMapping(t *testing.T) {
	cases := []struct {
		typName   string
		scale     int64
		hasScale  bool
		wantField data.FieldType
	}{
		{"SHORT", 0, false, data.FieldTypeNullableInt64},
		{"LONG", 0, false, data.FieldTypeNullableInt64},
		{"INT64", 0, false, data.FieldTypeNullableInt64},
		{"INT64", -4, true, data.FieldTypeNullableFloat64}, // NUMERIC(18,4)
		{"SHORT", -2, true, data.FieldTypeNullableFloat64}, // NUMERIC(9,2)
		{"FLOAT", 0, false, data.FieldTypeNullableFloat64},
		{"DOUBLE", 0, false, data.FieldTypeNullableFloat64},
		{"DECFLOAT", 0, false, data.FieldTypeNullableFloat64},
		{"TIMESTAMP", 0, false, data.FieldTypeNullableTime},
		{"DATE", 0, false, data.FieldTypeNullableTime},
		{"TIME", 0, false, data.FieldTypeNullableString},
		{"VARCHAR", 0, false, data.FieldTypeNullableString},
		{"BLOB", 0, false, data.FieldTypeNullableString},
		{"WEIRD", 0, false, data.FieldTypeNullableString},
	}
	for _, c := range cases {
		ct := &columnTypeFake{typName: c.typName, scale: c.scale, hasPS: c.hasScale}
		f, scanner := newFieldForType(ct)
		if f.Type() != c.wantField {
			t.Errorf("type %s scale %d: got %v, want %v", c.typName, c.scale, f.Type(), c.wantField)
		}
		if scanner == nil {
			t.Errorf("type %s: nil scanner", c.typName)
		}
	}
}

// columnTypeFake implements columnTypeView.
type columnTypeFake struct {
	name    string
	typName string
	prec    int64
	scale   int64
	hasPS   bool
}

func (c *columnTypeFake) Name() string             { return c.name }
func (c *columnTypeFake) DatabaseTypeName() string { return c.typName }
func (c *columnTypeFake) DecimalSize() (precision, scale int64, ok bool) {
	return c.prec, c.scale, c.hasPS
}

func TestNullScaledFloatScan(t *testing.T) {
	var n NullScaledFloat
	if err := n.Scan(nil); err != nil || n.Valid {
		t.Fatalf("nil scan: %v %v", err, n.Valid)
	}
	if err := n.Scan("123.456"); err != nil || n.Float64 != 123.456 || !n.Valid {
		t.Fatalf("string scan: %v %v %v", err, n.Float64, n.Valid)
	}
	if err := n.Scan([]byte("42.5")); err != nil || n.Float64 != 42.5 {
		t.Fatalf("bytes scan: %v %v", err, n.Float64)
	}
	if err := n.Scan(int64(7)); err != nil || n.Float64 != 7 {
		t.Fatalf("int64 scan: %v %v", err, n.Float64)
	}
	if err := n.Scan("notanumber"); err == nil {
		t.Fatal("expected error for garbage string")
	}
}

func TestSettingsHashChangesOnPassword(t *testing.T) {
	a := backend.DataSourceInstanceSettings{
		JSONData:                 []byte(`{"host":"h"}`),
		DecryptedSecureJSONData:  map[string]string{"password": "a"},
	}
	b := a
	b.DecryptedSecureJSONData = map[string]string{"password": "b"}
	if settingsHash(a) == settingsHash(b) {
		t.Fatal("password change must change the settings hash")
	}
}

func TestSettingsValidate(t *testing.T) {
	s := DatasourceSettings{Host: "h", Database: "db", Charset: "UTF8&wire_crypt=false"}
	if err := s.validate(); err == nil {
		t.Fatal("charset injection must be rejected")
	}
	s.Charset = "utf8"
	if err := s.validate(); err != nil {
		t.Fatalf("utf8 must be allowed: %v", err)
	}
	s.Charset = "WIN1252"
	if err := s.validate(); err != nil {
		t.Fatalf("win1252 must be allowed: %v", err)
	}
}

func TestDSNEscaping(t *testing.T) {
	s := DatasourceSettings{
		Host: "db.local", Port: 3050, Database: "D:/data db/EXAMPLE.fdb",
		User: "us er", Password: "p@ss:word", Charset: "UTF8",
	}
	d := s.dsn()
	// Credentials and database must be escaped so metacharacters cannot
	// alter the DSN structure (user:pass@host:port/db?query).
	if strings.Contains(d, "p@ss:word@") || strings.Contains(d, "us er:") {
		t.Fatalf("credentials not escaped: %s", d)
	}
	// database path escaped (no raw slashes/colons outside the query part)
	if strings.Contains(d, "/D:/") || strings.Contains(d, "data db/") {
		t.Fatalf("database path not escaped: %s", d)
	}
	if !strings.HasPrefix(d, "us+er:p%40ss%3Aword@db.local:3050/D%3A%2Fdata+db%2FEXAMPLE.fdb") {
		t.Fatalf("unexpected DSN: %s", d)
	}
}

// --- end of unit tests -------------------------------------------------------
// rowsToFrame itself is covered end-to-end by the integration test
// (integration_test.go, env-gated) against the real Firebird driver, plus
// by newFieldForTypeMapping + NullScaledFloatScan for the mapping logic.

func TestQueryModelUnmarshal(t *testing.T) {
	var m queryModel
	if err := json.Unmarshal([]byte(`{"rawSql":"SELECT 1","format":"table"}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.RawSQL != "SELECT 1" || m.Format != "table" {
		t.Fatalf("unexpected model: %+v", m)
	}
}

// --- macro tests ------------------------------------------------------------

func TestExpandMacrosTimeFilter(t *testing.T) {
	from := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	to := time.Date(2024, 6, 2, 12, 0, 0, 0, time.UTC)
	tr := backend.TimeRange{From: from, To: to}

	cases := []struct{ name, in, want string }{
		{
			name: "timeFilter basic",
			in:   "SELECT * FROM T WHERE $__timeFilter(ERFDATUM)",
			want: "SELECT * FROM T WHERE ERFDATUM >= '2024-06-01 12:00:00' AND ERFDATUM <= '2024-06-02 12:00:00'",
		},
		{
			name: "timeFilter case-insensitive column",
			in:   "select x from y where $__timefilter(erfdatum)",
			want: "select x from y where erfdatum >= '2024-06-01 12:00:00' AND erfdatum <= '2024-06-02 12:00:00'",
		},
		{
			name: "timeFilter with tz bias",
			in:   "SELECT * FROM T WHERE $__timeFilter(ERFDATUM, -120)",
			want: "SELECT * FROM T WHERE ERFDATUM >= '2024-06-01 14:00:00' AND ERFDATUM <= '2024-06-02 14:00:00'",
		},
		{
			name: "timeFrom/timeTo",
			in:   "SELECT * FROM T WHERE T >= $__timeFrom() AND T <= $__timeTo()",
			want: "SELECT * FROM T WHERE T >= '2024-06-01 12:00:00' AND T <= '2024-06-02 12:00:00'",
		},
		{
			name: "unixEpochFilter",
			in:   "SELECT * FROM T WHERE $__unixEpochFilter(EPOCH_COL)",
			want: "SELECT * FROM T WHERE EPOCH_COL >= 1717243200 AND EPOCH_COL <= 1717329600",
		},
		{
			name: "macro inside string literal stays untouched",
			in:   "SELECT * FROM T WHERE note = 'use $__timeFilter(X) here'",
			want: "SELECT * FROM T WHERE note = 'use $__timeFilter(X) here'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := expandMacros(tc.in, tr, 0, 0)
			if got != tc.want {
				t.Errorf("expandMacros()\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

func TestExpandMacrosInterval(t *testing.T) {
	from := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 6, 2, 0, 0, 0, 0, time.UTC)
	tr := backend.TimeRange{From: from, To: to}
	// 24h range, 500 points -> 172800ms = 172s bucket
	got := expandMacros("SELECT COUNT(*) FROM T WHERE $__timeFilter(T) GROUP BY $__interval", tr, 0, 500)
	if !strings.Contains(got, "GROUP BY 172") {
		t.Errorf("interval macro not expanded to seconds bucket: %s", got)
	}
	// explicit interval wins
	got = expandMacros("GROUP BY $__interval", tr, 30*time.Second, 500)
	if !strings.Contains(got, "GROUP BY 30") {
		t.Errorf("explicit interval ignored: %s", got)
	}
	// interval_ms
	got = expandMacros("SELECT $__interval_ms", tr, 30*time.Second, 500)
	if !strings.Contains(got, "SELECT 30000") {
		t.Errorf("interval_ms wrong: %s", got)
	}
	// unixEpochGroup
	got = expandMacros("SELECT $__unixEpochGroup(T, $__interval) AS t, COUNT(*) AS c FROM T WHERE $__timeFilter(T) GROUP BY 1", tr, 0, 500)
	wantSub := "DATEADD(MINUTE, FLOOR(DATEDIFF(MINUTE, TIMESTAMP '1970-01-01 00:00:00', T) / 2) * 2, TIMESTAMP '1970-01-01 00:00:00')"
	if !strings.Contains(got, wantSub) {
		t.Errorf("unixEpochGroup wrong:\n got: %s\nwant substring: %s", got, wantSub)
	}
	// timeGroupAlias alias — note: a quoted bucket arg ('1m') would make the
	// macro span a string literal boundary, which the quote-aware expander
	// deliberately never crosses; unquoted args work everywhere.
	got = expandMacros("SELECT $__timeGroupAlias(T, 1m) FROM T", tr, 0, 0)
	if !strings.Contains(got, "DATEADD") || strings.Contains(got, "$__timeGroupAlias") {
		t.Errorf("timeGroupAlias not expanded: %s", got)
	}
}
