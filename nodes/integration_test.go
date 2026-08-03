//go:build integration

// Local live-database oracle: run with
//
//	go test -tags integration ./nodes/... -run Integration -v
//
// against the docker postgres+mysql containers seeded per RETRO-NOTES.md.
// Excluded from the default `go test ./...` / `axiom test` run (no build
// tag) so the deterministic unit-test gate never depends on live docker
// containers being present.
package nodes_test

import (
	"context"
	"strings"
	"testing"

	"christiangeorgelucas/sql-connector/nodes"

	gen "christiangeorgelucas/sql-connector/gen"
)

const (
	pgDSN    = "postgres://testuser:testpass@localhost:15432/testdb?sslmode=disable"
	mysqlDSN = "mysql://root:testpass@localhost:13306/testdb"
)

// TestIntegration_Query_BadSQLSyntax proves a genuine SQL syntax error
// surfaces as the real database error message (typed, not swallowed).
func TestIntegration_Query_BadSQLSyntax(t *testing.T) {
	for _, tc := range []struct{ engine, dsn string }{{"postgres", pgDSN}, {"mysql", mysqlDSN}} {
		ax := newTestContext(t)
		ax.secretsMap["DB"] = tc.dsn
		_, err := nodes.Query(context.Background(), ax, &gen.QueryRequest{
			Connection: &gen.ConnectionConfig{DsnSecretName: "DB"},
			Sql:        "SELEKT * FROM users WHERE",
		})
		if err == nil {
			t.Fatalf("[%s] expected a syntax error, got nil", tc.engine)
		}
		if !strings.Contains(err.Error(), "query failed against "+tc.engine) {
			t.Errorf("[%s] expected the error to be wrapped as a query failure, got %v", tc.engine, err)
		}
	}
}

// TestIntegration_Query_ParamCountMismatch proves a param-count mismatch
// (fewer bound params than placeholders in the SQL) surfaces the real
// driver error rather than hanging or silently binding wrong.
func TestIntegration_Query_ParamCountMismatch(t *testing.T) {
	for _, tc := range []struct{ engine, dsn, sql string }{
		{"postgres", pgDSN, "SELECT * FROM users WHERE id = $1 AND name = $2"},
		{"mysql", mysqlDSN, "SELECT * FROM users WHERE id = ? AND name = ?"},
	} {
		ax := newTestContext(t)
		ax.secretsMap["DB"] = tc.dsn
		_, err := nodes.Query(context.Background(), ax, &gen.QueryRequest{
			Connection: &gen.ConnectionConfig{DsnSecretName: "DB"},
			Sql:        tc.sql,
			Params:     []*gen.Param{{Type: gen.ParamType_PARAM_TYPE_INT, Value: "1"}}, // SQL wants 2, only 1 given
		})
		if err == nil {
			t.Fatalf("[%s] expected a param-count-mismatch error, got nil", tc.engine)
		}
	}
}

