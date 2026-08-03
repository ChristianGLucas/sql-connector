package nodes

import (
	"context"
	"fmt"
	"strings"

	"christiangeorgelucas/sql-connector/axiom"
	gen "christiangeorgelucas/sql-connector/gen"
)

// StreamQueryRows runs a parameterized SELECT and emits one QueryRowFrame
// per result row using a real server-side cursor (sql.Rows.Next) — the
// result set is never materialized in memory, making this the right choice
// for tables too large to return from Query. Designed as this pipeline
// flow's START node (per platform convention the input channel then yields
// exactly one request): is_final is set true on the last frame of THAT
// request's row stream, so a downstream consumer sees exactly one terminal
// marker per query.
func StreamQueryRows(ctx context.Context, ax axiom.Context, in <-chan *gen.StreamQueryRowsRequest, emit func(*gen.QueryRowFrame) error) error {
	for input := range in {
		if err := streamOneQuery(ctx, ax, input, emit); err != nil {
			return err
		}
	}
	return nil
}

func streamOneQuery(ctx context.Context, ax axiom.Context, input *gen.StreamQueryRowsRequest, emit func(*gen.QueryRowFrame) error) error {
	sqlText := strings.TrimSpace(input.GetSql())
	if sqlText == "" {
		return fmt.Errorf("sql is required")
	}

	args, err := bindParams(input.GetParams())
	if err != nil {
		return err
	}

	db, engine, err := openConnection(ctx, ax, input.GetConnection())
	if err != nil {
		return err
	}
	defer db.Close()

	// Deliberately no artificial per-query timeout here beyond the
	// connect-validation already done in openConnection: row iteration runs
	// under the platform's own pipeline stream ceiling (ctx), not a fixed
	// node-side one, so a legitimately large result isn't cut short.
	rows, err := db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return fmt.Errorf("stream query failed against %s: %w", engine, err)
	}
	defer rows.Close()

	columns, colTypes, err := columnsFromRows(rows)
	if err != nil {
		return fmt.Errorf("reading column metadata: %w", err)
	}

	// Buffer exactly one row ahead so we know, when emitting row N, whether
	// it is the last one (is_final) without materializing the whole result:
	// only ever one row of lookahead is held at a time.
	hasNext := rows.Next()
	var rowIndex int64
	for hasNext {
		row, err := scanRow(rows, colTypes)
		if err != nil {
			return fmt.Errorf("scanning row %d: %w", rowIndex, err)
		}
		hasNext = rows.Next()
		frame := &gen.QueryRowFrame{
			Columns:  columns,
			Row:      row,
			RowIndex: rowIndex,
			IsFinal:  !hasNext,
		}
		if err := emit(frame); err != nil {
			return err
		}
		rowIndex++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating rows: %w", err)
	}
	return nil
}
