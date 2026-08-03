package nodes_test

import (
	"context"
	"strings"
	"testing"

	"christiangeorgelucas/sql-connector/nodes"

	gen "christiangeorgelucas/sql-connector/gen"
)

func TestExecuteTransaction_UnconfiguredSecret(t *testing.T) {
	ax := newTestContext(t)
	input := &gen.ExecuteTransactionRequest{
		Connection: &gen.ConnectionConfig{DsnSecretName: "NOPE"},
		Statements: []*gen.Statement{{Sql: "INSERT INTO t (n) VALUES ($1)", Params: []*gen.Param{{Type: gen.ParamType_PARAM_TYPE_INT, Value: "1"}}}},
	}
	// A connection-level failure is a hard node error, not a typed response —
	// unlike a statement-level failure, which the response fields carry.
	_, err := nodes.ExecuteTransaction(context.Background(), ax, input)
	if err == nil || !strings.Contains(err.Error(), `secret "NOPE" is not configured`) {
		t.Fatalf("expected an unconfigured-secret node error, got %v", err)
	}
}

func TestExecuteTransaction_EmptyStatements(t *testing.T) {
	// An empty statement list still needs a connection to begin+commit a
	// (no-op) transaction, so this still surfaces the connection failure.
	ax := newTestContext(t)
	input := &gen.ExecuteTransactionRequest{
		Connection: &gen.ConnectionConfig{DsnSecretName: "NOPE"},
	}
	_, err := nodes.ExecuteTransaction(context.Background(), ax, input)
	if err == nil {
		t.Fatal("expected an error for an unconfigured connection secret")
	}
}

// TestExecuteTransaction_BadParam proves a malformed param in ANY statement
// is caught before the connection is ever opened — no database round-trip,
// no half-started transaction — and reports the failing statement's index.
func TestExecuteTransaction_BadParam(t *testing.T) {
	ax := newTestContext(t) // no DB secret configured at all — must not be needed
	input := &gen.ExecuteTransactionRequest{
		Connection: &gen.ConnectionConfig{DsnSecretName: "NEVER_CONFIGURED"},
		Statements: []*gen.Statement{
			{Sql: "INSERT INTO t (n) VALUES ($1)", Params: []*gen.Param{{Type: gen.ParamType_PARAM_TYPE_INT, Value: "1"}}},
			{Sql: "UPDATE t SET n = $1", Params: []*gen.Param{{Type: gen.ParamType_PARAM_TYPE_INT, Value: "not-a-number"}}},
		},
	}
	got, err := nodes.ExecuteTransaction(context.Background(), ax, input)
	if err != nil {
		t.Fatalf("expected a typed response (not a node error) for a statement-level param failure, got node error: %v", err)
	}
	if got.GetCommitted() {
		t.Error("expected committed=false")
	}
	if got.GetFailedStatementIndex() != 1 {
		t.Errorf("expected failed_statement_index=1 (the second statement), got %d", got.GetFailedStatementIndex())
	}
	if !strings.Contains(got.GetError(), "params[0]") {
		t.Errorf("expected the error to name params[0] of the failing statement, got %q", got.GetError())
	}
}
