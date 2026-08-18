# Flowgrid — Phase 2 (AI)

Status: implemented 2026-08-18
Builds on [Phase 1](2026-08-18-workflow-platform-core-design.md), which stays the authority for
the engine, item semantics, expressions and error model.

## 1. Scope

Two node types and the one engine capability they need.

| Node | Type | What it does |
|---|---|---|
| LLM | `llm.chat` | Sends one prompt per item and returns the reply, optionally constrained to a JSON schema |
| AI Agent | `ai.agent` | Runs a model in a tool-calling loop, where the tools are other nodes in the graph |

Everything else in Phase 1 is unchanged. The Schedule trigger, which the original plan listed
under Phase 2, already shipped in Phase 1.

## 2. The provider seam

`internal/llm` is the boundary. Nodes never touch a vendor SDK.

```go
type Provider interface {
    Name() string
    Models() []string
    Chat(ctx context.Context, req Request) (Response, error)
}
```

`Request` carries the small common subset — model, system, messages, tools, max tokens,
effort, thinking, output format — rather than passing any one vendor's request type through.
`Response` normalises text, thinking, tool calls, stop reason and token usage.

`Registry` holds the configured providers; `llm.FromAPIKey` builds it from the environment.
A deployment with no key gets an **empty registry rather than a boot failure**: every non-AI
node still works, and the AI nodes report the missing key with a message naming the variable
to set. Making the whole platform refuse to start over a feature most workflows never touch
would be the wrong trade.

One provider ships: Anthropic, via the official `anthropic-sdk-go`.

### Deliberate choices inside the adapter

- **Adaptive thinking only.** The current models decide reasoning depth themselves; the older
  fixed token budget is gone from the API. `effort` is the dial the nodes expose instead.
- **The manual tool loop, not the SDK's tool runner.** The runner generates its schema from a
  Go struct, and this platform's tools are discovered from the graph at run time — their
  schemas are author-supplied JSON, not compile-time types. The loop is also where the
  transcript is recorded and the iteration ceiling enforced.
- **Tool input is parsed, never string-matched.** The models vary their JSON escaping, so the
  raw input is always `json.Unmarshal`ed.
- **`stop_details` is read only for a refusal**, which is the only case the API populates it.
- **Errors are translated by status code.** A 401 says to check `ANTHROPIC_API_KEY`; a 429
  says rate limit; a 400 quotes the API. These land in an execution log a workflow author
  reads, so "request failed" is not good enough.

## 3. Credentials — and the honest gap

The AI nodes declare `Credential: "anthropicApi"` and every node already carries a
`credentialId`, but **Phase 2 reads the key from `ANTHROPIC_API_KEY`**, not from the
credential vault. The vault is Phase 3.

This is a real limitation, stated rather than papered over: one key serves the whole
deployment, which is correct for single-tenant Phase 2 and wrong for the multi-tenant Phase 4.
The seam that closes it already exists — the table, the AES-256-GCM sealer in
`internal/crypto`, and the `credentialId` on the node — so Phase 3 fills it in rather than
migrating anything.

## 4. The engine capability: tool edges

The Agent node needs to invoke another node *during* its own execution. That is the one
genuinely new thing in this phase.

### Wiring

`ai.agent` declares a second input handle, `tool`. Nodes wired into it become its tools. The
relationship is therefore visible on the canvas rather than hidden in a config field.

```
  Trigger ──main──▶ AI Agent ──main──▶ …
                        ▲
                        │ tool
                  Fetch profile
```

### A tool edge is a capability edge, not a data edge

Nothing pushes items into a tool node, and it may run zero times or many. So the engine
treats a `tool`-handle edge differently from every other edge:

- **Readiness ignores it.** `dataPredecessors` filters tool edges out, so the agent does not
  wait for its tools to have run — which would be backwards.
- **Input gathering ignores it.** `gather` walks data predecessors only.
- **A tool-only node is never picked by the main loop.** A node with no incoming data edge
  whose only outgoing edges are tool edges is invoked exclusively by its consumer. Critically
  it is also **not marked `skipped`**: a grey card for a tool the agent called four times
  would be a lie.
