# Ducker Flow Grid — Phase 3 (Connectors)

Status: implemented 2026-08-19
Builds on [Phase 1 — Core](2026-08-18-workflow-platform-core-design.md) and
[Phase 2 — AI](2026-08-18-workflow-platform-ai-design.md).

## 1. Scope

Three things, in dependency order:

1. **A credential vault** — encrypted secrets a workspace owns, with a descriptor-driven form.
2. **OAuth2** — the authorisation-code grant, with refresh.
3. **A declarative connector registry** — real apps (Slack, Gmail, Google Sheets) added as
   data rather than as code.

Phase 1 left the room for all three: the `credentials` table, the AES-256-GCM sealer in
`internal/crypto`, and `credentialId` on every node have been in place since the first commit.

## 2. Where the key lives

`internal/credentials` is the only package that holds the encryption key.

- `internal/store` reads and writes a sealed blob it cannot open. A store test, a migration,
  or a stray log line therefore cannot leak a secret.
- `internal/api` serves summaries it never decrypts.
- A node never sees a secret at all: it hands the resolver an `*http.Request` and gets a
  signed one back.

That last point is the one worth defending. A node that could read a token could log it,
put it in an item, or send it to the wrong host — and nodes are the part of this system a
user configures freely. Handing over the request instead of the value removes the whole
class of mistake.

## 3. Credential types are descriptors

A `domain.CredentialType` declares its fields and its auth kind, exactly as a node descriptor
declares its parameters. The editor renders the form from it, the API validates against it,
and `Apply` signs from it. Adding an authentication scheme is a declaration in
`internal/credentials/registry.go` — no screen, no handler, no migration.

Shipped types: `httpBasicAuth`, `httpHeaderAuth` (key in a header *or* a query parameter),
`bearerAuth`, `anthropicApi`, `slackOAuth2`, `gmailOAuth2`, `googleSheetsOAuth2`.

Three details that matter:

- **A field marked `Secret` is never returned.** The summary lists which fields *are set*,
  which is what lets an edit form show "unchanged" rather than an empty box that looks like
  data loss.
- **An empty string for a secret in an update means "keep it".** The form cannot show the
  stored value, so it cannot round-trip it either.
- **A credential that will not decrypt still lists.** A rotated `CREDENTIAL_KEY` is a real
  operational event; the list renders it as visibly unusable rather than dropping it, and
  `Resolve` says so in words that name the likely cause.

## 4. OAuth2

`StartOAuth` builds the provider URL and records a one-time state; `CompleteOAuth` exchanges
the code and seals the tokens. Decisions:

- **State is in memory, and it is checked.** It is worthless after a few minutes and a lost
  one costs a single retry — but skipping the check is how an attacker attaches their own
  account to someone else's credential. It is single-use: a replayed callback is refused.
- **Unknown, expired and replayed states get the same message**, so a probe learns nothing.
- **The redirect URI is computed in one place** and used by both the authorisation and the
  token exchange. A mismatch between them is the single most common way this flow fails, and
  the UI shows the exact string to register with the provider.
- **A refresh that omits `refresh_token` keeps the old one.** Most providers do not re-issue
  it; dropping it would turn a working credential into one that needs re-authorising by hand.
- **Google types request `access_type=offline` and `prompt=consent`.** Without them Google
  returns no refresh token on a repeat authorisation and the credential dies an hour later.
- **A provider error arriving with HTTP 200 is still an error** — several do this.
- **Tokens are refreshed 60 seconds early.** A token that expires mid-flight fails a run that
  would otherwise have succeeded.

## 5. Connectors are declarations

```go
type Connector struct {
    ID, Name, Icon, Description string
    Credential string   // credential type id
    BaseURL    string
    Operations []Operation
}
type Operation struct {
    ID, Name, Description string
    Method, Path string            // {param} placeholders
    Params []nodes.ParamSpec
    Query  map[string]string
    Body   string                  // JSON template
    ItemsPath string               // plucks a sub-array into items
}
```

One generic executor runs any of them. `Connector.Descriptor()` builds the node descriptor:
an `operation` select, then every operation's parameters, each gated with
`ShowWhen{Param: "operation", Equals: [thatOperation]}`.

That gating is the load-bearing trick: because the Phase 1 editor already honours `ShowWhen`,
**a new connector needs no frontend change at all**. The form for an app nobody has written
UI for is correct on the first request.

Two hazards handled explicitly:

- **Body templating JSON-encodes its substitutions.** A quote or a newline in user data must
  not be able to break the document — that is an injection bug, not a formatting one.
- **Some APIs report failure in a 200 body.** Slack answers `{"ok": false, "error": …}`; a
  connector declares that so a failure is not recorded as a success.

## 6. How a node gets its credential

`nodes.ExecContext` gains a `Credential` of type `CredentialResolver`:

```go
type CredentialResolver interface {
    Apply(ctx context.Context, req *http.Request) error
    Type() string
    Name() string
}
```

The engine builds it from the node's `credentialId` and the *execution's* workspace, so a
workflow can only ever reach its own workspace's secrets. Resolution is lazy and cached per
node call: a node that never makes a request pays for no decrypt, three requests refresh one
token once, and a broken credential fails the node that uses it rather than one that does not.

## 7. Deleting a credential does not rewrite history

A delete leaves referencing nodes alone. Rewriting saved workflow versions to erase an id
would corrupt the history an execution replay depends on — Phase 1's whole versioning promise.
A run then fails with a message naming the missing credential, and the delete confirmation
states the usage count up front so the choice is informed.

## 8. What is verified, and what is not

The vault, the token lifecycle and the connector executor are covered against stubs: an
`httptest` server plays the authorize and token endpoints, and the full round trip —
start → callback → resolve → signed request → expiry → refresh — is asserted, along with
replayed state, provider errors returned as HTTP 200, and a refresh that omits the refresh
token.

**No live OAuth against Google or Slack has been performed.** That needs registered client
credentials and a publicly reachable redirect URI, neither of which this environment has.
The flow is built to the specification and tested against a faithful stub; the first real
authorisation should be treated as unverified, and the most likely failure is a redirect URI
that does not match the one registered with the provider — which is why the UI shows the exact
string to paste.

## 9. Configuration

| Variable | Default | Meaning |
|---|---|---|
| `CREDENTIAL_KEY` | — | base64 of 32 bytes. Required since Phase 1; now it actually protects something. Changing it makes every stored credential unreadable. |
| `PUBLIC_BASE_URL` | `http://localhost:3000` | The redirect URI is derived from this, so it must be the URL the provider can reach. |
