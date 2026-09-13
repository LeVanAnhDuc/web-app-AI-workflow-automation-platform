# Ducker Flow Grid — AI Workflow Automation Platform

A visual workflow automation platform: draw a graph of nodes, connect them, and the platform
runs it — triggered by hand, by an incoming webhook, or on a cron schedule. Every run is
recorded node by node, so it can be inspected, replayed, and resumed from the node that
failed rather than from the beginning.

Phases 1 to 3 are implemented: the canvas editor, the execution engine, the execution history,
the AI nodes — including an agent that calls other nodes in the graph as tools — and a
credential vault with OAuth2 and real app connectors. The designs that govern them are
[Phase 1 — Core](docs/superpowers/specs/2026-08-18-workflow-platform-core-design.md),
[Phase 2 — AI](docs/superpowers/specs/2026-08-18-workflow-platform-ai-design.md) and
[Phase 3 — Connectors](docs/superpowers/specs/2026-08-19-workflow-platform-connectors-design.md).

**Read [`docs/SESSION-LOG.md`](docs/SESSION-LOG.md) first.** It records why the architecture is
the way it is, every defect found while building it, and — most importantly — exactly which
paths have been verified against a running system and which have only ever been exercised
against a stub.

![Workflow editor](docs/screenshots/editor-run.png)

## Features

- **Graph editor**
  - A React Flow canvas: place nodes, drag edges between handles, rename in place, and
    configure the selected node in a side drawer.
  - A node picker driven by the keyboard, and a parameter form rendered from each node type's
    descriptor rather than hand-written per node.
  - Saving the graph creates a new version, and an execution points at the version it ran, so
    an old execution's replay never drifts when the workflow is edited.
- **Triggers**
  - Manual, from the editor or the workflow list.
  - Webhook — a public `POST /webhook/<path>` that answers `202` immediately.
  - Schedule — five-field cron, with a per-workflow timezone.
- **Execution engine**
  - Every node takes and returns a list of items, so "for each of these ten records, call an
    API" needs no loop node.
  - Per-node retry with backoff, per-node timeout, continue-on-fail, and a per-execution
    ceiling.
  - Output is persisted after every node, so a crashed worker resumes without re-issuing
    completed HTTP calls, and "Retry from failed node" restarts at the failure with the
    stored input.
  - Jobs are claimed from a Postgres table with `FOR UPDATE SKIP LOCKED` — a worker claims only
    the job kinds it can run, and concurrent workers never take the same job.
- **Node library**
  - HTTP Request, Code (JavaScript under goja), IF, Set, Merge.
  - Expressions everywhere: `{{ $json.field }}`, `{{ $node["Other node"].json.id }}`,
    `{{ $items }}`, `{{ $itemIndex }}`, `{{ $now }}`, `{{ $execution.id }}`. Reaching through a
    key that is not there yields nothing rather than failing the run, because workflow data is
    shaped by whoever sends it.
- **AI nodes**
  - **LLM node** — one prompt per item, optionally constrained to a JSON schema so downstream
    nodes can rely on the reply's shape.
  - **AI Agent node** — a tool-calling loop where the tools are *other nodes in the graph*,
    wired into the agent's second input handle so the relationship is visible on the canvas. A
    tool edge is a capability edge, not a data edge: the agent does not wait on its tools, and a
    node that exists only to be a tool is neither run by the main flow nor greyed out as
    skipped.
  - **A transcript worth reading** — the agent's output carries every turn, its token usage, and
    every tool call with arguments and result. An agent that answers wrongly is only debuggable
    if you can see what it looked at.
  - **A provider seam** — `internal/llm` is the only package that knows a vendor SDK exists.
    Set `ANTHROPIC_API_KEY` to enable the AI nodes; leave it unset and everything else still
    works, and those two nodes say what to set rather than failing obscurely.
