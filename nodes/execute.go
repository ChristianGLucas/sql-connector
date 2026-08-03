package nodes

import (
	"context"
	"fmt"
	"strings"

	"christiangeorgelucas/sql-connector/axiom"
	gen "christiangeorgelucas/sql-connector/gen"
)

// Execute runs a single parameterized INSERT/UPDATE/DELETE/DDL statement
// against PostgreSQL or MySQL. Axiom invokes nodes at-least-once, so a
// retried delivery can execute this statement more than once — prefer
// naturally idempotent SQL (see ExecuteRequest's field doc for detail).
func Execute(ctx context.Context, ax axiom.Context, input *gen.ExecuteRequest) (*gen.ExecuteResponse, error) {
	sqlText := strings.TrimSpace(input.GetSql())
	if sqlText == "" {
		return nil, fmt.Errorf("sql is required")
	}

	args, err := bindParams(input.GetParams())
	if err != nil {
		return nil, err
	}

	db, engine, err := openConnection(ctx, ax, input.GetConnection())
	if err != nil {
		return nil, err
	}
	defer db.Close()

	execCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	res, err := db.ExecContext(execCtx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("execute failed against %s: %w", engine, err)
	}

	out := &gen.ExecuteResponse{}
	if ra, err := res.RowsAffected(); err == nil {
		out.RowsAffected = ra
	}
	if engine == "mysql" {
		if id, err := res.LastInsertId(); err == nil {
			out.LastInsertId = id
			out.LastInsertIdSupported = true
		}
	}
	return out, nil
}
