package plugin

// Version matrix test: runs the plugin's core paths against multiple live
// Firebird servers (2.5 / 3.0 / 4.0 / 5.0) started in local Docker.
//
// Env:
//	FB_MATRIX="host:port:db:user:pass,..." — comma-separated targets
//	Skipped entirely when FB_MATRIX is empty.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

type matrixTarget struct {
	name, host string
	port       int
	db, user   string
	pass       string
}

func parseMatrix(t *testing.T) []matrixTarget {
	t.Helper()
	raw := os.Getenv("FB_MATRIX")
	if raw == "" {
		t.Skip("matrix test: set FB_MATRIX=host:port:db:user:pass,...")
	}
	var out []matrixTarget
	for _, entry := range strings.Split(raw, ",") {
		parts := strings.SplitN(strings.TrimSpace(entry), ":", 5)
		if len(parts) != 5 {
			t.Fatalf("bad FB_MATRIX entry: %q", entry)
		}
		var port int
		fmt.Sscanf(parts[1], "%d", &port)
		name := parts[0]
		host := name
		if i := strings.IndexRune(name, '@'); i != -1 {
			host = name[i+1:] // "fb30@127.0.0.1" -> name=fb30, host=127.0.0.1
			name = name[:i]
		}
		out = append(out, matrixTarget{name: name, host: host, port: port, db: parts[2], user: parts[3], pass: parts[4]})
	}
	return out
}

func openTarget(t *testing.T, m matrixTarget) *sql.DB {
	t.Helper()
	s := DatasourceSettings{Host: m.host, Port: m.port, Database: m.db, User: m.user, Password: m.pass, Charset: "UTF8"}
	inst, err := newDatasourceInstance(context.Background(), s)
	if err != nil {
		t.Fatalf("%s: connect: %v", m.name, err)
	}
	return inst.db
}

// TestMatrixTypeMappingAndQueries: connect, query typed columns, verify mapping.
func TestMatrixTypeMappingAndQueries(t *testing.T) {
	for _, m := range parseMatrix(t) {
		t.Run(m.name, func(t *testing.T) {
			db := openTarget(t, m)
			defer db.Close()

			rows, err := db.Query("SELECT ID, NAME, AMOUNT, RATIO, CREATED, FLAG FROM TBLTEST ORDER BY ID")
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			defer rows.Close()

			frame, err := rowsToFrame(rows, 100)
			if err != nil {
				t.Fatalf("rowsToFrame: %v", err)
			}
			if frame.Rows() != 3 {
				t.Fatalf("expected 3 rows, got %d", frame.Rows())
			}
			// ID -> nullable int64 (SQL columns are NULL-able by definition;
			// Grafana SQL datasources use the Nullable* field family)
			if f := frame.Fields[0]; f.Type() != data.FieldTypeNullableInt64 {
				t.Errorf("ID field type = %s, want NullableInt64", f.Type().ItemTypeString())
			}
			// NAME -> nullable string
			if f := frame.Fields[1]; f.Type() != data.FieldTypeNullableString {
				t.Errorf("NAME field type = %s, want NullableString", f.Type().ItemTypeString())
			}
			// AMOUNT (scaled NUMERIC) -> nullable float64
			if f := frame.Fields[2]; f.Type() != data.FieldTypeNullableFloat64 {
				t.Errorf("AMOUNT field type = %s, want NullableFloat64", f.Type().ItemTypeString())
			}
			// CREATED -> nullable time
			if f := frame.Fields[4]; f.Type() != data.FieldTypeNullableTime {
				t.Errorf("CREATED field type = %s, want NullableTime", f.Type().ItemTypeString())
			}
		})
	}
}

// TestMatrixMacros: expandMacros output runs as real SQL on each version —
// timeFilter, interval group-by (incl. the FB2.5 INTEGER-overflow path with
// dates far in the future), epoch filter.
func TestMatrixMacros(t *testing.T) {
	for _, m := range parseMatrix(t) {
		t.Run(m.name, func(t *testing.T) {
			db := openTarget(t, m)
			defer db.Close()

			tr := backend.TimeRange{
				From: time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
				To:   time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
			}

			// 1) timeFilter
			q := expandMacros("SELECT FIRST 3 ID FROM TBLTEST WHERE $__timeFilter(CREATED) ORDER BY ID", tr, 0, 0)
			rows, err := db.Query(q)
			if err != nil {
				t.Fatalf("timeFilter: %v\nSQL: %s", err, q)
			}
			rows.Close()

			// 2) interval group-by on a 130-year range with 2000 points ->
			//    bucket = 2054880ms = 2054s -> >= 60s -> DATEDIFF(MINUTE) path,
			//    exercises the FB2.5 INTEGER overflow protection.
			q = expandMacros("SELECT $__unixEpochGroup(CREATED, $__interval) AS b, COUNT(*) AS c FROM TBLTEST WHERE $__timeFilter(CREATED) GROUP BY 1 ORDER BY 1", tr, 0, 2000)
			rows, err = db.Query(q)
			if err != nil {
				t.Fatalf("interval group: %v\nSQL: %s", err, q)
			}
			rows.Close()

			// 3) unixEpochFilter
			q = expandMacros("SELECT FIRST 1 ID FROM TBLTEST WHERE $__unixEpochFilter(ID)", tr, 0, 0)
			// ID is an integer not a timestamp — on dialect-1 FB2.5 the numeric
			// compare still works; query must PARSE (we only check compile).
			rows, err = db.Query(q)
			if err != nil {
				t.Fatalf("unixEpochFilter: %v\nSQL: %s", err, q)
			}
			rows.Close()
		})
	}
}

// TestMatrixMaxRows: maxRows cap enforced per version.
func TestMatrixMaxRows(t *testing.T) {
	for _, m := range parseMatrix(t) {
		t.Run(m.name, func(t *testing.T) {
			db := openTarget(t, m)
			defer db.Close()
			rows, err := db.Query("SELECT ID FROM TBLTEST ORDER BY ID")
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			defer rows.Close()
			_, err = rowsToFrame(rows, 2)
			if err == nil || !strings.Contains(err.Error(), "maxRows") {
				t.Fatalf("expected maxRows error, got %v", err)
			}
		})
	}
}

// keep data import used (frame assertions may be extended)
var _ = data.Frame{}