// TestIntegration_SelectViaExecute_DMLViaQuery documents and locks in this
// package's decided (empirically observed, not assumed) behavior for the
// two "wrong verb" cases, uniform across BOTH engines: this connector does
// not police the SQL verb against the node — Execute runs ExecContext and
// Query runs QueryContext exactly as given, and both engines' drivers
// happily run either. Execute(SELECT) succeeds and reports rows_affected as
// the number of rows the SELECT matched (a legal but useless way to invoke
// a SELECT — you get a count, not the data). Query(INSERT/UPDATE/DELETE)
// ALSO succeeds on both engines — the write takes effect — and returns a
// zero-column, zero-row QueryResponse, since neither driver's Query path
// produces a result set for a plain DML statement without RETURNING.
func TestIntegration_SelectViaExecute_DMLViaQuery(t *testing.T) {
	t.Run("select_via_execute_postgres", func(t *testing.T) {
		ax := newTestContext(t)
		ax.secretsMap["DB"] = pgDSN
		res, err := nodes.Execute(context.Background(), ax, &gen.ExecuteRequest{
			Connection: &gen.ConnectionConfig{DsnSecretName: "DB"},
			Sql:        "SELECT id FROM users WHERE id IN (1, 2, 3)",
		})
		if err != nil {
			t.Fatalf("Execute(SELECT) on postgres: unexpected error: %v", err)
		}
		if res.GetRowsAffected() != 3 {
			t.Errorf("Execute(SELECT) on postgres: expected rows_affected=3 (rows matched, not mutated), got %d", res.GetRowsAffected())
		}
	})

	t.Run("insert_via_query_postgres_succeeds_no_returning", func(t *testing.T) {
		ax := newTestContext(t)
		ax.secretsMap["DB"] = pgDSN
		before, _ := nodes.Query(context.Background(), ax, &gen.QueryRequest{
			Connection: &gen.ConnectionConfig{DsnSecretName: "DB"}, Sql: "SELECT count(*) FROM users",
		})
		got, err := nodes.Query(context.Background(), ax, &gen.QueryRequest{
			Connection: &gen.ConnectionConfig{DsnSecretName: "DB"},
			Sql:        "INSERT INTO users (name) VALUES ('via-query-postgres')",
		})
		if err != nil {
			t.Fatalf("Query(INSERT) on postgres: unexpected error: %v", err)
		}
		if got.GetRowCount() != 0 || len(got.GetColumns()) != 0 {
			t.Errorf("Query(INSERT) on postgres: expected a zero-column, zero-row result, got columns=%v rows=%d", got.GetColumns(), got.GetRowCount())
		}
		after, _ := nodes.Query(context.Background(), ax, &gen.QueryRequest{
			Connection: &gen.ConnectionConfig{DsnSecretName: "DB"}, Sql: "SELECT count(*) FROM users",
		})
		if before.GetRows()[0].GetValues()[0].GetValue() == after.GetRows()[0].GetValues()[0].GetValue() {
			t.Error("Query(INSERT) on postgres: expected the row count to have increased — the write should still take effect")
		}
	})

	t.Run("insert_via_query_mysql_also_succeeds", func(t *testing.T) {
		ax := newTestContext(t)
		ax.secretsMap["DB"] = mysqlDSN
		before, _ := nodes.Query(context.Background(), ax, &gen.QueryRequest{
			Connection: &gen.ConnectionConfig{DsnSecretName: "DB"}, Sql: "SELECT count(*) FROM users",
		})
		got, err := nodes.Query(context.Background(), ax, &gen.QueryRequest{
			Connection: &gen.ConnectionConfig{DsnSecretName: "DB"},
			Sql:        "INSERT INTO users (name) VALUES ('via-query-mysql')",
		})
		if err != nil {
			t.Fatalf("Query(INSERT) on mysql: unexpected error: %v", err)
		}
		if got.GetRowCount() != 0 || len(got.GetColumns()) != 0 {
			t.Errorf("Query(INSERT) on mysql: expected a zero-column, zero-row result, got columns=%v rows=%d", got.GetColumns(), got.GetRowCount())
		}
		after, _ := nodes.Query(context.Background(), ax, &gen.QueryRequest{
			Connection: &gen.ConnectionConfig{DsnSecretName: "DB"}, Sql: "SELECT count(*) FROM users",
		})
		if before.GetRows()[0].GetValues()[0].GetValue() == after.GetRows()[0].GetValues()[0].GetValue() {
			t.Error("Query(INSERT) on mysql: expected the row count to have increased — the write should still take effect, same as postgres")
		}
	})
}

