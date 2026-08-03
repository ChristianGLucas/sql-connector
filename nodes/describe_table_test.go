package nodes_test

import (
	"context"
	"strings"
	"testing"

	"christiangeorgelucas/sql-connector/nodes"

	gen "christiangeorgelucas/sql-connector/gen"
)

func TestDescribeTable_MissingTable(t *testing.T) {
	ax := newTestContext(t)
	ax.secretsMap["DB"] = "postgres://u:p@localhost:5432/db"
	input := &gen.DescribeTableRequest{Connection: &gen.ConnectionConfig{DsnSecretName: "DB"}, Table: "  "}
	_, err := nodes.DescribeTable(context.Background(), ax, input)
	if err == nil || !strings.Contains(err.Error(), "table is required") {
		t.Fatalf("expected a \"table is required\" error, got %v", err)
	}
}

func TestDescribeTable_UnconfiguredSecret(t *testing.T) {
	ax := newTestContext(t)
	input := &gen.DescribeTableRequest{Connection: &gen.ConnectionConfig{DsnSecretName: "MISSING"}, Table: "users"}
	_, err := nodes.DescribeTable(context.Background(), ax, input)
	if err == nil || !strings.Contains(err.Error(), `secret "MISSING" is not configured`) {
		t.Fatalf("expected an unconfigured-secret error, got %v", err)
	}
}