- **Credential vault**
  - Encrypted secrets a workspace owns. `internal/credentials` is the only package that holds
    the key: the store handles a blob it cannot open, the API returns summaries it never
    decrypts, and a node never sees a secret at all — it hands over a request and gets a signed
    one back.
  - A secret is never returned by any endpoint: the summary says which fields are *set*, which
    is what lets an edit form show "unchanged" instead of an empty box that looks like data
    loss.
- **Connectors and OAuth2**
  - The authorisation-code grant with refresh, for Slack, Gmail and Google Sheets. The redirect
    URL is shown with a copy button, because registering a different one with the provider is
    the most common way the flow fails.
  - **Connectors as declarations** — Slack, Gmail and Google Sheets are data files, and one
    generic executor runs any of them. A connector's node descriptor gates each operation's
    parameters with `ShowWhen`, so the editor renders a correct form for an app nobody wrote UI
    for.
- **Execution history**
  - A run list with filters, and an execution detail view with a read-only graph replay, a node
    timeline, and the input and output of each node.
  - Live progress over SSE while a run is in flight, in both the editor's log panel and the
    execution detail.

**No live OAuth against Google or Slack has been run** — that needs registered client
credentials and a publicly reachable redirect URI. The flow is tested against a stub that
plays both endpoints, including refresh and a replayed state.

Phase 4 (multi-tenant workspaces, RBAC, quota, billing) is described in the Phase 1 spec. The
room for it already exists: `workspace_id` is on every root table and required by every store
call, and a new node type is still one Go file.

## Tech Stack

| Part | Choice |
|---|---|
| Frontend | Next.js (App Router), TypeScript, Tailwind, React Flow, TanStack Query, Zustand |
| Backend | Go — `cmd/api` (REST + SSE + webhook ingress) and `cmd/worker` (execution engine) |
| Storage | Postgres — application state, execution log, and the job queue |
| Expressions | `expr-lang/expr` for `{{ }}`, `goja` for the Code node |
| AI | `anthropic-sdk-go` behind a provider interface in `internal/llm` |
| Secrets | AES-256-GCM, sealed and opened only inside `internal/credentials` |

No Redis and no external queue broker: the queue is a Postgres table claimed with
`FOR UPDATE SKIP LOCKED`.

## Running

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

## Screens

| | |
|---|---|
| ![Workflow list](docs/screenshots/workflows.png) | ![Execution detail](docs/screenshots/execution-detail.png) |
| Workflow list | Execution detail, with the read-only graph replay |
| ![Node picker](docs/screenshots/node-picker.png) | ![Executions](docs/screenshots/executions.png) |
| Node picker, driven by the keyboard | Execution history |
| ![Agent editor](docs/screenshots/agent-editor.png) | ![Agent transcript](docs/screenshots/agent-transcript.png) |
| An agent with a node wired into its tool handle | The transcript: every turn and tool call |
| ![Credentials](docs/screenshots/credentials.png) | ![Credential form](docs/screenshots/credential-form.png) |
| The vault, with honest per-credential status | A form rendered entirely from the type descriptor |

## Project structure

```
cmd/api        REST API, SSE, public webhook ingress
cmd/worker     job claimer, execution engine, schedule ticker
internal/
  domain       items, graph, statuses, error shape
  expr         quote-aware {{ }} evaluator
  credentials  the vault: sealing, the type registry, OAuth2
  connectors   app declarations — Slack, Gmail, Google Sheets
  llm          the language-model provider seam and the Anthropic adapter
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

15 Go packages, 119 frontend unit tests, and 2 Playwright end-to-end tests.

The store, queue and integration suites need a real database and skip without one:

```bash
export TEST_DATABASE_URL=postgres://flowgrid:flowgrid@localhost:6543/flowgrid?sslmode=disable
go test ./...
```

The end-to-end suite drives the real app in a browser, so it needs Postgres, `cmd/api` and
`cmd/worker` running:

```bash
cd web && pnpm test:e2e
```

It signs in, builds a two-node workflow on the canvas, connects it, saves, runs it, and
waits for the execution log to go green — crossing Next, the Go API, the Postgres queue and
the worker in one pass.
