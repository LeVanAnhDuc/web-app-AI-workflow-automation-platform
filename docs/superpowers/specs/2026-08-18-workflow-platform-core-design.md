# Flowgrid — AI Workflow Automation Platform, Phase 1 (Core)

Status: approved 2026-08-18
Scope: Phase 1 only. Phases 2–4 appear here only where Phase 1 must leave room for them.

## 1. Product

A visual workflow automation platform: users draw a graph of nodes on a canvas, connect
them, and the platform executes the graph — triggered manually, by an incoming webhook, or
on a schedule. Every run is recorded node by node so it can be inspected, debugged and
resumed.

Long-term the product is a multi-tenant SaaS with real app connectors and AI agent nodes.
Phase 1 delivers a single-tenant, demo-quality product that runs end to end.

### Phase plan

| Phase | Content |
|---|---|
| **1 · Core** | Canvas editor, execution engine, nodes (manual/webhook/schedule triggers, HTTP, Code, IF, Set, Merge), execution history with live log |
| 2 · AI | LLM node, AI Agent node with tool-calling loop |
| 3 · Connectors | Credential vault, OAuth2, Gmail/Slack/Sheets, declarative connector registry |
| 4 · Multi-tenant | Workspaces/teams UI, RBAC, quota, billing hooks |

Phase 1 leaves room by: `workspace_id` on every root table and required by every store
call; a `credentials` table and `credentialId` on every node; an `LLMProvider`-shaped seam
absent but unblocked by the node registry.

## 2. Architecture

Two Go binaries from one module, one Next.js app, one Postgres.

```
Next.js (web)  --/api/v1 (proxied)-->  Go API (cmd/api)
                                            |  enqueue job row
public webhook --------------------->       v
                                        Postgres  <-- claim job --  Go Worker (cmd/worker)
                                                                       └─ execution engine
```

- **cmd/api** — REST + SSE + public webhook ingress. Never runs workflows.
- **cmd/worker** — claims jobs from the `jobs` table, runs the engine; also runs the
  schedule ticker that enqueues cron-triggered executions.
- **Postgres** — application state, execution log, and the job queue. No Redis, no
  external queue broker in Phase 1.

Next.js has no backend of its own. `next.config.ts` rewrites `/api/*` and `/webhook/*` to
the Go API so the session cookie is same-origin.

### Decision: own job queue instead of River

The queue is a `jobs` table claimed with `SELECT … FOR UPDATE SKIP LOCKED`. Roughly 150
lines in `internal/queue`, no external dependency, fully unit-testable, and it keeps the
design promise (queue lives in Postgres). Revisit if Phase 3 needs features it lacks.

## 3. Data model

Migrations live in `db/migrations`, goose format, applied by `cmd/api` on boot and by
`make migrate`.

```
workspaces        id, name, created_at
users             id, workspace_id, email(unique), password_hash, role, created_at
workflows         id, workspace_id, name, active, active_version_id, created_at, updated_at
workflow_versions id, workflow_id, version, graph(jsonb), created_by, created_at
                  unique(workflow_id, version)
executions        id, workspace_id, workflow_id, workflow_version_id, status,
                  trigger_type, trigger_data(jsonb), error(jsonb), resume_from_node,
                  cancel_requested, created_at, started_at, finished_at
node_executions   id, execution_id, node_id, node_name, node_type, status, attempt,
                  input(jsonb), output(jsonb), error(jsonb), started_at, finished_at
                  unique(execution_id, node_id)
webhooks          id, workspace_id, workflow_id, node_id, path(unique), method, created_at
schedules         id, workspace_id, workflow_id, node_id, cron, timezone,
                  next_run_at, last_run_at   unique(workflow_id, node_id)
credentials       id, workspace_id, type, name, data_encrypted(bytea), created_at, updated_at
jobs              id, kind, payload(jsonb), status, attempt, max_attempts, run_at,
                  locked_at, locked_by, last_error, created_at
```

Two deliberate choices:

- **Workflows are versioned.** Saving the graph creates a new `workflow_versions` row.
  An execution points at the exact version it ran, so an old execution log never drifts
  when the workflow is edited afterwards.
- **`workspace_id` is present from day one** and every repository function takes a
  `workspaceID` argument. Phase 4 opens a UI over it; it never migrates data.

Statuses: `queued`, `running`, `succeeded`, `failed`, `skipped`, `cancelled`.
`skipped` applies to node executions only (a branch that was not taken).

### Graph JSON

