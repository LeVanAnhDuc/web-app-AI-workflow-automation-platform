# Flowgrid — AI Workflow Automation Platform

A visual workflow automation platform: draw a graph of nodes, connect them, and the
platform runs it — triggered by hand, by an incoming webhook, or on a schedule. Every run
is recorded node by node, so it can be inspected, replayed and resumed from the node that
failed.

Phase 1 (this repository's current scope) covers the canvas editor, the execution engine
and the execution history. The design that governs it is
[`docs/superpowers/specs/2026-08-18-workflow-platform-core-design.md`](docs/superpowers/specs/2026-08-18-workflow-platform-core-design.md).

## Stack

| Part | Choice |
|---|---|
| Frontend | Next.js (App Router), TypeScript, Tailwind, React Flow, TanStack Query, Zustand |
| Backend | Go — `cmd/api` (REST + SSE + webhook ingress) and `cmd/worker` (execution engine) |
| Storage | Postgres — application state, execution log, and the job queue |
| Expressions | `expr-lang/expr` for `{{ }}`, `goja` for the Code node |

No Redis, no external queue broker: the queue is a Postgres table claimed with
`FOR UPDATE SKIP LOCKED`.

## Running it

```bash
cp .env.example .env          # then edit JWT_SECRET and CREDENTIAL_KEY
make db-up                    # Postgres on :6543
make migrate
make api                      # :8080
make worker                   # in a second shell
make web                      # :3000, proxies /api to the Go API
```

Sign in with the `SEED_EMAIL` / `SEED_PASSWORD` pair from your `.env`.

## Layout

```
cmd/api        REST API, SSE, public webhook ingress
cmd/worker     job claimer, execution engine, schedule ticker
internal/
  domain       items, graph, statuses, error shape
  expr         quote-aware {{ }} evaluator
  nodes        node contract and one file per node type
  engine       graph runner: order, retry, resume
  queue        Postgres job queue
  store        pgx repositories, all scoped by workspace
  api          chi router, handlers, middleware, SSE
  auth crypto config
db/migrations  goose SQL
web            Next.js app
design         approved UI mockups (Design Components source)
docs           specs
```

## Tests

```bash
make test-go     # engine, expressions, nodes, queue, API handlers
make test-web    # typecheck + Vitest
```

Store tests need a real database and skip unless `TEST_DATABASE_URL` is set:

```bash
TEST_DATABASE_URL=postgres://flowgrid:flowgrid@localhost:6543/flowgrid?sslmode=disable go test ./internal/store/...
```
