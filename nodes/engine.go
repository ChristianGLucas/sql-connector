// Package nodes' engine.go holds the shared connection/param/value plumbing
// used by every node in this package. It is a helper file, not a node — it
// is not listed in axiom.yaml.
package nodes

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-mysql-org/go-mysql/driver" // registers the "mysql" database/sql driver
	_ "github.com/jackc/pgx/v5/stdlib"          // registers the "pgx" database/sql driver

	"christiangeorgelucas/sql-connector/axiom"
	gen "christiangeorgelucas/sql-connector/gen"
)

// connectTimeout bounds how long opening + validating a connection may take,
// so an unreachable host fails with a clean typed error instead of hanging
// on an OS-level TCP timeout. It does NOT bound the query/exec/transaction
// itself once connectivity is confirmed.
const connectTimeout = 15 * time.Second

// queryTimeout bounds a single Query/Execute/ExecuteTransaction/ListTables/
// DescribeTable call end-to-end once connected. StreamQueryRows deliberately
// does not use this — its row iteration runs under the platform's own
// pipeline stream ceiling instead, so a legitimately large result isn't cut
// off by a fixed node-side timeout.
const queryTimeout = 30 * time.Second

// engineForDSN inspects the DSN's URL scheme to pick the database/sql driver
// and a human-readable engine label. This is the ONLY place engine selection
// happens — everything downstream is engine-agnostic except where SQL syntax
// genuinely differs (introspection queries, placeholder style).
func engineForDSN(dsn string) (driverName, engine string, err error) {
	u, err := url.Parse(dsn)
	if err != nil {
		// Deliberately NOT wrapping the underlying url.Parse error: its
		// message embeds the offending input VERBATIM (Go's url.Error
		// formats as `parse "<input>": <reason>`), which would leak a
		// secret-sourced DSN — credentials and all — into this node's
		// returned error, and from there into logs/execution history.
		return "", "", fmt.Errorf("invalid connection DSN: malformed URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "postgres", "postgresql":
		return "pgx", "postgres", nil
	case "mysql":
		return "mysql", "mysql", nil
	default:
		return "", "", fmt.Errorf("unsupported DSN scheme %q: connection secret must start with postgres://, postgresql://, or mysql://", u.Scheme)
	}
}

// resolveDSN implements ConnectionConfig's two-way precedence: a named
// secret wins over the raw `dsn` field when both are set; leaving both
// empty is a structured error. NOTE (2026-08-03): dsn_secret_name is not
// currently deliverable — this package declares no required_secrets (any
// caller can name any secret of their own; there is no single fixed name
// to declare), and the platform only forwards a secret whose name is
// declared. ax.Secrets().Get(name) reliably returns not-found regardless
// of whether that secret is actually configured. `dsn` is the only
// currently-working path; see ConnectionConfig's doc comment.
func resolveDSN(ax axiom.Context, cfg *gen.ConnectionConfig) (string, error) {
	name := strings.TrimSpace(cfg.GetDsnSecretName())
	if name != "" {
		dsn, ok := ax.Secrets().Get(name)
		// An empty resolved value is treated the same as "not configured" —
		// a clear, actionable error naming the secret, rather than falling
		// through into an opaque "unsupported DSN scheme" from downstream.
		if !ok || dsn == "" {
			return "", fmt.Errorf("required secret %q is not configured", name)
		}
		return dsn, nil
	}
	if raw := strings.TrimSpace(cfg.GetDsn()); raw != "" {
		return raw, nil
	}
	return "", fmt.Errorf("connection.dsn_secret_name or connection.dsn is required")
}

// openConnection resolves the connection's DSN (secret or raw, see
// resolveDSN), opens a FRESH *sql.DB for this invocation, and validates
// reachability with a bounded timeout. Nodes are stateless — no connection
// pool is kept across invocations, so the caller must db.Close() when done
// (every node does, via defer, immediately after this returns successfully).
func openConnection(ctx context.Context, ax axiom.Context, cfg *gen.ConnectionConfig) (*sql.DB, string, error) {
	dsn, err := resolveDSN(ax, cfg)
	if err != nil {
		return nil, "", err
	}
	driverName, engine, err := engineForDSN(dsn)
	if err != nil {
		return nil, "", err
	}
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, "", fmt.Errorf("opening %s connection: %w", engine, err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, "", fmt.Errorf("connecting to %s database: %w", engine, err)
	}
	return db, engine, nil
}

// bindParams converts every request Param into the native Go value its
// ParamType calls for, ready to pass as a database/sql query argument.
func bindParams(params []*gen.Param) ([]interface{}, error) {
	out := make([]interface{}, len(params))
	for i, p := range params {
		v, err := bindParam(p)
		if err != nil {
			return nil, fmt.Errorf("params[%d]: %w", i, err)
		}
		out[i] = v
	}
	return out, nil
}

func bindParam(p *gen.Param) (interface{}, error) {
	switch p.GetType() {
	case gen.ParamType_PARAM_TYPE_NULL:
		return nil, nil
	case gen.ParamType_PARAM_TYPE_INT:
		n, err := strconv.ParseInt(p.GetValue(), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid PARAM_TYPE_INT value %q: %w", p.GetValue(), err)
		}
		return n, nil
	case gen.ParamType_PARAM_TYPE_FLOAT:
		f, err := strconv.ParseFloat(p.GetValue(), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid PARAM_TYPE_FLOAT value %q: %w", p.GetValue(), err)
		}
		return f, nil
	case gen.ParamType_PARAM_TYPE_BOOL:
		b, err := strconv.ParseBool(p.GetValue())
		if err != nil {
			return nil, fmt.Errorf("invalid PARAM_TYPE_BOOL value %q: %w", p.GetValue(), err)
		}
		return b, nil
	case gen.ParamType_PARAM_TYPE_TIMESTAMP:
		t, err := time.Parse(time.RFC3339, p.GetValue())
		if err != nil {
			return nil, fmt.Errorf("invalid PARAM_TYPE_TIMESTAMP value %q (want RFC 3339): %w", p.GetValue(), err)
		}
		return t, nil
	case gen.ParamType_PARAM_TYPE_BYTES:
		b, err := base64.StdEncoding.DecodeString(p.GetValue())
		if err != nil {
			return nil, fmt.Errorf("invalid PARAM_TYPE_BYTES value (want base64): %w", err)
		}
		return b, nil
	default: // PARAM_TYPE_UNSPECIFIED, PARAM_TYPE_STRING
		return p.GetValue(), nil
	}
}

// columnsFromRows reads column metadata off an open *sql.Rows. Returns both
// the wire-ready Column messages and the raw *sql.ColumnType slice (needed
// again by scanRow to make binary-vs-text formatting decisions per column).
func columnsFromRows(rows *sql.Rows) ([]*gen.Column, []*sql.ColumnType, error) {
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, nil, err
	}
	cols := make([]*gen.Column, len(colTypes))
	for i, ct := range colTypes {
		cols[i] = &gen.Column{Name: ct.Name(), DbType: ct.DatabaseTypeName()}
	}
	return cols, colTypes, nil
}

