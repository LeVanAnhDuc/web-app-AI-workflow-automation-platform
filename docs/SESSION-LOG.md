# Build session log — 18–19 August 2026

## What this file is

A structured record of the session that produced this repository: what was asked, what was
decided and why, what was built, every defect found along the way, and — most importantly —
**what is verified and what is not**.

It is not a verbatim transcript. It is the reasoning behind the code, which neither the source
nor the git history carries on its own. Read it before changing anything load-bearing; several
decisions here look arbitrary until you know what they are protecting against.

Everything below is written from the working session itself. Where something was never proven,
this file says so rather than implying otherwise.

---

## 1. The brief, and how it was narrowed

The request was "an AI Workflow Automation Platform, but design the UI for me to approve
before writing any code". The scope was narrowed by four questions:

| Question | Answer |
|---|---|
| What kind of product? | n8n/Zapier-style: a node canvas, connectors, an execution engine — with AI nodes |
| Who runs it? | Start at demo quality, grow into internal use, then SaaS. Architecture must not block that |
| How much of the node set? | Everything: real connectors *and* an AI agent |
| Stack? | Next.js + Go; the rest left to the implementer |

The "everything" answer is the reason for the phase plan below: the scope is roughly four
independent subsystems, and building them in one pass would have produced four half-finished
ones.

### The phase plan (agreed up front)

| Phase | Content | Status |
|---|---|---|
| 1 · Core | Canvas editor, execution engine, triggers, HTTP/Code/IF/Set/Merge, execution history | Done |
| 2 · AI | LLM node, AI Agent node with a tool-calling loop | Done |
| 3 · Connectors | Credential vault, OAuth2, Slack/Gmail/Sheets, declarative registry | Done |
| 4 · Multi-tenant | Workspace/team UI, RBAC, quota, billing hooks | **Not started** |

---

## 2. The UI, designed and approved before any code

Three aesthetic directions were drawn as the *same screen* (the editor, the most complex one)
so the comparison was fair — same structure, same sample workflow, only colour, type and
density differing:

- **A · Terminal noir** — 0px corners, technical grid, acid lime, dense mono
- **B · Studio dark** — 10–13px radii, soft shadows, violet accent, generous spacing
- **C · Blueprint** — light draughting-paper canvas, dark chrome, ink blue

**Option B was chosen.** Six high-fidelity screens were then drawn and approved before
implementation began: Login, Workflow list, Editor, Node picker, Execution list, Execution
detail. The sources are in `design/`, and they are deliberately left carrying the old product
name — they are the record of what was approved, not a live asset.

Four UI decisions made during that pass, all still in the shipped app:

- **No vertical nav rail.** List screens use topbar tabs; the editor replaces them with a
  breadcrumb so the canvas keeps the full width.
- **The node picker is keyboard-first** (Tab to open, ↑↓, ↵, Esc).
- **Execution detail uses two panes, not tabs** — debugging means comparing what went in with
  what came back, so both must be visible at once.
- **"Retry from failed node" is the primary action** on a failed run, because that is what the
  engine's per-node persistence buys.

---

## 3. Architecture decisions that are hard to reverse

These are the ones to understand before changing anything.

### The execution engine persists after every node

Three options were weighed: a durable step-runner, an in-memory DAG runner, and Temporal. The
step-runner was chosen because the in-memory version would have had to be **rewritten** for
Phase 2's agent (a 30–60 second node with many tool calls needs checkpoints), and Temporal
would have meant operating a cluster to model workflows that are themselves user data.

Everything else follows from it: crash resume, retry-from-failure, and a live log that is just
a table read.

### Workflows are versioned; an execution points at the version it ran

An old execution's replay therefore never drifts when the workflow is edited. This is why
deleting a credential does **not** rewrite saved versions — see §6.

### `workspace_id` from day one

Every root table has it and every repository function takes it, though Phase 1 seeded exactly
one workspace. Phase 4 opens a UI over this; it never migrates data.

