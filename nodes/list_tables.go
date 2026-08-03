package nodes

import (
	"context"
	"fmt"
	"strings"

	"christiangeorgelucas/sql-connector/axiom"
	gen "christiangeorgelucas/sql-connector/gen"
)

// ListTables lists tables and views visible to the connection via
// information_schema, so the same request shape works identically on
// PostgreSQL and MySQL.
func ListTables(ctx context.Context, ax axiom.Context, input *gen.ListTablesRequest) (*gen.ListTablesResponse, error) {
	db, engine, err := openConnection(ctx, ax, input.GetConnection())
	if err != nil {
		return nil, err
	}
	defer db.Close()

	schema := strings.TrimSpace(input.GetSchema())

	var query string
	var args []interface{}
	switch engine {
	case "postgres":
		if schema == "" {
			schema = "public"
		}
		query = `SELECT table_schema, table_name, table_type FROM information_schema.tables WHERE table_schema = $1 ORDER BY table_name`
		args = []interface{}{schema}
	default: // mysql
		if schema == "" {
			query = `SELECT table_schema, table_name, table_type FROM information_schema.tables WHERE table_schema = DATABASE() ORDER BY table_name`
		} else {
			query = `SELECT table_schema, table_name, table_type FROM information_schema.tables WHERE table_schema = ? ORDER BY table_name`
			args = []interface{}{schema}
		}
	}

	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	rows, err := db.QueryContext(queryCtx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing tables against %s: %w", engine, err)
	}
	defer rows.Close()

	var tables []*gen.TableInfo
	for rows.Next() {
		var s, n, t string
		if err := rows.Scan(&s, &n, &t); err != nil {
			return nil, fmt.Errorf("scanning table row: %w", err)
		}
		tables = append(tables, &gen.TableInfo{Schema: s, Name: n, TableType: t})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating tables: %w", err)
	}

	return &gen.ListTablesResponse{Tables: tables}, nil
}