```jsonc
{
  "nodes": [{
    "id": "n1",
    "type": "trigger.webhook",
    "name": "New lead",
    "position": { "x": 0, "y": 0 },
    "params": { "path": "lead-in", "method": "POST" },
    "credentialId": null,
    "settings": {
      "retryOnFail": false, "maxTries": 3, "waitBetweenTriesMs": 1000,
      "continueOnFail": false, "timeoutMs": 60000
    }
  }],
  "edges": [
    { "id": "e1", "source": "n1", "sourceHandle": "main", "target": "n2", "targetHandle": "main" }
  ]
}
```

Node names are unique within a graph — expressions reference other nodes by name.

## 4. Data flow between nodes

A node receives and returns an **array of items**, not a single object:

```go
type Item struct { JSON map[string]any `json:"json"` }
```

This is the n8n model, adopted deliberately: "fetch 10 emails → for each, call an API →
write a row" is expressible with no Loop node. A node declares in its descriptor whether
it runs `perItem` (the engine loops and concatenates) or `once` (the engine hands it the
whole set).

### Expressions

Any string param may contain `{{ … }}`. Evaluation lives in `internal/expr` and uses
`github.com/expr-lang/expr` (no filesystem or network access by construction).

Available in scope:

| Name | Meaning |
|---|---|
| `$json` | the current item's JSON (perItem nodes, and item 0 for `once` nodes) |
| `$items` | the full input item array |
| `$node["Name"].json` | first output item of the named node |
| `$node["Name"].items` | all output items of the named node |
| `$itemIndex` | zero-based index of the current item |
| `$now` | RFC3339 string of the current time |
| `$execution.id` | execution id |

`expr-lang` cannot lex `$`-prefixed identifiers, so `expr.Evaluate` preprocesses the
source with a **quote-aware scanner**: walking the string, tracking `'`, `"` and backtick
runs, it rewrites `$ident` outside quotes to `_dollar_ident`, and the environment is built
with `_dollar_` keys. A test must prove `"total: $5"` and `'$json'` survive untouched.

Semantics: if the whole param value is exactly one `{{ … }}` the typed value is returned
(number stays a number); otherwise the parts are interpolated into a string.

## 5. Node system

The frontend hardcodes **no node form**. It fetches descriptors from `GET /api/v1/node-types`
and renders the config panel generically. Adding a node is one Go file plus a registry
entry — no frontend change. This is the main lever on Phase 3 velocity.

```go
package nodes

type ParamType string
const (
    ParamString   ParamType = "string"
    ParamNumber   ParamType = "number"
    ParamBoolean  ParamType = "boolean"
    ParamSelect   ParamType = "select"
    ParamJSON     ParamType = "json"
    ParamCode     ParamType = "code"
    ParamKeyValue ParamType = "keyValue"
    ParamNotice   ParamType = "notice"
    ParamFilter   ParamType = "filter"   // IF conditions
)

type ParamOption struct { Label, Value string }
type ShowWhen    struct { Param string; Equals []any }

type ParamSpec struct {
    Name, Label        string
    Type               ParamType
    Default            any
    Required           bool
    Options            []ParamOption
    Placeholder        string
    Description        string
    SupportsExpression bool
    ShowWhen           *ShowWhen
}

type Handle   struct { Name, Label string }
type ExecMode string   // "perItem" | "once"

type Descriptor struct {
    Type, Name, Category, Description, Icon string
    Mode            ExecMode
    Inputs, Outputs []Handle
    Params          []ParamSpec
    Credential      string   // credential type required, "" if none
    IsTrigger       bool
}

type Result struct { Outputs map[string][]domain.Item }

type Node interface {
    Descriptor() Descriptor
    Execute(ec ExecContext) (Result, error)
}
```

`ExecContext` carries the resolved-param accessor, the current item, the full input set,
outputs of every finished node keyed by node name, an `*http.Client`, and a logger.
`nodes.Main(items...)` builds a single-`main`-output `Result`.

### Phase 1 node set

| Type | Mode | Notes |
|---|---|---|
| `trigger.manual` | once | Emits the run payload as one item, or `[{}]` |
| `trigger.webhook` | once | Params `path`, `method`. Emits `{headers, query, body}` |
| `trigger.schedule` | once | Params `cron`, `timezone`. Emits `{timestamp}` |
| `http.request` | perItem | `method`, `url`, `authentication`, `sendHeaders`/`headers`, `sendBody`/`bodyContentType`/`body`, `responseFormat` |
| `code` | once | JavaScript via **goja**, receives `items`, returns an array |
| `if` | once | `conditions` + `combinator`; two outputs `true` / `false` |
| `set` | perItem | `mode` (`merge`/`keepOnly`) + `fields`; expression-aware |
| `merge` | once | Two inputs; `mode` (`append`/`combineByPosition`) |