// scanRow reads the current row (caller must have already called
// rows.Next() and confirmed it returned true) into a wire-ready Row.
func scanRow(rows *sql.Rows, colTypes []*sql.ColumnType) (*gen.Row, error) {
	n := len(colTypes)
	raw := make([]interface{}, n)
	ptrs := make([]interface{}, n)
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	values := make([]*gen.Value, n)
	for i, v := range raw {
		values[i] = formatValue(colTypes[i].DatabaseTypeName(), v)
	}
	return &gen.Row{Values: values}, nil
}

// formatValue converts one scanned driver value into the wire Value
// wrapper. A nil v means SQL NULL — the ONLY signal Value.is_null carries;
// every non-nil branch below always yields is_null=false, even when the
// resulting string happens to be empty ("" is a real value, not NULL).
func formatValue(dbType string, v interface{}) *gen.Value {
	if v == nil {
		return &gen.Value{IsNull: true}
	}
	switch t := v.(type) {
	case bool:
		return &gen.Value{Value: strconv.FormatBool(t)}
	case int64:
		return &gen.Value{Value: strconv.FormatInt(t, 10)}
	case int32:
		return &gen.Value{Value: strconv.FormatInt(int64(t), 10)}
	case int:
		return &gen.Value{Value: strconv.Itoa(t)}
	case float64:
		return &gen.Value{Value: strconv.FormatFloat(t, 'g', -1, 64)}
	case float32:
		return &gen.Value{Value: strconv.FormatFloat(float64(t), 'g', -1, 32)}
	case time.Time:
		return &gen.Value{Value: t.UTC().Format(time.RFC3339Nano)}
	case string:
		return &gen.Value{Value: t}
	case []byte:
		if isBinaryDBType(dbType) {
			return &gen.Value{Value: base64.StdEncoding.EncodeToString(t)}
		}
		return &gen.Value{Value: string(t)}
	default:
		return &gen.Value{Value: fmt.Sprintf("%v", t)}
	}
}

// columnInfoFromRow converts one information_schema.columns (+ primary-key
// join) row into a wire-ready ColumnInfo. Pulled out as a pure function so
// introspection parsing — in particular the has_default/NULL-vs-zero
// distinction on defaultValue — is unit-testable without a live database.
func columnInfoFromRow(name, dbType, isNullable string, defaultValue sql.NullString, ordinalPosition int64, isPrimaryKey bool) *gen.ColumnInfo {
	return &gen.ColumnInfo{
		Name:            name,
		DbType:          dbType,
		Nullable:        strings.EqualFold(isNullable, "YES"),
		PrimaryKey:      isPrimaryKey,
		DefaultValue:    defaultValue.String,
		HasDefault:      defaultValue.Valid,
		OrdinalPosition: int32(ordinalPosition),
	}
}

// isBinaryDBType reports whether a driver-reported column type name denotes
// genuinely binary data (base64-encode it) as opposed to text that merely
// arrived as a []byte on the wire (common for TEXT/VARCHAR columns on some
// drivers) — the latter is rendered as plain text.
func isBinaryDBType(dbType string) bool {
	dt := strings.ToUpper(dbType)
	return strings.Contains(dt, "BYTEA") || strings.Contains(dt, "BLOB") ||
		strings.Contains(dt, "BINARY") || strings.Contains(dt, "VARBINARY")
}
