# sql-connector

Generic SQL execution template for **PostgreSQL** and **MySQL** — the STATE
pillar for agents that need to query and write databases. Built for the
[Axiom](https://axiomide.com) marketplace, MIT licensed.

**This package is not directly invocable.** Each node here is a
[Generic node](https://axiomide.com) (`kind: generic`) — a published,
deployed template whose connection is intentionally unbound. A consumer
binds one to a real database as a reusable **Instance** (one per
database/credential, via `axiom instance create`), and it's the *Instance*
that becomes the callable, addressable node: its own callers supply only the
query-shaped fields (`sql`, `params`, etc.) — never a connection string. See
[Creating your own database Instance](#creating-your-own-database-instance)
below.

**Boundary:** this package *executes* against live databases. It does not
parse or transpile SQL text — see
[`christiangeorgelucas/sql-tools`](https://github.com/ChristianGLucas/sql-tools)
for that.

## Nodes (generic templates)

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

## Creating your own database Instance

Bind any node above to a real database with `axiom instance create`. Pin the
DSN as a literal config constant for a **public, non-sensitive** database:

```bash
axiom instance create \
  --generic-package christiangeorgelucas/sql-connector \
  --generic-node Query \
  --generic-version 0.1.1 \
  --description "Query my-app's production database (read-only)" \
  --input-field sql=string \
  --input-field params={kind:message,repeated:true,message:christiangeorgelucas/sql-connector.Param} \
  --input-field max_rows=int32 \
  --map connection.dsn="'postgres://reader:PASSWORD@host:5432/mydb'"
```

Or, for a **private** database, declare a secret name on the Instance and
pin `connection.dsn_secret_name` to that same name — then set the secret's
actual value once, under **Console → Secrets**:

```bash
axiom instance create \
  --generic-package christiangeorgelucas/sql-connector \
  --generic-node Query \
  --generic-version 0.1.1 \
  --description "Query my private production database" \
  --required-secret MY_DB_DSN \
  --input-field sql=string \
  --input-field params={kind:message,repeated:true,message:christiangeorgelucas/sql-connector.Param} \
  --input-field max_rows=int32 \
  --map connection.dsn_secret_name="'MY_DB_DSN'"
```

Repeat for each node you need (Ping, ListTables, DescribeTable,
ExecuteTransaction, StreamQueryRows), and see the
[axiom-instance-authoring guide](https://axiomide.com) for the full
`axiom instance create` reference — including `--from` batch manifests for
binding several nodes to the same database at once, and `axiom instance
preview` for checking a mapping before you commit to it.

## Get started free

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

## Idempotency

Axiom invokes nodes **at-least-once**. A retried delivery can re-execute
`Execute` or `ExecuteTransaction` more than once for the same logical
execution. Prefer naturally idempotent statements (`INSERT ... ON CONFLICT
DO NOTHING`, `ON DUPLICATE KEY UPDATE`, a unique-key upsert) or derive your
own idempotency key and de-duplicate downstream.