The `code` node runs in-process under goja with a wall-clock interrupt and no host
bindings. Accepted technical debt for Phases 1–2 (operator-run); Phase 4 SaaS must move it
to container or WASM isolation.

## 6. Execution engine

`internal/engine`. Depends on a narrow `Store` interface so it is unit-testable without a
database.

```
Run(ctx, executionID):
  execution, graph := store.LoadExecution
  validate graph: one reachable trigger, no cycles, unique node names
  existing := store.ListNodeExecutions           # resume support
  order   := topologicalOrder(graph)             # deterministic, tie-break on node id

  loop:
    node := first node in `order` that is unsettled and whose every predecessor is settled
    if none -> break
    if existing[node].status == succeeded -> reuse its output, mark settled, continue
    if store.IsCancelRequested -> mark execution cancelled, return

    input := concat, in graph edge order, of outputs[src][srcHandle] for incoming edges
             (grouped by targetHandle for multi-input nodes)
    if input is empty and node is not the trigger -> persist `skipped`, continue

    persist node_execution(running, input)
    result, err := runWithRetry(node, input)
    if err != nil:
        if node.settings.continueOnFail:
            result = one item per input item carrying {"error": …}; persist succeeded
        else:
            persist failed; execution.status = failed; execution.error = err; return
    persist node_execution(succeeded, output=result.Outputs)

  execution.status = succeeded
```

`runWithRetry` wraps each attempt in `context.WithTimeout(node.settings.timeoutMs)`,
recovers panics into node errors, and on failure sleeps `waitBetweenTriesMs` before the
next of `maxTries` attempts. A retry re-runs the **whole node** (all items), not the failing
item.

**Sequential by design in Phase 1** — independent branches run one after another. The loop
is written as a ready-set so Phase 2 swaps the single `pick` for a worker pool without
restructuring.

**Resume is what makes this worth it.** Because output is persisted after every node, a
worker crash mid-run resumes without re-issuing completed HTTP calls, and
`POST /executions/{id}/retry` restarts at the failed node with its stored input.

### Error model

| Layer | Behaviour |
|---|---|
| Node | `retryOnFail` (tries + backoff); `continueOnFail` turns the error into an item and the flow continues |
| Node | per-node timeout (default 60 s); panics recovered into a node error |
| Execution | overall timeout (default 5 min) → `failed`, with the offending node id recorded |
| Job | queue retry with backoff; safe because the engine skips already-succeeded nodes |
| Ingress | webhook answers `202` as soon as the execution row exists |

Errors are stored as `{code, message, status?, node_id?, details?}`.

## 7. HTTP API

Base `/api/v1`. JSON in, JSON out. Errors: `{"error":{"code","message","details"}}` with a
matching HTTP status. Auth by `fg_session` httpOnly cookie (browser) or
`Authorization: Bearer` (scripts); both carry the same JWT.

```
POST   /auth/login              {email,password} -> {user} + Set-Cookie
POST   /auth/logout             -> 204
GET    /auth/me                 -> {user}

GET    /node-types              -> {nodeTypes: Descriptor[]}

GET    /workflows               ?q=&status=&sort=  -> {workflows:[summary]}
POST   /workflows               {name} -> {workflow}
GET    /workflows/{id}          -> {workflow, graph, version}
PATCH  /workflows/{id}          {name?,active?} -> {workflow}
DELETE /workflows/{id}          -> 204
POST   /workflows/{id}/versions {graph} -> {version}
POST   /workflows/{id}/run      {data?} -> {execution}

GET    /executions              ?workflowId=&status=&limit=&cursor= -> {executions,nextCursor}
GET    /executions/{id}         -> {execution, nodeExecutions}
POST   /executions/{id}/cancel  -> {execution}
POST   /executions/{id}/retry   -> {execution}
GET    /executions/{id}/stream  -> SSE

POST   /nodes/{type}/test       {params, inputItems} -> {outputs, error}

ANY    /webhook/{path}          public, outside /api/v1
```

A workflow summary carries `id, name, active, nodeCount, triggerType, updatedAt,
lastRun{status,at}, successRate7d`.