// TestIntegration_RawDSNField proves connection.dsn (the raw, non-secret
// path) actually works end-to-end against a real database — not just the
// precedence logic (covered by the pure-function TestResolveDSN_Precedence
// unit test).
func TestIntegration_RawDSNField(t *testing.T) {
	ax := newTestContext(t) // no secretsMap entries — proves the secret is genuinely not needed
	got, err := nodes.Query(context.Background(), ax, &gen.QueryRequest{
		Connection: &gen.ConnectionConfig{Dsn: pgDSN},
		Sql:        "SELECT count(*) FROM users",
	})
	if err != nil {
		t.Fatalf("Query via raw dsn: %v", err)
	}
	if got.GetRowCount() != 1 || got.GetRows()[0].GetValues()[0].GetIsNull() {
		t.Errorf("Query via raw dsn: expected a real count, got %+v", got)
	}
}

func TestIntegration_Query_NullVsZeroRoundTrip_Postgres(t *testing.T) {
	testQueryNullVsZeroRoundTrip(t, pgDSN, "postgres")
}
func TestIntegration_Query_NullVsZeroRoundTrip_MySQL(t *testing.T) {
	testQueryNullVsZeroRoundTrip(t, mysqlDSN, "mysql")
}

func testQueryNullVsZeroRoundTrip(t *testing.T, dsn, engine string) {
	ax := newTestContext(t)
	ax.secretsMap["DB"] = dsn
	// Scoped to the three seed rows by id (1,2,3) — NOT total table count —
	// so this stays correct even after other tests in this run (Execute,
	// ExecuteTransaction) insert additional rows into the same shared
	// docker database.
	input := &gen.QueryRequest{
		Connection: &gen.ConnectionConfig{DsnSecretName: "DB"},
		Sql:        "SELECT id, name, email, age, is_active, balance, bio, avatar FROM users WHERE id IN (1, 2, 3) ORDER BY id",
	}
	got, err := nodes.Query(context.Background(), ax, input)
	if err != nil {
		t.Fatalf("[%s] Query failed: %v", engine, err)
	}
	if got.GetRowCount() != 3 {
		t.Fatalf("[%s] expected 3 seed rows (ids 1-3), got %d — re-seed the docker database (see RETRO-NOTES.md)", engine, got.GetRowCount())
	}

	// Row 0: Alice — every column populated, no NULLs.
	alice := got.GetRows()[0].GetValues()
	if alice[1].GetIsNull() || alice[1].GetValue() != "Alice" {
		t.Errorf("[%s] Alice.name: got %+v", engine, alice[1])
	}
	if alice[5].GetIsNull() { // avatar bytea/blob = DEADBEEF
		t.Errorf("[%s] Alice.avatar unexpectedly null", engine)
	}

	// Row 1: Bob — email/age/is_active(false, not null)/balance/bio/avatar
	// are NULL except is_active which is explicitly false (not null).
	bob := got.GetRows()[1].GetValues()
	if !bob[2].GetIsNull() { // email
		t.Errorf("[%s] Bob.email: expected NULL, got %+v", engine, bob[2])
	}
	if !bob[3].GetIsNull() { // age
		t.Errorf("[%s] Bob.age: expected NULL, got %+v", engine, bob[3])
	}
	// is_active = false, NOT null. MySQL has no native BOOLEAN type (it's
	// TINYINT(1)) so the driver hands back int64, rendered "0"/"1" — that is
	// MySQL's own honest native representation, not a bug; Postgres has a
	// real boolean type and renders "true"/"false". Either way, is_null must
	// be false — that's the actual trap this test exists to catch.
	wantFalse := "false"
	if engine == "mysql" {
		wantFalse = "0"
	}
	if bob[4].GetIsNull() || bob[4].GetValue() != wantFalse {
		t.Errorf("[%s] Bob.is_active: expected {%s, is_null:false}, got %+v — THIS IS THE NULL-VS-ZERO TRAP", engine, wantFalse, bob[4])
	}
	if !bob[6].GetIsNull() { // bio
		t.Errorf("[%s] Bob.bio: expected NULL, got %+v", engine, bob[6])
	}

	// Row 2: unicode name, age=0 (real zero, not null), bio="" (real empty
	// string, not null) — the other half of the null-vs-zero trap.
	unicodeRow := got.GetRows()[2].GetValues()
	if unicodeRow[1].GetIsNull() || unicodeRow[1].GetValue() != "café-日本語-🎉" {
		t.Errorf("[%s] unicode name: got %+v", engine, unicodeRow[1])
	}
	if unicodeRow[3].GetIsNull() || unicodeRow[3].GetValue() != "0" {
		t.Errorf("[%s] age=0: expected {0, is_null:false}, got %+v", engine, unicodeRow[3])
	}
	if unicodeRow[6].GetIsNull() || unicodeRow[6].GetValue() != "" {
		t.Errorf("[%s] bio=\"\": expected {\"\", is_null:false}, got %+v", engine, unicodeRow[6])
	}
}

