package nodes_test

import (
	"context"
	"strings"
	"testing"

	"christiangeorgelucas/sql-connector/nodes"

	gen "christiangeorgelucas/sql-connector/gen"
)

func TestExecute_MissingSQL(t *testing.T) {
	ax := newTestContext(t)
	ax.secretsMap["DB"] = "postgres://u:p@localhost:5432/db"
	input := &gen.ExecuteRequest{
		Connection: &gen.ConnectionConfig{DsnSecretName: "DB"},
		Sql:        "",
	}
	_, err := nodes.Execute(context.Background(), ax, input)
	if err == nil || !strings.Contains(err.Error(), "sql is required") {
		t.Fatalf("expected a \"sql is required\" error, got %v", err)
	}
}

func TestExecute_UnconfiguredSecret(t *testing.T) {
	ax := newTestContext(t)
	input := &gen.ExecuteRequest{
		Connection: &gen.ConnectionConfig{DsnSecretName: "NOPE"},
		Sql:        "DELETE FROM t WHERE id = $1",
		Params:     []*gen.Param{{Type: gen.ParamType_PARAM_TYPE_INT, Value: "1"}},
	}
	_, err := nodes.Execute(context.Background(), ax, input)
	if err == nil || !strings.Contains(err.Error(), `secret "NOPE" is not configured`) {
		t.Fatalf("expected an unconfigured-secret error, got %v", err)
	}
}

func TestExecute_BadParam(t *testing.T) {
	ax := newTestContext(t)
	ax.secretsMap["DB"] = "postgres://u:p@localhost:5432/db"
	input := &gen.ExecuteRequest{
		Connection: &gen.ConnectionConfig{DsnSecretName: "DB"},
		Sql:        "UPDATE t SET n = $1",
		Params:     []*gen.Param{{Type: gen.ParamType_PARAM_TYPE_INT, Value: "not-a-number"}},
	}
	_, err := nodes.Execute(context.Background(), ax, input)
	if err == nil || !strings.Contains(err.Error(), "params[0]") {
		t.Fatalf("expected a params[0] binding error, got %v", err)
	}
}
