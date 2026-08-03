package nodes

import (
	"database/sql"
	"testing"
	"time"

	gen "christiangeorgelucas/sql-connector/gen"
)

// TestEngineForDSN_ScemeRouting is the independent oracle for engine
// selection: given a DSN scheme, hand-computed expected (driver, engine)
// pairs, plus the reject case for an unsupported scheme.
func TestEngineForDSN_ScemeRouting(t *testing.T) {
	cases := []struct {
		dsn        string
		wantDriver string
		wantEngine string
		wantErr    bool
	}{
		{"postgres://u:p@host:5432/db", "pgx", "postgres", false},
		{"postgresql://u:p@host:5432/db", "pgx", "postgres", false},
		{"mysql://u:p@host:3306/db", "mysql", "mysql", false},
		{"POSTGRES://u:p@host:5432/db", "pgx", "postgres", false}, // scheme is case-insensitive
		{"sqlite:///tmp/db.sqlite", "", "", true},
		{"mongodb://u:p@host:27017/db", "", "", true},
		{"not a url at all://", "", "", true},
	}
	for _, c := range cases {
		driver, engine, err := engineForDSN(c.dsn)
		if c.wantErr {
			if err == nil {
				t.Errorf("engineForDSN(%q): expected error, got driver=%q engine=%q", c.dsn, driver, engine)
			}
			continue
		}
		if err != nil {
			t.Errorf("engineForDSN(%q): unexpected error: %v", c.dsn, err)
			continue
		}
		if driver != c.wantDriver || engine != c.wantEngine {
			t.Errorf("engineForDSN(%q) = (%q, %q), want (%q, %q)", c.dsn, driver, engine, c.wantDriver, c.wantEngine)
		}
	}
}

// TestBindParam_TypeTranslation is the independent oracle for parameter
// binding: each ParamType's string value is hand-computed against the exact
// native Go value it must produce.
func TestBindParam_TypeTranslation(t *testing.T) {
	v, err := bindParam(&gen.Param{Type: gen.ParamType_PARAM_TYPE_STRING, Value: "hello"})
	if err != nil || v != "hello" {
		t.Errorf("STRING: got (%v, %v), want (\"hello\", nil)", v, err)
	}

	v, err = bindParam(&gen.Param{Type: gen.ParamType_PARAM_TYPE_UNSPECIFIED, Value: "raw"})
	if err != nil || v != "raw" {
		t.Errorf("UNSPECIFIED (defaults to string): got (%v, %v), want (\"raw\", nil)", v, err)
	}

	v, err = bindParam(&gen.Param{Type: gen.ParamType_PARAM_TYPE_INT, Value: "42"})
	if err != nil || v != int64(42) {
		t.Errorf("INT: got (%v, %v), want (int64(42), nil)", v, err)
	}
	if _, err := bindParam(&gen.Param{Type: gen.ParamType_PARAM_TYPE_INT, Value: "not-a-number"}); err == nil {
		t.Error("INT with garbage value: expected error, got nil")
	}

	v, err = bindParam(&gen.Param{Type: gen.ParamType_PARAM_TYPE_FLOAT, Value: "3.14"})
	if err != nil || v != 3.14 {
		t.Errorf("FLOAT: got (%v, %v), want (3.14, nil)", v, err)
	}

	v, err = bindParam(&gen.Param{Type: gen.ParamType_PARAM_TYPE_BOOL, Value: "true"})
	if err != nil || v != true {
		t.Errorf("BOOL true: got (%v, %v), want (true, nil)", v, err)
	}
	v, err = bindParam(&gen.Param{Type: gen.ParamType_PARAM_TYPE_BOOL, Value: "false"})
	if err != nil || v != false {
		t.Errorf("BOOL false: got (%v, %v), want (false, nil)", v, err)
	}

	// NULL is the ONLY way to bind SQL NULL — value is ignored regardless of
	// its content, including a non-empty one, which proves type (not value)
	// is the null signal.
	v, err = bindParam(&gen.Param{Type: gen.ParamType_PARAM_TYPE_NULL, Value: "ignored-should-not-matter"})
	if err != nil || v != nil {
		t.Errorf("NULL: got (%v, %v), want (nil, nil)", v, err)
	}

	v, err = bindParam(&gen.Param{Type: gen.ParamType_PARAM_TYPE_TIMESTAMP, Value: "2024-01-15T09:30:00Z"})
	if err != nil {
		t.Fatalf("TIMESTAMP: unexpected error: %v", err)
	}
	tm, ok := v.(time.Time)
	if !ok {
		t.Fatalf("TIMESTAMP: got %T, want time.Time", v)
	}
	if !tm.Equal(time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC)) {
		t.Errorf("TIMESTAMP: got %v, want 2024-01-15T09:30:00Z", tm)
	}
	if _, err := bindParam(&gen.Param{Type: gen.ParamType_PARAM_TYPE_TIMESTAMP, Value: "not-a-timestamp"}); err == nil {
		t.Error("TIMESTAMP with garbage value: expected error, got nil")
	}

	v, err = bindParam(&gen.Param{Type: gen.ParamType_PARAM_TYPE_BYTES, Value: "aGVsbG8="}) // base64("hello")
	if err != nil {
		t.Fatalf("BYTES: unexpected error: %v", err)
	}
	b, ok := v.([]byte)
	if !ok || string(b) != "hello" {
		t.Errorf("BYTES: got %v, want []byte(\"hello\")", v)
	}
	if _, err := bindParam(&gen.Param{Type: gen.ParamType_PARAM_TYPE_BYTES, Value: "not valid base64!!"}); err == nil {
		t.Error("BYTES with invalid base64: expected error, got nil")
	}
}