### The queue is a Postgres table

`SELECT … FOR UPDATE SKIP LOCKED`, ~150 lines, no Redis and no broker. The spec originally
named the River library; it was dropped for a hand-written version to keep the dependency count
at zero for something this small.

### Node forms are generated from descriptors

The frontend hardcodes no node form. `GET /node-types` returns descriptors — parameter kinds,
labels, defaults, and `ShowWhen` visibility rules — and one generic component renders them.

**This is the single most valuable decision in the codebase.** It is why Phase 3's connectors
needed no frontend work at all: a connector declares an `operation` select plus each
operation's parameters gated on it, and the Phase 1 editor renders a correct form for an app
nobody has written UI for.

### Sequential execution, deliberately

Independent branches run one after another. The loop is written as a ready-set pick so
concurrency is a change of one line, not a restructure — but running in parallel was not worth
the debugging cost at this stage.

---

## 4. Phase 1 — Core

**Built:** the canvas editor, the engine, the store, the queue, the API (REST + SSE + public
webhook ingress), two binaries (`cmd/api`, `cmd/worker`), and all six approved screens.

### Decisions worth remembering

- **Items, not records.** Every node takes and returns a *list*; "for each of ten records, call
  an API" needs no loop node. Copied from n8n on purpose.
- **Expressions use a quote-aware preprocessor.** `expr-lang` cannot lex `$json`, so `$name` is
  rewritten to `_dollar_name` — but only outside string literals, or `"total: $5"` would break.
- **SSE polls the database every 500 ms** rather than using an in-process bus, because the
  engine runs in a *different process*. This is the only approach that needs no extra
  infrastructure.
- **A draft may be saved incomplete.** Only running and activating demand a valid graph.
  Refusing to save half-built work would make the editor hostile.
- **Activating a workflow reconciles its webhook and schedule rows** from the active version,
  so a half-finished edit can never leave a stale public URL live.

### Defects found by driving the real app (all fixed)

1. **An empty graph serialised as `{"nodes":null,"edges":null}`** — Go marshals a nil slice to
   `null`. The editor called `.map` on it and the whole screen failed to load. Fixed once in
   `domain.Graph`, for every caller.
2. **The workflow list returned 500** for any row whose graph had a null `nodes` value —
   `jsonb_array_length` raises on a non-array, and `COALESCE` cannot help because `->` yields a
   *jsonb* null rather than SQL NULL. The query checks `jsonb_typeof` instead.
3. **`{{ $json.body.company }}` failed the whole run** when the payload had no `body`. Workflow
   data is shaped by whoever sends it, so member access on a `$`-name now compiles to optional
   chaining and yields nothing. A genuine mistake — syntax, unknown function, arithmetic on
   nothing — is still an error.
4. **Item counts read zero for every branch of an IF node**, because they counted only the
   `main` output handle.
5. **A worker claimed job kinds it could not run** and discarded them — which is how a job kind
   added by a newer deployment would silently vanish against an older worker. `Claim` and
   `Work` now take a kind filter.

---

## 5. Phase 2 — AI

**Built:** `internal/llm` (the provider seam), the Anthropic adapter, the `llm.chat` node, the
`ai.agent` node, and the engine capability that makes an agent's tools *other nodes in the
graph*.

### The provider seam

Nodes never touch a vendor SDK. `llm.Request`/`llm.Response` carry the small common subset, so
a second provider is one file. A deployment with no API key gets an **empty registry rather
than a boot failure** — every other node still works, and the AI nodes name the variable to set.

### The tool edge — the one genuinely new engine concept

`ai.agent` has a second input handle, `tool`. Nodes wired into it become its tools, so the
relationship is **visible on the canvas** rather than hidden in a config field.

A tool edge is a *capability* edge, not a data edge, and the engine treats it differently
everywhere:

