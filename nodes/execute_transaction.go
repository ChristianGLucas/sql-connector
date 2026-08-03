package nodes

import (
	"context"
	"fmt"
	"time"

	"christiangeorgelucas/sql-connector/axiom"
	gen "christiangeorgelucas/sql-connector/gen"
)

// txDuration bounds a whole multi-statement transaction. It is longer than
// queryTimeout (a single statement's budget) since ExecuteTransaction runs
// several statements on one connection before committing.
const txDuration = 60 * time.Second

// ExecuteTransaction runs multiple parameterized statements as one
// all-or-nothing transaction on a single connection. A STATEMENT failure
// (bad SQL, a constraint violation, an invalid param) rolls back the whole
// transaction and is reported IN the response (committed=false,
// failed_statement_index, error) rather than as a node error — the caller's
// flow can branch on it. A CONNECTION-level failure (can't reach the
// database, can't start a transaction) is a node error instead, consistent
// with every other node in this package.
func ExecuteTransaction(ctx context.Context, ax axiom.Context, input *gen.ExecuteTransactionRequest) (*gen.ExecuteTransactionResponse, error) {
	statements := input.GetStatements()

	// Bind every statement's params up front, before opening a connection —
	// a malformed param is a caller input error, not a database condition,
	// so it should fail without ever touching the database (and without
	// having to roll back a transaction that never needed to start).
	boundArgs := make([][]interface{}, len(statements))
	for i, stmt := range statements {
		args, err := bindParams(stmt.GetParams())
		if err != nil {
			return &gen.ExecuteTransactionResponse{
				Committed:            false,
				FailedStatementIndex: int32(i),
				Error:                err.Error(),
			}, nil
		}
		boundArgs[i] = args
	}

	db, engine, err := openConnection(ctx, ax, input.GetConnection())
	if err != nil {
		return nil, err
	}
	defer db.Close()

	txCtx, cancel := context.WithTimeout(ctx, txDuration)
	defer cancel()

	tx, err := db.BeginTx(txCtx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction against %s: %w", engine, err)
	}

	results := make([]*gen.StatementResult, 0, len(statements))
	for i, stmt := range statements {
		res, err := tx.ExecContext(txCtx, stmt.GetSql(), boundArgs[i]...)
		if err != nil {
			_ = tx.Rollback()
			return &gen.ExecuteTransactionResponse{
				Committed:            false,
				FailedStatementIndex: int32(i),
				Error:                err.Error(),
			}, nil
		}
		sr := &gen.StatementResult{}
		if ra, err := res.RowsAffected(); err == nil {
			sr.RowsAffected = ra
		}
		if engine == "mysql" {
			if id, err := res.LastInsertId(); err == nil {
				sr.LastInsertId = id
				sr.LastInsertIdSupported = true
			}
		}
		results = append(results, sr)
	}

	if err := tx.Commit(); err != nil {
		return &gen.ExecuteTransactionResponse{
			Committed:            false,
			FailedStatementIndex: -1,
			Error:                fmt.Sprintf("commit failed: %v", err),
		}, nil
	}

	return &gen.ExecuteTransactionResponse{
		Committed:            true,
		Results:              results,
		FailedStatementIndex: -1,
	}, nil
}