Activating a workflow (`PATCH active=true`) reconciles its `webhooks` and `schedules` rows
from the active version's trigger nodes; deactivating removes them.

**SSE is poll-based.** The handler diffs `executions` + `node_executions` every 500 ms and
emits `execution`, `node` and `done` events, closing when the execution is terminal. It
therefore works across the API/worker process boundary with no extra infrastructure.

## 8. Frontend

`web/` — Next.js 15 App Router, TypeScript strict, Tailwind v4 (CSS-first config),
`@xyflow/react` for the canvas, TanStack Query for server state, Zustand for editor state.
UI primitives are hand-written in `web/components/ui` — no component-library CLI.

Design language is the approved **Studio dark** direction, expressed as CSS variables in
`web/app/globals.css`: surface `#0b0d12`, panel `#12151c`, raised `#161a23`,
input `#1a1e28`, borders `rgba(255,255,255,.06/.08)`, text `#e8eaf0` / `#b9bfcf` /
`#8a90a2` / `#6b7284` / `#565d72`, accent `#8b5cf6` (light `#a78bfa`, lighter `#c4b5fd`),
success `#34d399`, danger `#f87171`, warning `#fbbf24`; control radius 9–10 px, card 13–14 px;
Manrope for UI, JetBrains Mono for values, loaded by `<link>` in the root layout.

Screens, matching the approved mockups:

1. `/login` — email + password only.
2. `/workflows` — topbar tabs (Workflows · Executions · Credentials, the last disabled
   until Phase 3), filter row, table with status toggle, last run, 7-day success bar.
3. `/workflows/[id]` — the editor: node palette left, canvas centre, generated config
   drawer right, execution log panel bottom. Breadcrumb replaces the nav tabs here.
4. Node picker — a modal over the editor, opened with `Tab` or the canvas `+`, driven by
   the keyboard (`↑↓` navigate, `↵` add, `Esc` close), grouped by category.
5. `/executions` — filters plus a table with status pill, failure reason subline, trigger,
   duration, item count, execution id.
6. `/executions/[id]` — read-only graph replay coloured by node status over a two-pane
   data viewer (Input | Output/Error) with a node timeline rail and a
   "Retry from failed node" primary action.

The config drawer is generated from the descriptor `Params`, including `ShowWhen`
visibility and the expression chip with a resolved-value preview.

## 9. Repository layout

```
/cmd/api  /cmd/worker
/internal/
  domain/    items, graph, statuses, errors
  expr/      quote-aware preprocessor + evaluator
  nodes/     registry and one file per node
  engine/    graph runner, retry, resume
  queue/     Postgres job queue
  store/     pgx repositories (every call takes workspaceID)
  api/       chi router, handlers, middleware, SSE
  auth/      JWT + bcrypt
  crypto/    AES-256-GCM for credential blobs
  config/    environment
/db/migrations
/web/
/docs/superpowers/specs
docker-compose.yml   Makefile
```

## 10. Testing

Order reflects where bugs are most expensive.

- **Engine** first, against a fake store and a fake registry: IF branching, merge,
  skipped branches, retry exhaustion, `continueOnFail`, and **resume** (kill after node 2,
  re-run, assert nodes 1–2 do not execute again).
- **expr**: typed single-expression returns, interpolation, `$node[...]` lookup, and the
  quote-aware `$` preprocessor.
- **Nodes**: table-driven; `http.request` against `httptest`; `code` against goja
  including the timeout path.
- **Queue**: claim/retry/backoff semantics.
- **Store**: real Postgres, skipped unless `TEST_DATABASE_URL` is set.
- **API**: `httptest` over handlers with a fake store.
- **Web**: Vitest for graph utilities and the expression preview; one Playwright spec for
  create workflow → connect two nodes → Run → green log.

Tests are written before implementation.

## 11. Configuration

| Variable | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | — | Postgres DSN, required |
| `PORT` | `8080` | API port |
| `JWT_SECRET` | — | session signing key, required |
| `CREDENTIAL_KEY` | — | 32-byte base64 AES key, required |
| `PUBLIC_BASE_URL` | `http://localhost:3000` | used to render webhook URLs |
| `WORKER_CONCURRENCY` | `4` | jobs claimed in parallel |
| `EXECUTION_TIMEOUT` | `5m` | per-execution ceiling |
| `SEED_EMAIL` / `SEED_PASSWORD` | — | if set, creates the first workspace and user on boot |

`docker-compose.yml` provides Postgres 16 only; both Go binaries and the web app run on the
host in development.
