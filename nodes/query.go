package nodes

import (
	"context"
	"fmt"
	"strings"

	"christiangeorgelucas/sql-connector/axiom"
	gen "christiangeorgelucas/sql-connector/gen"
)

// Query runs a parameterized SELECT against PostgreSQL or MySQL and returns
// the full result set (subject to max_rows), with every parameter bound
// through the driver's own placeholder mechanism — never string-interpolated
// into the SQL text.
func Query(ctx context.Context, ax axiom.Context, input *gen.QueryRequest) (*gen.QueryResponse, error) {
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

	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	rows, err := db.QueryContext(queryCtx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed against %s: %w", engine, err)
	}
	defer rows.Close()

	columns, colTypes, err := columnsFromRows(rows)
	if err != nil {
		return nil, fmt.Errorf("reading column metadata: %w", err)
	}

	maxRows := input.GetMaxRows()
	var resultRows []*gen.Row
	for maxRows <= 0 || int32(len(resultRows)) < maxRows {
		if !rows.Next() {
			break
		}
		row, err := scanRow(rows, colTypes)
		if err != nil {
			return nil, fmt.Errorf("scanning row %d: %w", len(resultRows), err)
		}
		resultRows = append(resultRows, row)
	}

	truncated := false
	if maxRows > 0 && int32(len(resultRows)) == maxRows {
		// Peek for one more row (without collecting it) to distinguish "the
		// result set had exactly max_rows rows" from "it had more and we cut
		// it off" — truncated must only be true in the latter case.
		if rows.Next() {
			truncated = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}

	return &gen.QueryResponse{
		Columns:   columns,
		Rows:      resultRows,
		RowCount:  int64(len(resultRows)),
		Truncated: truncated,
	}, nil
}
