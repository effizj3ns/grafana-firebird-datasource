package plugin

// Integration test for rowsToFrame against a real Firebird server.
// Proves the type-mapping fix end-to-end: INT64 columns arrive as int64
// (not string), timestamps as time, and maxRows caps the result.

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

func integrationDB(t *testing.T) *sql.DB {
	t.Helper()
	host := os.Getenv("FB_HOST")
	db := os.Getenv("FB_DATABASE")
	user := os.Getenv("FB_USER")
	pass := os.Getenv("FB_PASSWORD")
	if host == "" || db == "" || user == "" {
		t.Skip("integration env not set (FB_HOST/FB_DATABASE/FB_USER/FB_PASSWORD)")
	}
	s := DatasourceSettings{Host: host, Port: 3050, Database: db, User: user, Password: pass}
	dsn := s.dsn()
	conn, err := sql.Open("firebirdsql", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestIntegrationRowsToFrameTypeMapping(t *testing.T) {
	db := integrationDB(t)
	// Live-probed schema facts (TBLAUFTRAG): AUFTRAGNUMMER is VARCHAR(80)
	// (driver type VARYING), ID is LONG (int64), ERFDATUM is TIMESTAMP.
	rows, err := db.Query("SELECT FIRST 3 ID, AUFTRAGNUMMER, ERFDATUM FROM TBLAUFTRAG ORDER BY ERFDATUM DESC")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	frame, err := rowsToFrame(rows, 1000)
	if err != nil {
		t.Fatalf("rowsToFrame: %v", err)
	}
	if len(frame.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(frame.Fields))
	}
	idField := frame.Fields[0]
	if idField.Type() != data.FieldTypeNullableInt64 {
		t.Fatalf("ID (driver LONG) must be Int64 (type-mapping fix), got %v", idField.Type())
	}
	v0 := idField.At(0)
	if _, ok := v0.(*int64); !ok {
		t.Fatalf("first value must be *int64, got %T", v0)
	}
	numField := frame.Fields[1]
	if numField.Type() != data.FieldTypeNullableString {
		t.Fatalf("AUFTRAGNUMMER (driver VARYING) must be String, got %v", numField.Type())
	}
	timeField := frame.Fields[2]
	if timeField.Type() != data.FieldTypeNullableTime {
		t.Fatalf("ERFDATUM must be Time, got %v", timeField.Type())
	}
	t.Logf("type mapping OK: ID=%v (%T), NUMMER=%v, rows=%d", v0, v0, numField.At(0), frame.Rows())
}

func TestIntegrationRowsToFrameMaxRows(t *testing.T) {
	db := integrationDB(t)
	rows, err := db.Query("SELECT AUFTRAGNUMMER FROM TBLAUFTRAG")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	_, err = rowsToFrame(rows, 5)
	if err == nil {
		t.Fatal("expected maxRows error when result exceeds cap")
	}
	t.Logf("maxRows enforcement OK: %v", err)
}

func TestIntegrationRowsToFrameScaledNumeric(t *testing.T) {
	db := integrationDB(t)
	// Driver under dialect 3 reports scaled numerics via SHORT/LONG/INT64
	// with DecimalSize scale != 0. Use a real scaled expression: the driver
	// then reports typeName INT64 with scale -4 for NUMERIC(18,4).
	rows, err := db.Query("SELECT FIRST 1 ID * 1.0000 AS SCALED FROM TBLAUFTRAG")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	frame, err := rowsToFrame(rows, 100)
	if err != nil {
		t.Fatalf("rowsToFrame: %v", err)
	}
	f := frame.Fields[0]
	if f.Type() != data.FieldTypeNullableFloat64 {
		t.Fatalf("scaled NUMERIC must be Float64, got %v", f.Type())
	}
	t.Logf("scaled numeric OK: %v", f.At(0))
}

// TestIntegrationMacroTimeFilter runs a real query with $__timeFilter against
// the live DB — the exact path a dashboard panel takes (rawSql verbatim from
// the frontend, expanded server-side).
func TestIntegrationMacroTimeFilter(t *testing.T) {
	if testing.Short() || os.Getenv("FB_HOST") == "" {
		t.Skip("integration test: set FB_HOST/FB_DATABASE/FB_USER/FB_PASSWORD")
	}
	s := DatasourceSettings{
		Host: os.Getenv("FB_HOST"), Port: 3050,
		Database: os.Getenv("FB_DATABASE"), User: os.Getenv("FB_USER"),
		Password: os.Getenv("FB_PASSWORD"), Charset: "UTF8",
	}
	inst, err := newDatasourceInstance(context.Background(), s)
	if err != nil {
		t.Fatalf("instance: %v", err)
	}
	defer inst.Dispose()

	// Wide range (1970..2100) so the live rows are included regardless of dates.
	tr := backend.TimeRange{
		From: time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	raw := "SELECT FIRST 5 AUFTRAGNUMMER, ERFDATUM FROM TBLAUFTRAG WHERE $__timeFilter(ERFDATUM) ORDER BY ERFDATUM DESC"
	expanded := expandMacros(raw, tr, 0, 0)
	if strings.Contains(expanded, "$__timeFilter") {
		t.Fatalf("macro not expanded: %s", expanded)
	}
	rows, err := inst.db.Query(expanded)
	if err != nil {
		t.Fatalf("live query failed: %v\nexpanded: %s", err, expanded)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if n == 0 {
		t.Fatal("expected rows in 1970..2100 range, got 0")
	}
	t.Logf("macro timeFilter live OK: %d rows", n)
}