// TestBindParams_IndexedError proves a param-binding failure names WHICH
// positional parameter failed, not just that binding failed.
func TestBindParams_IndexedError(t *testing.T) {
	_, err := bindParams([]*gen.Param{
		{Type: gen.ParamType_PARAM_TYPE_STRING, Value: "ok"},
		{Type: gen.ParamType_PARAM_TYPE_INT, Value: "also-not-a-number"},
	})
	if err == nil {
		t.Fatal("expected an error for the malformed second param, got nil")
	}
	if got := err.Error(); !contains(got, "params[1]") {
		t.Errorf("error %q does not name the failing index params[1]", got)
	}
}

// TestFormatValue_NullVsZero is THE review trap this package's design
// exists to solve: a legitimate zero-value (empty string, "0", "false")
// must never be confused with SQL NULL. is_null is the only authoritative
// signal — hand-computed pairs below.
func TestFormatValue_NullVsZero(t *testing.T) {
	// The actual NULL case: driver hands back a nil interface{}.
	got := formatValue("text", nil)
	if !got.GetIsNull() || got.GetValue() != "" {
		t.Errorf("nil driver value: got {value:%q is_null:%v}, want {value:\"\" is_null:true}", got.GetValue(), got.GetIsNull())
	}

	// A real empty string is NOT null.
	got = formatValue("text", "")
	if got.GetIsNull() || got.GetValue() != "" {
		t.Errorf("empty string driver value: got {value:%q is_null:%v}, want {value:\"\" is_null:false}", got.GetValue(), got.GetIsNull())
	}

	// A real zero integer is NOT null.
	got = formatValue("int4", int64(0))
	if got.GetIsNull() || got.GetValue() != "0" {
		t.Errorf("int64(0) driver value: got {value:%q is_null:%v}, want {value:\"0\" is_null:false}", got.GetValue(), got.GetIsNull())
	}

	// A real false boolean is NOT null.
	got = formatValue("bool", false)
	if got.GetIsNull() || got.GetValue() != "false" {
		t.Errorf("bool(false) driver value: got {value:%q is_null:%v}, want {value:\"false\" is_null:false}", got.GetValue(), got.GetIsNull())
	}
}

