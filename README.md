# sql-connector

Generic SQL execution against live **PostgreSQL** and **MySQL** databases via a
bring-your-own connection string — the STATE pillar for agents that need to
query and write their own databases. Parameterized query/execute/transaction/
introspection nodes (unary, for typical request/response use) plus a true
streaming row-cursor node (pipeline, for unbounded result sets). Built for the
[Axiom](https://axiomide.com) marketplace, MIT licensed.

**Boundary:** this package *executes* against live databases. It does not
parse or transpile SQL text — see
[`christiangeorgelucas/sql-tools`](https://github.com/ChristianGLucas/sql-tools)
for that.

## Use it from your agent or app

Every node in this package is a **live, auto-scaling API endpoint** on the
[Axiom](https://axiomide.com) marketplace — call it from an AI agent or your
own code, with nothing to self-host.

**📦 See it on the marketplace:**
https://dev.axiomide.com/marketplace/christiangeorgelucas/sql-connector@0.1.0

**Hook it up to an AI agent (MCP).** Add Axiom's hosted MCP server to any MCP
client and every node becomes a typed tool your agent can call — search the
catalog, inspect a schema, and invoke it directly.

```bash
# Claude Code
claude mcp add --transport http axiom https://api.axiomide.com/mcp \
  --header "Authorization: Bearer $AXIOM_API_KEY"
```

Claude Desktop, Cursor, or any config-based client:

```json
{
  "mcpServers": {
    "axiom": {
      "type": "http",
      "url": "https://api.axiomide.com/mcp",
      "headers": { "Authorization": "Bearer YOUR_AXIOM_API_KEY" }
    }
  }
}
```

**Call it from the CLI.**

```bash
axiom invoke christiangeorgelucas/sql-connector/Query --input '{"connection":{"dsnSecretName":"MY_DB_DSN"},"sql":"SELECT id, name FROM users WHERE id = $1","params":[{"type":"PARAM_TYPE_INT","value":"42"}],"maxRows":100}'
```

**Call it over HTTP.**

```bash
curl -X POST https://api.axiomide.com/invocations/v1/nodes/christiangeorgelucas/sql-connector/0.1.0/Query \
  -H "Authorization: Bearer $AXIOM_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"connection":{"dsnSecretName":"MY_DB_DSN"},"sql":"SELECT id, name FROM users WHERE id = $1","params":[{"type":"PARAM_TYPE_INT","value":"42"}],"maxRows":100}'
```

### Get started free

Install the CLI:

```bash
# macOS / Linux — Homebrew
brew install axiomide/tap/axiom

# macOS / Linux — install script
curl -fsSL https://raw.githubusercontent.com/AxiomIDE/axiom-releases/main/install.sh | sh
```

**Windows:** download the `windows/amd64` `.zip` from the
[releases page](https://github.com/AxiomIDE/axiom-releases/releases), unzip
it, and put `axiom.exe` on your `PATH`.

Then `axiom version` to verify, `axiom login` (GitHub or Google) to
authenticate, and create an API key under **Console → API Keys**. Docs and
sign-up at **[axiomide.com](https://axiomide.com)**.

## Connecting to your database

Every node takes a `connection` with two ways to supply the DSN:

- **`connection.dsn`** — **the working path today.** A raw DSN. A value
  placed here is visible in flow definitions and execution history, so use
  it for publicly-documented or throwaway credentials only (e.g. a published
  read-only public database) — never a real credential.
- **`connection.dsn_secret_name`** — **not yet deliverable.** The intended
  design: name a tenant secret (Console → Secrets) holding your full
  connection string, resolved server-side at invocation time so a real
  credential never appears in a flow manifest, log, or node input. This
  field is present in the schema and the design is sound, but the current
  platform only delivers a secret to a node whose name is declared in that
  node's `required_secrets` — and this package deliberately declares none
  (the whole point of this field is letting any caller name any secret of
  their own, not one fixed name the package author picks). Until a
  secret-grants model closes that gap, a secret named here will not
  resolve — use `dsn` instead. When it does become deliverable, the secret
  will take precedence over `dsn` if both are set.

The DSN's scheme selects the engine either way:

- `postgres://user:pass@host:5432/dbname` or `postgresql://...` → PostgreSQL
  (via [pgx](https://github.com/jackc/pgx))
- `mysql://user:pass@host:3306/dbname` → MySQL
  (via [go-mysql-org/go-mysql](https://github.com/go-mysql-org/go-mysql))

## Nodes

**Unary** (request/response — most agent queries):

- **Query** — parameterized SELECT → columns, rows (NULL explicitly
  distinguished from a zero-value via an `is_null` flag on every cell),
  row_count, and a `truncated` flag when `max_rows` capped the result.
- **Execute** — parameterized INSERT/UPDATE/DELETE/DDL → rows_affected +
  last_insert_id where the engine supports it (MySQL only).
- **ExecuteTransaction** — multiple parameterized statements, all-or-nothing,
  on one connection. Full rollback + the failing statement's index and error
  on any failure.
- **Ping** — connectivity check + server version, with a bounded timeout so
  an unreachable host fails fast instead of hanging.
- **ListTables** / **DescribeTable** — schema-qualified introspection via
  `information_schema`, working identically on both engines.

**Pipeline** (streaming — unbounded result sets):

- **StreamQueryRows** — parameterized SELECT streamed one row per frame using
  a real server-side cursor. The full result set is never materialized in
  memory, making this the right choice for tables too large to return from
  `Query`. `is_final` (true only on the last frame) is the business-level
  completion signal; a zero-row result emits no frames at all, and stream
  completion in that case is signaled the same way any pipeline node signals
  it — Axiom's own transport-level stream close (SSE terminal event / gRPC
  stream end), independent of any node payload.

Parameterization is mandatory everywhere: every value binds through the
target engine's native placeholder syntax (`$1, $2, ...` for PostgreSQL, `?`
for MySQL) via the driver's own parameter binding — this connector never
string-interpolates a value into SQL text.

## Idempotency

Axiom invokes nodes **at-least-once**. A retried delivery can re-execute
`Execute` or `ExecuteTransaction` more than once for the same logical
execution. Prefer naturally idempotent statements (`INSERT ... ON CONFLICT
DO NOTHING`, `ON DUPLICATE KEY UPDATE`, a unique-key upsert) or derive your
own idempotency key and de-duplicate downstream.