- **A tool edge into a node with no tool handle is a validation error.** Silently ignoring it
  would leave an author watching an agent never use a tool they thought they had wired up.

### Invocation

The engine builds a `nodes.ToolBinding` per provider node, whose `Invoke` runs the target
through `executeNode` — the same path as any other node, so a tool gets the same retry,
timeout and panic handling. The model's arguments arrive as the tool's single input item, so a
tool's own expressions read them as `{{ $json.whatever }}`, exactly like a node in the main
flow.

**Known limitation, by design for this phase:** `node_executions` is unique on
`(execution_id, node_id)`, so a tool called four times leaves **one row showing its latest
call**. A per-call history would need a schema change. The agent's own transcript records
every call in full, and the row's job here is to colour the card and show the last arguments.

## 5. Tool descriptions and schemas

A tool needs a description for the model to choose it, and optionally an argument schema.
Both live on the **agent's** parameters rather than on each tool node:

- `toolDescriptions` — a keyValue table, node name to description.
- `toolSchemas` — optional JSON, node name to JSON Schema. A tool with no schema accepts any
  object.

One place to look, next to the prompt the descriptions have to agree with, and no new
parameter kind. **An undescribed tool is refused** rather than passed through: the model would
simply never pick it, and a silent no-op is the worst possible outcome.

## 6. The agent loop

```
messages := [user task]
for turn := 1; ; turn++ {
    if cancelled -> stop
    resp := provider.Chat(model, system, messages, tools, effort, thinking: on)
    accumulate usage; record the turn
    if refusal          -> fail with llm_refusal
    if not tool_use     -> done
    if turn >= ceiling  -> fail with agent_budget_exhausted
    messages += assistant turn (must precede its results)
    for each tool call  -> invoke, record, collect result
    messages += ONE user turn carrying every result
}
```

The details that matter:

- **Thinking is on and not configurable** on the Agent. With thinking off a model is
  measurably more likely to describe a tool call in prose instead of emitting one, which in a
  loop silently poisons later turns.
- **Every result goes back in one user turn.** Splitting them teaches the model to stop
  calling tools in parallel.
- **A failing tool is reported to the model, not fatal.** A wrong argument is something the
  model can correct next turn; killing the run would throw away everything it had worked out.
  An invented tool name comes back with the list of real ones.
- **Cancellation is checked between turns**, not mid-call, so an in-flight request finishes
  and is billed once.
- **The iteration ceiling is a hard failure**, with its own error code. A model still asking
  for tools after ten rounds is usually stuck, and an unbounded loop bills real money.
- **The whole transcript is in the output** — every turn, its thinking, its usage, and every
  tool call with arguments, result and item count. An agent that answers wrongly is only
  debuggable if you can see what it looked at, and the execution log shows one row for the
  node no matter how many turns it took.

## 7. Error codes added

| Code | Meaning |
|---|---|
| `llm_error` | The provider call failed, or the reply did not match the requested schema |
| `llm_refusal` | The model declined. Kept separate because retrying is pointless — the request needs changing |
| `agent_budget_exhausted` | The iteration ceiling was reached with the model still asking for tools |

## 8. Configuration

| Variable | Default | Meaning |
|---|---|---|
| `ANTHROPIC_API_KEY` | — | Enables the AI nodes. Absent means they report that, and nothing else changes |

## 9. Testing

The provider is faked everywhere except the adapter's own tests, which run against an
`httptest` stub — no test contacts a real API. Coverage concentrates on:

- the adapter's request shaping and its response and error translation;
- the agent loop: the happy path, parallel calls arriving in one user turn, a failing tool, an
  invented tool, the iteration ceiling, history ordering, refusal, cancellation;
- the engine's tool wiring: readiness excludes tool edges, a tool-only node is neither run nor
  skipped, an invocation persists a row, and a tool edge into a node that takes no tools is
  refused.
