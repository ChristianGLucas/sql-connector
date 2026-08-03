package nodes

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"christiangeorgelucas/sql-connector/axiom"
	gen "christiangeorgelucas/sql-connector/gen"
)

// postgresDescribeQuery lists a table's columns with type, nullability,
// default, and primary-key membership in one pass, ordered by position.
const postgresDescribeQuery = `
SELECT c.column_name, c.data_type, c.is_nullable, c.column_default, c.ordinal_position,
       (pk.column_name IS NOT NULL) AS is_primary_key
FROM information_schema.columns c
LEFT JOIN (
  SELECT kcu.column_name
  FROM information_schema.key_column_usage kcu
  JOIN information_schema.table_constraints tc
    ON tc.constraint_name = kcu.constraint_name
   AND tc.table_schema = kcu.table_schema
   AND tc.table_name = kcu.table_name
  WHERE tc.constraint_type = 'PRIMARY KEY'
    AND kcu.table_schema = $1 AND kcu.table_name = $2
) pk ON pk.column_name = c.column_name
WHERE c.table_schema = $1 AND c.table_name = $2
ORDER BY c.ordinal_position`

// mysqlDescribeQuery is the same shape, using ? placeholders and MySQL's
// TRUE/FALSE literals (equivalent to 1/0, which the driver reports as bool
// when the destination is a Go bool).
const mysqlDescribeQuery = `
SELECT c.column_name, c.data_type, c.is_nullable, c.column_default, c.ordinal_position,
       (pk.column_name IS NOT NULL) AS is_primary_key
FROM information_schema.columns c
LEFT JOIN (
  SELECT kcu.column_name
  FROM information_schema.key_column_usage kcu
  JOIN information_schema.table_constraints tc
    ON tc.constraint_name = kcu.constraint_name
   AND tc.table_schema = kcu.table_schema
   AND tc.table_name = kcu.table_name
  WHERE tc.constraint_type = 'PRIMARY KEY'
    AND kcu.table_schema = ? AND kcu.table_name = ?
) pk ON pk.column_name = c.column_name
WHERE c.table_schema = ? AND c.table_name = ?
ORDER BY c.ordinal_position`

// DescribeTable describes one table's columns — name, native database type,
// nullability, primary-key membership, default value, and ordinal
// position — via information_schema, schema-qualified so an agent can write
// a correct query without guessing types.
func DescribeTable(ctx context.Context, ax axiom.Context, input *gen.DescribeTableRequest) (*gen.DescribeTableResponse, error) {
	table := strings.TrimSpace(input.GetTable())
	if table == "" {
		return nil, fmt.Errorf("table is required")
	}

	db, engine, err := openConnection(ctx, ax, input.GetConnection())
	if err != nil {
		return nil, err
	}
	defer db.Close()

	schema := strings.TrimSpace(input.GetSchema())
	if schema == "" {
		if engine == "postgres" {
			schema = "public"
		} else {
			// MySQL: fall back to the DSN's target database.
			resolveCtx, cancel := context.WithTimeout(ctx, connectTimeout)
			defer cancel()
			if err := db.QueryRowContext(resolveCtx, "SELECT DATABASE()").Scan(&schema); err != nil {
				return nil, fmt.Errorf("resolving default schema: %w", err)
			}
		}
	}

	query := postgresDescribeQuery
	if engine == "mysql" {
		query = mysqlDescribeQuery
	}

	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	var rows *sql.Rows
	if engine == "postgres" {
		rows, err = db.QueryContext(queryCtx, query, schema, table)
	} else {
		rows, err = db.QueryContext(queryCtx, query, schema, table, schema, table)
	}
	if err != nil {
		return nil, fmt.Errorf("describing table against %s: %w", engine, err)
	}
	defer rows.Close()

	var columns []*gen.ColumnInfo
	for rows.Next() {
		var name, dbType, isNullable string
		var defaultValue sql.NullString
		var ordinalPosition int64
		var isPrimaryKey bool
		if err := rows.Scan(&name, &dbType, &isNullable, &defaultValue, &ordinalPosition, &isPrimaryKey); err != nil {
			return nil, fmt.Errorf("scanning column row: %w", err)
		}
		columns = append(columns, columnInfoFromRow(name, dbType, isNullable, defaultValue, ordinalPosition, isPrimaryKey))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating columns: %w", err)
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("table %q not found in schema %q (or it has no columns)", table, schema)
	}

	return &gen.DescribeTableResponse{
		Schema:  schema,
		Table:   table,
		Columns: columns,
	}, nil
}
