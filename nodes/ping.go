package nodes

import (
	"context"
	"fmt"
	"time"

	"christiangeorgelucas/sql-connector/axiom"
	gen "christiangeorgelucas/sql-connector/gen"
)

// Ping checks connectivity to a PostgreSQL or MySQL database within a
// bounded timeout and reads back its version. An unreachable host, auth
// failure, or timeout returns a structured node error rather than a
// response with zero-valued fields — a caller only ever gets a
// PingResponse back on genuine success.
func Ping(ctx context.Context, ax axiom.Context, input *gen.PingRequest) (*gen.PingResponse, error) {
	start := time.Now()

	db, engine, err := openConnection(ctx, ax, input.GetConnection())
	if err != nil {
		return nil, err
	}
	defer db.Close()

	versionQuery := "SELECT version()"
	if engine == "mysql" {
		versionQuery = "SELECT VERSION()"
	}
	versionCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	var version string
	if err := db.QueryRowContext(versionCtx, versionQuery).Scan(&version); err != nil {
		return nil, fmt.Errorf("reading %s server version: %w", engine, err)
	}

	return &gen.PingResponse{
		Engine:        engine,
		ServerVersion: version,
		LatencyMs:     time.Since(start).Milliseconds(),
	}, nil
}
