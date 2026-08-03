package nodes_test

import (
	"context"
	"strings"
	"testing"

	"christiangeorgelucas/sql-connector/nodes"

	gen "christiangeorgelucas/sql-connector/gen"
)

func TestQuery_MissingSQL(t *testing.T) {
	ax := newTestContext(t)
	ax.secretsMap["DB"] = "postgres://u:p@localhost:5432/db"
	input := &gen.QueryRequest{
		Connection: &gen.ConnectionConfig{DsnSecretName: "DB"},
		Sql:        "   ",
	}
	_, err := nodes.Query(context.Background(), ax, input)
	if err == nil || !strings.Contains(err.Error(), "sql is required") {
		t.Fatalf("expected a \"sql is required\" error, got %v", err)
	}
}

func TestQuery_MissingDsnSecretName(t *testing.T) {
	ax := newTestContext(t)
	input := &gen.QueryRequest{
		Connection: &gen.ConnectionConfig{},
		Sql:        "SELECT 1",
	}
	_, err := nodes.Query(context.Background(), ax, input)
	if err == nil || !strings.Contains(err.Error(), "dsn_secret_name is required") {
		t.Fatalf("expected a dsn_secret_name-required error, got %v", err)
	}
}

func TestQuery_UnconfiguredSecret(t *testing.T) {
	ax := newTestContext(t) // secretsMap left empty
	input := &gen.QueryRequest{
		Connection: &gen.ConnectionConfig{DsnSecretName: "MISSING_SECRET"},
		Sql:        "SELECT 1",
	}
	_, err := nodes.Query(context.Background(), ax, input)
	if err == nil || !strings.Contains(err.Error(), `secret "MISSING_SECRET" is not configured`) {
		t.Fatalf("expected an unconfigured-secret error naming the secret, got %v", err)
	}
}

func TestQuery_UnsupportedScheme(t *testing.T) {
	ax := newTestContext(t)
	ax.secretsMap["DB"] = "mongodb://u:p@localhost:27017/db"
	input := &gen.QueryRequest{
		Connection: &gen.ConnectionConfig{DsnSecretName: "DB"},
		Sql:        "SELECT 1",
	}
	_, err := nodes.Query(context.Background(), ax, input)
	if err == nil || !strings.Contains(err.Error(), "unsupported DSN scheme") {
		t.Fatalf("expected an unsupported-scheme error, got %v", err)
	}
}
