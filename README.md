# Flowgrid — AI Workflow Automation Platform

A visual workflow automation platform: draw a graph of nodes, connect them, and the platform
runs it — triggered by hand, by an incoming webhook, or on a cron schedule. Every run is
recorded node by node, so it can be inspected, replayed, and resumed from the node that
failed rather than from the beginning.

Phase 1 — this repository's current scope — covers the canvas editor, the execution engine
and the execution history. The design that governs it is
[`docs/superpowers/specs/2026-08-18-workflow-platform-core-design.md`](docs/superpowers/specs/2026-08-18-workflow-platform-core-design.md).

![Workflow editor](docs/screenshots/editor-run.png)

## Stack

| Part | Choice |
|---|---|
| Frontend | Next.js (App Router), TypeScript, Tailwind, React Flow, TanStack Query, Zustand |
| Backend | Go — `cmd/api` (REST + SSE + webhook ingress) and `cmd/worker` (execution engine) |
| Storage | Postgres — application state, execution log, and the job queue |
| Expressions | `expr-lang/expr` for `{{ }}`, `goja` for the Code node |

No Redis and no external queue broker: the queue is a Postgres table claimed with
`FOR UPDATE SKIP LOCKED`.

## Running it

```bash
cp .env.example .env          # then change JWT_SECRET and CREDENTIAL_KEY
make db-up                    # Postgres on :6543
make migrate
make api                      # :8080
make worker                   # in a second shell
make web                      # :3000, proxies /api and /webhook to the Go API
```

Sign in with the `SEED_EMAIL` / `SEED_PASSWORD` pair from your `.env`; the API creates that
workspace and user on boot, idempotently.

## What Phase 1 can do

- **Triggers** — Manual, Webhook (a public `POST /webhook/<path>` that answers `202`
  immediately), and Schedule (five-field cron, per-workflow timezone).
- **Nodes** — HTTP Request, Code (JavaScript under goja), IF, Set, Merge.
- **Expressions** — `{{ $json.field }}`, `{{ $node["Other node"].json.id }}`, `{{ $items }}`,
  `{{ $itemIndex }}`, `{{ $now }}`, `{{ $execution.id }}`. Reaching through a key that is not
  there yields nothing rather than failing the run, because workflow data is shaped by
  whoever sends it.
- **Item semantics** — every node takes and returns a list of items, so "for each of these
  ten records, call an API" needs no loop node.
- **Error handling** — per-node retry with backoff, per-node timeout, continue-on-fail, and
  a per-execution ceiling.
- **Resume** — output is persisted after every node, so a crashed worker resumes without
  re-issuing completed HTTP calls, and "Retry from failed node" restarts at the failure with
  the stored input.
- **Versioning** — saving the graph creates a new version, and an execution points at the
  version it ran. An old execution's replay never drifts when the workflow is edited.

Phases 2–4 (AI nodes, real app connectors with a credential vault, multi-tenant workspaces)
are described in the spec. Phase 1 leaves room for them: `workspace_id` is on every root
table and required by every store call, the `credentials` table and per-node `credentialId`
already exist, and a new node type is one Go file — the frontend renders its whole
configuration form from the descriptor the API returns.

## Screens

| | |
|---|---|
| ![Workflow list](docs/screenshots/workflows.png) | ![Execution detail](docs/screenshots/execution-detail.png) |
| Workflow list | Execution detail, with the read-only graph replay |
| ![Node picker](docs/screenshots/node-picker.png) | ![Executions](docs/screenshots/executions.png) |
| Node picker, driven by the keyboard | Execution history |

## Layout

```
cmd/api        REST API, SSE, public webhook ingress
cmd/worker     job claimer, execution engine, schedule ticker
internal/
  domain       items, graph, statuses, error shape
  expr         quote-aware {{ }} evaluator
  nodes        the node contract and one file per node type
  engine       graph runner: order, retry, skip, resume
  queue        Postgres job queue
  store        pgx repositories, every call scoped by workspace
  api          chi router, handlers, middleware, SSE
  auth crypto config schedule
  integration  end-to-end tests across every layer
db/migrations  goose SQL
web            Next.js app
design         the approved UI mockups (Design Components source)
docs           spec and screenshots
```

## Tests

```bash
make test-go     # engine, expressions, nodes, queue, API handlers, domain
make test-web    # typecheck + Vitest
```

The store, queue and integration suites need a real database and skip without one:

```bash
export TEST_DATABASE_URL=postgres://flowgrid:flowgrid@localhost:6543/flowgrid?sslmode=disable
go test ./...
```

The end-to-end suite drives the real app in a browser, so it needs Postgres, `cmd/api` and
`cmd/worker` running:

```bash
cd web && npm run test:e2e
```

It signs in, builds a two-node workflow on the canvas, connects it, saves, runs it, and
waits for the execution log to go green — crossing Next, the Go API, the Postgres queue and
the worker in one pass.