- readiness ignores it (the agent does not wait for its tools);
- input gathering ignores it;
- a node that exists only to be a tool is never run by the main loop **and is never marked
  skipped** — a grey card for a tool the agent called four times would be a lie;
- a tool edge into a node with no tool handle is a validation error, because silently ignoring
  it would leave an author watching an agent never use a tool they thought they had wired up.

Invocation goes through the same `executeNode` path as any other node, so a tool gets the same
retry, timeout and panic handling.

### Agent loop decisions

- **Thinking is on and not configurable.** With it off, a model is measurably more likely to
  *describe* a tool call in prose instead of emitting one — which in a loop poisons later turns.
- **Every tool result goes back in one user turn.** Splitting them teaches the model to stop
  calling tools in parallel.
- **A failing tool is reported to the model, not fatal.** A wrong argument is something it can
  correct next turn. An invented tool name comes back with the list of real ones.
- **The iteration ceiling is a hard failure** with its own error code — an unbounded loop bills
  real money.
- **The whole transcript is in the output**: every turn, its usage, and every tool call with
  arguments and result. An agent that answers wrongly is only debuggable if you can see what it
  looked at.

### Tool descriptions live on the agent, not on each tool node

A keyValue table (node name → description) plus optional JSON schemas, on the agent's own
parameters — one place to look, next to the prompt they have to agree with, and no new
parameter kind. **An undescribed tool is refused**, because the model would simply never pick
it and a silent no-op is the worst outcome.

### Defects found (all fixed)

1. **A tool invocation recorded its output by node id but not by name**, so a later node's
   `{{ $node["Fetch profile"].json }}` silently resolved to nothing.
2. **`toolInvoker` replaced the caller's context with the run's**, discarding the agent's own
   timeout and cancellation. The caller's context already descends from the run budget, so it
   is strictly the tighter of the two and is now the only one used.
3. **Tool rows were persisted with a context that could already be dead**, so a tool that timed
   out left no row explaining why. Bookkeeping now uses a separate context.
4. Three generic descriptor-form bugs: a `notice` whose prose lives in `Default` rendered
   blank; keyValue coercion could drift between the form and validation; `upstreamNames` walked
   tool edges, so an agent's input preview could come from a tool that had never run.

---

## 6. Phase 3 — Connectors

**Built:** the credential vault, OAuth2 with refresh, a declarative connector registry with
Slack/Gmail/Google Sheets, the credentials UI, and the engine wiring that gives a node its
credential.

### Where the key lives — the central decision

`internal/credentials` is the **only** package that holds the encryption key.

- The store reads and writes a sealed blob it cannot open, so a store test, a migration or a
  stray log line cannot leak a secret.
- The API serves summaries it never decrypts.
- **A node never sees a secret at all** — it hands over an `*http.Request` and gets a signed one
  back.

That last point is the one to defend. Nodes are the part of the system a user configures
freely; a node that could read a token could log it, put it in an item, or send it to the wrong
host. Handing over the request instead of the value removes the whole class of mistake.

### Credential types are descriptors

Same trick as node forms: the editor renders the form, the API validates, and `Apply` signs,
all from one declaration. Adding an authentication scheme is a declaration — no screen, no
handler, no migration.

- A field marked `Secret` is **never returned**. The summary lists which fields *are set*,
  which is what lets an edit form show "unchanged" instead of an empty box that looks like data
  loss.
- An empty string for a secret on update means "keep it".
- A credential that will not decrypt still lists, visibly unusable, and `Resolve` names the
  likely cause (a rotated `CREDENTIAL_KEY`).

### OAuth2 decisions

- **State is single-use and checked.** Skipping the check is how an attacker attaches their own
  account to someone else's credential.
- **Unknown, expired and replayed states share one message**, so a probe learns nothing.
- **The redirect URI is computed in one place** and used by both halves. A mismatch between
  them is the most common way this flow fails, which is why the UI shows the exact string with
  a copy button.
