package nodes_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"christiangeorgelucas/sql-connector/nodes"

	gen "christiangeorgelucas/sql-connector/gen"
)

func TestPing_UnreachableHost_BoundedTimeout(t *testing.T) {
	ax := newTestContext(t)
	// 203.0.113.0/24 is TEST-NET-3 (RFC 5737) — guaranteed non-routable, so
	// this proves an unreachable host fails with a clean typed error inside
	// connectTimeout rather than hanging on an OS-level TCP timeout.
	ax.secretsMap["DB"] = "postgres://u:p@203.0.113.1:5432/db"
	input := &gen.PingRequest{Connection: &gen.ConnectionConfig{DsnSecretName: "DB"}}

	start := time.Now()
	_, err := nodes.Ping(context.Background(), ax, input)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error for an unreachable host, got nil")
	}
	if !strings.Contains(err.Error(), "connecting to postgres database") {
		t.Errorf("expected a \"connecting to postgres database\" error, got %v", err)
	}
	if elapsed > 20*time.Second {
		t.Errorf("Ping took %v to fail — connectTimeout should bound this well under 20s", elapsed)
	}
}

func TestPing_MissingDsnSecretName(t *testing.T) {
	ax := newTestContext(t)
	input := &gen.PingRequest{Connection: &gen.ConnectionConfig{}}
	_, err := nodes.Ping(context.Background(), ax, input)
	if err == nil || !strings.Contains(err.Error(), "dsn_secret_name or connection.dsn is required") {
		t.Fatalf("expected a dsn_secret_name-required error, got %v", err)
	}
}