// TestFormatValue_TypeMapping is the independent-oracle type mapping table:
// hand-computed expected string renderings for every Go type the drivers
// hand back, keyed by the same switch formatValue uses internally — verifying
// the RENDERING (strconv/format calls), not re-deriving the switch itself.
func TestFormatValue_TypeMapping(t *testing.T) {
	cases := []struct {
		name   string
		dbType string
		v      interface{}
		want   string
	}{
		{"bool true", "bool", true, "true"},
		{"bool false", "bool", false, "false"},
		{"int64", "int8", int64(9223372036854775807), "9223372036854775807"},
		{"int64 negative", "int4", int64(-42), "-42"},
		{"float64", "float8", 3.14159, "3.14159"},
		{"string", "varchar", "hello world", "hello world"},
		{"string unicode", "text", "café — 日本語 — 🎉", "café — 日本語 — 🎉"},
		{"text bytes", "varchar", []byte("plain text"), "plain text"},
		{"binary bytes", "bytea", []byte{0xDE, 0xAD, 0xBE, 0xEF}, "3q2+7w=="}, // base64
		{"binary blob mysql", "BLOB", []byte{0x00, 0x01}, "AAE="},
	}
	for _, c := range cases {
		got := formatValue(c.dbType, c.v)
		if got.GetIsNull() {
			t.Errorf("%s: unexpectedly marked is_null", c.name)
		}
		if got.GetValue() != c.want {
			t.Errorf("%s: formatValue(%q, %v) = %q, want %q", c.name, c.dbType, c.v, got.GetValue(), c.want)
		}
	}
}

// TestIsBinaryDBType checks the binary-vs-text classification used to decide
// base64 encoding for []byte driver values.
func TestIsBinaryDBType(t *testing.T) {
	binary := []string{"bytea", "BYTEA", "BLOB", "TINYBLOB", "LONGBLOB", "BINARY", "VARBINARY"}
	for _, dt := range binary {
		if !isBinaryDBType(dt) {
			t.Errorf("isBinaryDBType(%q) = false, want true", dt)
		}
	}
	text := []string{"varchar", "text", "TEXT", "character varying", "int4", "timestamp"}
	for _, dt := range text {
		if isBinaryDBType(dt) {
			t.Errorf("isBinaryDBType(%q) = true, want false", dt)
		}
	}
}

// TestColumnInfoFromRow_HasDefaultVsEmptyDefault covers the same
// NULL-vs-zero trap for DescribeTable's introspection parsing: a column
// with NO default (sql.NullString{Valid:false}) must be distinguishable
// from a column whose default IS the literal empty string.
func TestColumnInfoFromRow_HasDefaultVsEmptyDefault(t *testing.T) {
	noDefault := columnInfoFromRow("id", "integer", "NO", sql.NullString{Valid: false}, 1, true)
	if noDefault.GetHasDefault() || noDefault.GetDefaultValue() != "" {
		t.Errorf("no default: got {has_default:%v default_value:%q}, want {false \"\"}", noDefault.GetHasDefault(), noDefault.GetDefaultValue())
	}

	emptyStringDefault := columnInfoFromRow("label", "character varying", "YES", sql.NullString{String: "", Valid: true}, 2, false)
	if !emptyStringDefault.GetHasDefault() || emptyStringDefault.GetDefaultValue() != "" {
		t.Errorf("empty-string default: got {has_default:%v default_value:%q}, want {true \"\"}", emptyStringDefault.GetHasDefault(), emptyStringDefault.GetDefaultValue())
	}

	realDefault := columnInfoFromRow("status", "character varying", "NO", sql.NullString{String: "'active'::character varying", Valid: true}, 3, false)
	if !realDefault.GetHasDefault() || realDefault.GetDefaultValue() != "'active'::character varying" {
		t.Errorf("real default: got {has_default:%v default_value:%q}", realDefault.GetHasDefault(), realDefault.GetDefaultValue())
	}

	if !noDefault.GetPrimaryKey() {
		t.Error("expected primary_key=true for the id column")
	}
	if noDefault.GetNullable() {
		t.Error("is_nullable=\"NO\" must map to Nullable=false")
	}
	if !emptyStringDefault.GetNullable() {
		t.Error("is_nullable=\"YES\" must map to Nullable=true")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(substr) > 0 && indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