func TestIntegration_Execute_And_Ping(t *testing.T) {
	for _, tc := range []struct {
		engine string
		dsn    string
	}{{"postgres", pgDSN}, {"mysql", mysqlDSN}} {
		t.Run(tc.engine, func(t *testing.T) {
			ax := newTestContext(t)
			ax.secretsMap["DB"] = tc.dsn

			ping, err := nodes.Ping(context.Background(), ax, &gen.PingRequest{Connection: &gen.ConnectionConfig{DsnSecretName: "DB"}})
			if err != nil {
				t.Fatalf("Ping: %v", err)
			}
			if ping.GetEngine() != tc.engine || ping.GetServerVersion() == "" {
				t.Errorf("Ping: got %+v", ping)
			}

			execRes, err := nodes.Execute(context.Background(), ax, &gen.ExecuteRequest{
				Connection: &gen.ConnectionConfig{DsnSecretName: "DB"},
				Sql:        placeholder(tc.engine, "INSERT INTO users (name, age) VALUES (%s, %s)"),
				Params: []*gen.Param{
					{Type: gen.ParamType_PARAM_TYPE_STRING, Value: "IntegrationTestUser"},
					{Type: gen.ParamType_PARAM_TYPE_INT, Value: "99"},
				},
			})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if execRes.GetRowsAffected() != 1 {
				t.Errorf("Execute: expected rows_affected=1, got %d", execRes.GetRowsAffected())
			}
			if tc.engine == "mysql" && (!execRes.GetLastInsertIdSupported() || execRes.GetLastInsertId() == 0) {
				t.Errorf("Execute (mysql): expected a nonzero supported last_insert_id, got %+v", execRes)
			}
			if tc.engine == "postgres" && execRes.GetLastInsertIdSupported() {
				t.Errorf("Execute (postgres): expected last_insert_id_supported=false")
			}
		})
	}
}

func TestIntegration_ExecuteTransaction_RollbackOnFailure(t *testing.T) {
	ax := newTestContext(t)
	ax.secretsMap["DB"] = pgDSN

	before, _ := nodes.Query(context.Background(), ax, &gen.QueryRequest{
		Connection: &gen.ConnectionConfig{DsnSecretName: "DB"}, Sql: "SELECT count(*) FROM users",
	})
	beforeCount := before.GetRows()[0].GetValues()[0].GetValue()

	got, err := nodes.ExecuteTransaction(context.Background(), ax, &gen.ExecuteTransactionRequest{
		Connection: &gen.ConnectionConfig{DsnSecretName: "DB"},
		Statements: []*gen.Statement{
			{Sql: "INSERT INTO users (name, age) VALUES ($1, $2)", Params: []*gen.Param{
				{Type: gen.ParamType_PARAM_TYPE_STRING, Value: "TxUser"},
				{Type: gen.ParamType_PARAM_TYPE_INT, Value: "1"},
			}},
			{Sql: "INSERT INTO this_table_does_not_exist (n) VALUES ($1)", Params: []*gen.Param{
				{Type: gen.ParamType_PARAM_TYPE_INT, Value: "1"},
			}},
		},
	})
	if err != nil {
		t.Fatalf("expected a typed response, got node error: %v", err)
	}
	if got.GetCommitted() {
		t.Fatal("expected committed=false")
	}
	if got.GetFailedStatementIndex() != 1 {
		t.Errorf("expected failed_statement_index=1, got %d", got.GetFailedStatementIndex())
	}

	after, _ := nodes.Query(context.Background(), ax, &gen.QueryRequest{
		Connection: &gen.ConnectionConfig{DsnSecretName: "DB"}, Sql: "SELECT count(*) FROM users",
	})
	afterCount := after.GetRows()[0].GetValues()[0].GetValue()
	if afterCount != beforeCount {
		t.Errorf("expected row count unchanged after rollback: before=%s after=%s", beforeCount, afterCount)
	}
}