- **A refresh that omits `refresh_token` keeps the old one** — most providers do not re-issue
  it, and dropping it would turn a working credential into one needing manual re-authorisation.
- **Google types request `access_type=offline` and `prompt=consent`**, without which Google
  returns no refresh token on a repeat authorisation and the credential dies an hour later.
- **A provider error arriving with HTTP 200 is still an error** — several providers do this.
- **Tokens refresh 60 seconds early**, so one that would expire mid-flight does not fail a run.

### Connectors are declarations

Slack, Gmail and Google Sheets are data files; one generic executor runs any of them. Two
hazards handled explicitly:

- **Body templating JSON-encodes its substitutions** — a quote or newline in user data must not
  be able to break the document. That is an injection bug, not a formatting one.
- **Slack reports failure in a 200 body** (`{"ok": false}`), so a connector declares that and a
  failure is not recorded as a success.

### Deleting a credential does not rewrite history

Referencing nodes are left alone. Rewriting saved versions to erase an id would corrupt the
history an execution replay depends on. A run then fails with a message naming the missing
credential, and the delete confirmation states the usage count up front.

### Defects found (all fixed)

1. **A connector read "no credential chosen" from an empty `Type()`** — but resolution is
   deliberately lazy, so `Type()` is empty until the credential is first opened. **Every first
   request** told users to pick a credential they had already picked. `Apply` is what proves a
   credential; the type check now runs after it, on a request that is signed but not yet sent.
   *This was a bug at the seam between two independently written pieces — neither was wrong
   alone.*
2. **A missing credential now blocks a save — but only when the node needs one.** The first
   version of this check blocked graphs using the AI nodes, which legitimately fall back to a
   server-side key. The descriptor now says whether its credential is optional.

Two security improvements were added beyond the brief: `returnTo` is confined to this origin
(closing an open redirect), and the credential-test endpoint reports a status code and a
sentence rather than echoing the provider's body, which could reflect the signed request back.

---

## 7. Verified, and not verified

This section matters more than any other. **Do not assume anything here that is not listed as
verified.**

### Verified against the running application

- Building a workflow on the canvas, connecting it, saving, running it, and watching the log go
  green — across all four processes (Next → Go API → Postgres queue → worker → engine).
- Webhook ingress: a real `POST` producing a real execution, answered `202` immediately.
- IF branching: the taken branch runs, the other is recorded `skipped`.
- Per-item fan-out, `continueOnFail`, retry exhaustion, and **resume** (succeeded nodes are not
  re-executed on retry).
- The AI nodes end to end, including the agent's tool loop, history ordering on the wire, and
  the iteration ceiling stopping after exactly the configured number of provider calls.
- Credentials: **every response and the database row scanned for a known secret — no leak.**
  The database row is ciphertext. A patch that omits a secret keeps it.
- A connector node whose credential is not connected fails with a message naming the
  credential, the node, and the fix.
- Tool-edge validation refuses a tool wired into a node that takes none.

### Test suites

15 Go packages, 119 frontend unit tests, 2 Playwright end-to-end tests. All green, including
with a live worker competing for the same database.

### **Not** verified

- **No live call to the Anthropic API has ever been made.** This machine has no
  `ANTHROPIC_API_KEY` and no `ant` CLI. Every AI verification ran against a local stub
  reproducing the Messages API. That proves the adapter, the agent loop, the engine wiring and
  the UI — it proves **nothing** about real model behaviour.
  *To run for real: set `ANTHROPIC_API_KEY` in `.env`, do not set `ANTHROPIC_BASE_URL`, restart
  the worker.*
- **No live OAuth against Google or Slack has ever been run.** That needs registered client
  credentials and a publicly reachable redirect URI. The flow is tested against a stub playing
  both endpoints — including refresh, replayed state, and provider errors returned as HTTP 200
  — but the first real authorisation should be treated as unverified. The most likely failure
  is a redirect URI that does not match the one registered with the provider.
