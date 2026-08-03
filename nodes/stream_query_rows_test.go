package nodes_test

import (
	"context"
	"strings"
	"testing"

	"christiangeorgelucas/sql-connector/nodes"

	gen "christiangeorgelucas/sql-connector/gen"
)

func TestStreamQueryRows_EmptyInput(t *testing.T) {
	// A closed, never-seeded input channel must return cleanly and emit
	// nothing, rather than hanging or panicking.
	ax := newTestContext(t)
	in := make(chan *gen.StreamQueryRowsRequest)
	close(in)

	var frames []*gen.QueryRowFrame
	emit := func(f *gen.QueryRowFrame) error {
		frames = append(frames, f)
		return nil
	}

	if err := nodes.StreamQueryRows(context.Background(), ax, in, emit); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 0 {
		t.Fatalf("expected 0 frames for an empty input channel, got %d", len(frames))
	}
}

func TestStreamQueryRows_MissingSQL(t *testing.T) {
	ax := newTestContext(t)
	ax.secretsMap["DB"] = "postgres://u:p@localhost:5432/db"
	in := make(chan *gen.StreamQueryRowsRequest, 1)
	in <- &gen.StreamQueryRowsRequest{Connection: &gen.ConnectionConfig{DsnSecretName: "DB"}, Sql: ""}
	close(in)

	emit := func(f *gen.QueryRowFrame) error { return nil }
	err := nodes.StreamQueryRows(context.Background(), ax, in, emit)
	if err == nil || !strings.Contains(err.Error(), "sql is required") {
		t.Fatalf("expected a \"sql is required\" error, got %v", err)
	}
}

func TestStreamQueryRows_UnconfiguredSecret(t *testing.T) {
	ax := newTestContext(t)
	in := make(chan *gen.StreamQueryRowsRequest, 1)
	in <- &gen.StreamQueryRowsRequest{Connection: &gen.ConnectionConfig{DsnSecretName: "MISSING"}, Sql: "SELECT 1"}
	close(in)

	emit := func(f *gen.QueryRowFrame) error { return nil }
	err := nodes.StreamQueryRows(context.Background(), ax, in, emit)
	if err == nil || !strings.Contains(err.Error(), `secret "MISSING" is not configured`) {
		t.Fatalf("expected an unconfigured-secret error, got %v", err)
	}
}
