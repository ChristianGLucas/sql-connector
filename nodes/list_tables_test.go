package nodes_test

import (
	"context"
	"strings"
	"testing"

	"christiangeorgelucas/sql-connector/nodes"

	gen "christiangeorgelucas/sql-connector/gen"
)

func TestListTables_MissingDsnSecretName(t *testing.T) {
	ax := newTestContext(t)
	input := &gen.ListTablesRequest{Connection: &gen.ConnectionConfig{}}
	_, err := nodes.ListTables(context.Background(), ax, input)
	if err == nil || !strings.Contains(err.Error(), "dsn_secret_name is required") {
		t.Fatalf("expected a dsn_secret_name-required error, got %v", err)
	}
}

func TestListTables_UnconfiguredSecret(t *testing.T) {
	ax := newTestContext(t)
	input := &gen.ListTablesRequest{Connection: &gen.ConnectionConfig{DsnSecretName: "MISSING"}}
	_, err := nodes.ListTables(context.Background(), ax, input)
	if err == nil || !strings.Contains(err.Error(), `secret "MISSING" is not configured`) {
		t.Fatalf("expected an unconfigured-secret error, got %v", err)
	}
}