- **No connector has been called against a real Slack, Gmail or Sheets API.** The executor is
  tested against `httptest`; the endpoint paths and parameter names come from the published
  APIs but have not been exercised live.
- **No load, concurrency or security testing** beyond the unit and integration suites.

---

## 8. Known gaps and accepted debt

Listed so nobody has to rediscover them.

| Gap | Why it is acceptable now | What it costs later |
|---|---|---|
| The Code node runs JavaScript in-process under goja | Single-tenant, operator-run | Phase 4 SaaS must move it to container or WASM isolation |
| A tool called *n* times leaves **one** `node_executions` row (the latest call) | The agent transcript records every call | A per-call history needs a schema change |
| The executions list shows `—` for item count | The API returns none per execution | A column, or an API change |
| Date-range filtering on executions is a disabled control | The API has no date parameter | An API change; the control is honestly disabled rather than faking it |
| Gmail `sendMessage` takes `raw`, not to/subject/body | Gmail wants base64url RFC 2822; the description says so and gives a Code-node snippet | A per-operation hook would make connectors code again |
| Retry-attempt timestamps in the UI are derived, not recorded | Only the final attempt is persisted | Storing each attempt |
| Execution runs sequentially | Easier to debug; the loop is already a ready-set | One line to parallelise |
| The product name is not applied to infrastructure identifiers | See §10 | Cosmetic |

---

## 9. Running it

```bash
cp .env.example .env          # then change JWT_SECRET and CREDENTIAL_KEY
make db-up                    # Postgres on :6543
make migrate
make api                      # :8080
make worker                   # second shell
make web                      # :3000, proxies /api and /webhook to the Go API
```

**Test account** (from `SEED_EMAIL`/`SEED_PASSWORD`, created idempotently on boot):

```
ha.nguyen@acme.vn / flowgrid123
```

Tests:

```bash
make test-go      # needs TEST_DATABASE_URL for the store, queue and integration suites
make test-web
make test-e2e     # needs Postgres, cmd/api and cmd/worker running
```

`CREDENTIAL_KEY` is load-bearing since Phase 3: **changing it makes every stored credential
unreadable**, by design.

---

## 10. The rename

The product was renamed to **Ducker Flow Grid** at the end of the session. Every name a person
reads was changed. Infrastructure identifiers were deliberately left alone:

- the Postgres database, role and container (`flowgrid`) — changing them needs the database
  recreated;
- the session cookie (`fg_session`) and the JWT issuer — changing them logs everyone out;
- the Go module path — that is the repository name, not the product;
- `design/*.dc.html` — the record of the approved design.

---

## 11. How the work was run

Worth knowing, because it shaped the result.

- **Design was approved before any code.** Three directions, then six screens, then the spec.
- **Specs were written before implementation** and live in `docs/superpowers/specs/`. They are
  the authority; where the code diverged, the spec was updated to match reality rather than the
  divergence being left silent.
- **Parallel agents** built independent slices (store/queue, nodes, engine, screens,
  connectors, tests) against contracts fixed in advance. Two of the more interesting defects —
  the lazy-`Type()` credential bug and the tool-context bug — appeared **at the seams between
  agents**, which is exactly where the integration tests earned their cost.
- **The app was driven for real at every phase**, not just unit-tested. Every defect in §4–6
  was found that way, not by a test suite written against the same assumptions as the code.
- **Test failures were investigated, not silenced.** In two cases the test was wrong and was
  rewritten with the reason recorded; in the rest the code was wrong.

---

## 12. What is next

Phase 4: workspace and team UI, RBAC, quota, billing hooks. The room already exists —
`workspace_id` is on every root table and required by every store call, and a new node type is
still one Go file.

Before that, the two honest priorities are the ones in §7: **a real Anthropic key** and **a real
OAuth authorisation**, because those are the two paths that have never been exercised outside a
stub.