func TestIntegration_ListTables_And_DescribeTable(t *testing.T) {
	for _, tc := range []struct {
		engine, dsn, schema string
	}{{"postgres", pgDSN, "public"}, {"mysql", mysqlDSN, "testdb"}} {
		t.Run(tc.engine, func(t *testing.T) {
			ax := newTestContext(t)
			ax.secretsMap["DB"] = tc.dsn

			lt, err := nodes.ListTables(context.Background(), ax, &gen.ListTablesRequest{Connection: &gen.ConnectionConfig{DsnSecretName: "DB"}})
			if err != nil {
				t.Fatalf("ListTables: %v", err)
			}
			names := map[string]bool{}
			for _, tbl := range lt.GetTables() {
				names[tbl.GetName()] = true
			}
			if !names["users"] || !names["big_table"] {
				t.Errorf("ListTables: expected users+big_table, got %+v", lt.GetTables())
			}

			dt, err := nodes.DescribeTable(context.Background(), ax, &gen.DescribeTableRequest{
				Connection: &gen.ConnectionConfig{DsnSecretName: "DB"}, Table: "users",
			})
			if err != nil {
				t.Fatalf("DescribeTable: %v", err)
			}
			var idCol *gen.ColumnInfo
			for _, c := range dt.GetColumns() {
				if c.GetName() == "id" {
					idCol = c
				}
			}
			if idCol == nil || !idCol.GetPrimaryKey() {
				t.Errorf("DescribeTable: expected id to be primary_key=true, got %+v", idCol)
			}
		})
	}
}

func TestIntegration_StreamQueryRows_5000Rows_NoFullMaterialization(t *testing.T) {
	ax := newTestContext(t)
	ax.secretsMap["DB"] = pgDSN

	in := make(chan *gen.StreamQueryRowsRequest, 1)
	in <- &gen.StreamQueryRowsRequest{
		Connection: &gen.ConnectionConfig{DsnSecretName: "DB"},
		Sql:        "SELECT id, val FROM big_table ORDER BY id",
	}
	close(in)

	var count int64
	var lastIsFinal bool
	err := nodes.StreamQueryRows(context.Background(), ax, in, func(f *gen.QueryRowFrame) error {
		if f.GetRowIndex() != count {
			t.Fatalf("row_index out of order: got %d, want %d", f.GetRowIndex(), count)
		}
		count++
		lastIsFinal = f.GetIsFinal()
		if count < 5000 && f.GetIsFinal() {
			t.Fatalf("is_final=true before the last row (at row %d)", f.GetRowIndex())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamQueryRows: %v", err)
	}
	if count != 5000 {
		t.Fatalf("expected exactly 5000 frames, got %d", count)
	}
	if !lastIsFinal {
		t.Fatal("expected is_final=true on the last frame")
	}
}

// placeholder renders the two-param INSERT template with the target
// engine's native placeholder syntax ($1/$2 vs ?/?) — this package never
// translates placeholders itself, so the test data must match too.
func placeholder(engine, tmpl string) string {
	if engine == "mysql" {
		return strings.Replace(strings.Replace(tmpl, "%s", "?", 1), "%s", "?", 1)
	}
	return strings.Replace(strings.Replace(tmpl, "%s", "$1", 1), "%s", "$2", 1)
}
