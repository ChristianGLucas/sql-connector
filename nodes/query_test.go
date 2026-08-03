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
	if err == nil || !strings.Contains(err.Error(), "dsn_secret_name or connection.dsn is required") {
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

// TestQuery_MalformedSecretDSN_DoesNotLeakCredentials is a regression test
// for a real finding from independent review: url.Parse's error message
// embeds its input VERBATIM, so a secret-sourced DSN that fails to parse
// must NOT have that raw parse error wrapped into the node's returned
// error — doing so would leak the secret's credentials (and any other DSN
// content) into the caller's error string, and from there into logs/
// execution history.
func TestQuery_MalformedSecretDSN_DoesNotLeakCredentials(t *testing.T) {
	ax := newTestContext(t)
	secret := "postgres://superSecretUser:sup3rSecr3tP@ssw0rd!@evil-leak-marker-host/db\x7f" // \x7f: rejected by url.Parse
	ax.secretsMap["DB"] = secret
	_, err := nodes.Query(context.Background(), ax, &gen.QueryRequest{
		Connection: &gen.ConnectionConfig{DsnSecretName: "DB"},
		Sql:        "SELECT 1",
	})
	if err == nil {
		t.Fatal("expected an error for a malformed DSN, got nil")
	}
	if strings.Contains(err.Error(), "sup3rSecr3tP") || strings.Contains(err.Error(), "evil-leak-marker-host") {
		t.Fatalf("secret DSN contents leaked into error message: %q", err.Error())
	}
}
