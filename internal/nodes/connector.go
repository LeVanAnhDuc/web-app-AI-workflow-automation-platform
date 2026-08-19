package nodes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

/* ---------------------------------------------------------------------------
   The generic connector executor.

   A connector — Slack, Gmail, Sheets — is a declaration living in
   internal/connectors: a base URL, a credential type and a list of operations,
   each with its parameters and a template for the path, query and body. This
   file is the one piece of code that runs any of them, so adding an app is a
   data file rather than another Execute method.

   The dependency deliberately runs connectors -> nodes: a declaration is
   written in ParamSpec, so this package must not import that one back.
   ConnectorSpec is the seam. It is small on purpose — everything the executor
   needs about one call arrives flattened in a ConnectorCall.
   --------------------------------------------------------------------------- */

// ConnectorOperationParam is the name of the select that chooses the operation.
// Every other parameter of a connector node is gated on it with ShowWhen, which
// is what lets the existing config drawer render a form for an app it has never
// heard of.
const ConnectorOperationParam = "operation"

// ConnectorSpec is a connector declaration as the executor sees it.
type ConnectorSpec interface {
	// Descriptor is the node descriptor built from the operations.
	Descriptor() Descriptor

	// Call flattens one operation into the request to make. The bool is false
	// for an operation this connector does not have.
	Call(operation string) (ConnectorCall, bool)

	// OperationIDs lists the operation ids in declaration order, so an unknown
	// one can be reported alongside the real choices.
	OperationIDs() []string
}

// ConnectorCall is one operation flattened: everything needed to build, send
// and interpret a single request.
type ConnectorCall struct {
	Connector string // connector id, for error messages
	Operation string

	// Credential is the credential type id this connector needs, "" for none.
	Credential string

	BaseURL string
	Method  string

	// Path and the values of Query may contain {param} placeholders. Path
	// substitutions are URL-escaped; query substitutions are escaped by the
	// query encoder.
	Path  string
	Query map[string]string

	// Body is a JSON template whose {param} placeholders are replaced with the
	// JSON *encoding* of each value. See connectorBody.
	Body string

	// ItemsPath plucks an array out of the response, one item per element.
	ItemsPath string

	// Params are the operation's own parameters, the only ones this call reads.
	Params []ParamSpec

	// OKField and ErrorField cover an API that reports failure in the body of a
	// 200 response, Slack's {"ok": false} being the case that forced it.
	OKField    string
	ErrorField string
}

// ConnectorNode runs one connector. Every connector shares this implementation;
// they differ only in the declaration behind Spec.
type ConnectorNode struct {
	Spec ConnectorSpec

	// BaseURL overrides the connector's own base, which is how a test points a
	// real declaration at an httptest server. Empty means use the declaration.
	BaseURL string
}

// Descriptor implements Node.
func (n ConnectorNode) Descriptor() Descriptor { return n.Spec.Descriptor() }

// Execute implements Node. It runs perItem, so a list of records fans out into
// one call each with no loop node — the same bargain http.request makes.
func (n ConnectorNode) Execute(ec ExecContext) (Result, error) {
	operation, err := ec.Params.String(ConnectorOperationParam)
	if err != nil {
		return Result{}, err
	}
	operation = strings.TrimSpace(operation)
	call, ok := n.Spec.Call(operation)
	if !ok {
		return Result{}, domain.Errorf(domain.ErrCodeValidation,
			"unknown operation %q; this connector offers %s",
			operation, strings.Join(quoted(n.Spec.OperationIDs()), ", "))
	}

	values, err := resolveConnectorParams(ec.Params, call.Params)
	if err != nil {
		return Result{}, err
	}

	base := call.BaseURL
	if n.BaseURL != "" {
		base = n.BaseURL
	}
	req, err := buildConnectorRequest(ec, call, base, values)
	if err != nil {
		return Result{}, err
	}

	resp, err := ec.Client().Do(req)
	if err != nil {
		// A cancelled or timed-out context is the engine stopping us rather than
		// the remote host misbehaving, so it keeps its own error code.
		if cause := req.Context().Err(); cause != nil {
			return Result{}, &domain.NodeError{
				Code:    domain.ErrCodeCancelled,
				Message: fmt.Sprintf("%s %s was interrupted: %v", call.Connector, call.Operation, cause),
			}
		}
		return Result{}, &domain.NodeError{
			Code:    domain.ErrCodeHTTP,
			Message: fmt.Sprintf("%s %s failed: %v", req.Method, req.URL, err),
		}
	}
	// Read to completion and close before branching, so the connection returns
	// to the pool even on the error paths below.
	raw, readErr := io.ReadAll(resp.Body)
	if closeErr := resp.Body.Close(); closeErr != nil {
		ec.Log().Warn("connector: closing the response body failed",
			"connector", call.Connector, "operation", call.Operation, "error", closeErr)
	}
	if readErr != nil {
		return Result{}, &domain.NodeError{
			Code:    domain.ErrCodeHTTP,
			Status:  resp.StatusCode,
			Message: fmt.Sprintf("%s %s: reading the response body failed: %v", req.Method, req.URL, readErr),
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, &domain.NodeError{
			Code:    domain.ErrCodeHTTP,
			Status:  resp.StatusCode,
			Message: httpErrorMessage(req, resp.Status, raw),
		}
	}

	body, err := decodeConnectorResponse(raw)
	if err != nil {
		return Result{}, err
	}
	if err := connectorBodyError(call, req, resp.StatusCode, body, raw); err != nil {
		return Result{}, err
	}
	return connectorItems(call, body)
}

/* --- parameters ----------------------------------------------------------- */

// resolveConnectorParams reads the chosen operation's parameters. Expressions
// are already evaluated by the resolver; what is left is giving each value the
// Go type its declared ParamType implies, so that a number reaches a JSON body
// as a number rather than as a quoted string.
//
// A parameter the user left empty is absent from the map rather than present
// and blank: that distinction is what lets an optional query entry be dropped
// and an optional body member become null.
func resolveConnectorParams(p ParamResolver, specs []ParamSpec) (map[string]any, error) {
	out := make(map[string]any, len(specs))
	for _, spec := range specs {
		raw, err := p.Raw(spec.Name)
		if err != nil {
			return nil, err
		}
		if isEmptyValue(raw) {
			// The engine's resolver already refuses a blank required parameter,
			// but a node that only complains when run under the engine is a node
			// whose contract lives somewhere else.
			if spec.Required {
				return nil, ErrRequiredParam(spec.Name)
			}
			continue
		}

		switch spec.Type {
		case ParamNumber:
			f, ok := toFloat(raw)
			if !ok {
				return nil, domain.Errorf(domain.ErrCodeValidation,
					"parameter %q is not a number: %v", spec.Name, raw)
			}
			out[spec.Name] = f
		case ParamBoolean:
			out[spec.Name] = truthy(raw)
		case ParamJSON:
			// A json parameter arrives either as the text the user typed or,
			// when a whole-value expression produced a typed value, already
			// decoded. Both must end up as a value the body template can embed.
			if text, isString := raw.(string); isString {
				var decoded any
				if err := json.Unmarshal([]byte(text), &decoded); err != nil {
					return nil, domain.Errorf(domain.ErrCodeValidation,
						"parameter %q is not valid JSON: %v", spec.Name, err)
				}
				out[spec.Name] = decoded
				continue
			}
			out[spec.Name] = raw
		default:
			out[spec.Name] = stringify(raw)
		}
	}
	return out, nil
}

/* --- building the request ------------------------------------------------- */

// buildConnectorRequest turns the declaration and the resolved values into a
// request. It is split out so every failure before the call is a validation
// error and only the call itself can produce an http_error.
func buildConnectorRequest(ec ExecContext, call ConnectorCall, base string, values map[string]any) (*http.Request, error) {
	target, err := connectorURL(base, call, values)
	if err != nil {
		return nil, err
	}

	body, err := connectorBody(call, values)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(execCtx(ec), call.Method, target.String(), reader)
	if err != nil {
		return nil, domain.Errorf(domain.ErrCodeValidation, "could not build the request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	if err := applyConnectorCredential(ec, call, req); err != nil {
		return nil, err
	}
	return req, nil
}

// connectorURL joins the base and the operation path, substituting placeholders,
// then adds the query.
//
// The path is rendered twice, once decoded and once escaped, and both halves are
// put on the URL. That is deliberate: net/url only preserves an escaping it is
// given explicitly, and building the string and re-parsing it would let a value
// containing a slash invent a path segment — a range called "Q1/2026" would
// silently address a different resource.
func connectorURL(base string, call ConnectorCall, values map[string]any) (*url.URL, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, domain.Errorf(domain.ErrCodeValidation,
			"connector %q has an invalid base URL %q: %v", call.Connector, base, err)
	}
	basePath := strings.TrimSuffix(u.Path, "/")
	baseRaw := strings.TrimSuffix(u.EscapedPath(), "/")

	u.Path = basePath + ConnectorSubstitute(call.Path, func(name string) string {
		return connectorText(values[name])
	})
	u.RawPath = baseRaw + ConnectorSubstitute(call.Path, func(name string) string {
		return url.PathEscape(connectorText(values[name]))
	})
	if u.RawPath == u.Path {
		// An empty RawPath means "no special escaping", which is the shape
		// net/url expects when the two agree.
		u.RawPath = ""
	}

	query := u.Query()
	for key, tmpl := range call.Query {
		missing := false
		value := ConnectorSubstitute(tmpl, func(name string) string {
			v, present := values[name]
			if !present {
				missing = true
			}
			return connectorText(v)
		})
		// An entry whose parameter was left empty is dropped rather than sent
		// blank: "?q=" is a search for the empty string, not the absence of one.
		if missing || value == "" {
			continue
		}
		query.Set(key, value)
	}
	u.RawQuery = query.Encode()
	return u, nil
}

// connectorBody renders the JSON body template.
//
// Placeholders are replaced with the JSON *encoding* of the value, which is why
// a template writes {"text":{text}} and not {"text":"{text}"}. Substituting the
// raw text into a quoted slot is the bug this design exists to remove: one quote
// or newline in an item and the request becomes a different document, or invalid
// JSON, in a way that only shows up on the data that contains it.
//
// A parameter left empty renders as null and is then dropped, so an optional
// field such as Slack's thread_ts is omitted rather than sent as "".
func connectorBody(call ConnectorCall, values map[string]any) ([]byte, error) {
	if call.Body == "" || call.Method == http.MethodGet || call.Method == http.MethodHead {
		return nil, nil
	}

	var encodeErr error
	rendered := ConnectorSubstitute(call.Body, func(name string) string {
		value, present := values[name]
		if !present {
			return "null"
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			encodeErr = domain.Errorf(domain.ErrCodeValidation,
				"parameter %q cannot be encoded as JSON: %v", name, err)
			return "null"
		}
		return string(encoded)
	})
	if encodeErr != nil {
		return nil, encodeErr
	}

	// Decoding proves the template itself is sound before anything is sent. A
	// broken template is the connector author's mistake, and saying so here is
	// far clearer than the API's own complaint about a malformed payload.
	var decoded any
	if err := json.Unmarshal([]byte(rendered), &decoded); err != nil {
		return nil, domain.Errorf(domain.ErrCodeValidation,
			"the body template of %s operation %q produced invalid JSON: %v",
			call.Connector, call.Operation, err)
	}
	return json.Marshal(pruneNulls(decoded))
}

// pruneNulls drops null members from objects, so a placeholder that resolved to
// nothing leaves no trace. Array elements are kept, because a hole in an array
// is a position and positions carry meaning.
func pruneNulls(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for key, value := range t {
			if value == nil {
				continue
			}
			out[key] = pruneNulls(value)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, value := range t {
			out[i] = pruneNulls(value)
		}
		return out
	default:
		return v
	}
}

// applyConnectorCredential attaches the node's configured credential. A
// connector that declares a credential type and has none resolved fails here,
// naming what to configure: an unauthenticated call to Slack would otherwise
// come back as a bare "not_authed" that says nothing about which node to fix.
func applyConnectorCredential(ec ExecContext, call ConnectorCall, req *http.Request) error {
	if call.Credential == "" {
		return nil
	}
	cred := ec.Credential
	// A nil resolver is the only honest signal that nothing is configured.
	// Type() cannot be used for that: resolution is deliberately lazy, so it
	// reads empty until the credential has actually been opened — and treating
	// that as "none chosen" told users to pick a credential they had already
	// picked.
	if cred == nil {
		return domain.Errorf(domain.ErrCodeValidation,
			"this node needs a %q credential: choose one on the node, or add one under Credentials first",
			call.Credential)
	}

	if err := cred.Apply(execCtx(ec), req); err != nil {
		return domain.Errorf(domain.ErrCodeValidation,
			"the %q credential could not be used: %v", call.Credential, err)
	}

	// Checked after Apply, because that is when the type is known. The request
	// has been signed but not sent, so a mismatch still costs nothing.
	if t := cred.Type(); t != "" && t != call.Credential {
		return domain.Errorf(domain.ErrCodeValidation,
			"this node needs a %q credential but %q is a %q one",
			call.Credential, cred.Name(), t)
	}
	return nil
}

/* --- reading the response ------------------------------------------------- */

// decodeConnectorResponse parses the body. Unlike http.request there is no
// format choice here: a connector is a declaration about a specific JSON API, so
// a reply that is not JSON is a real failure rather than something to fall back
// from.
func decodeConnectorResponse(raw []byte) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, domain.Errorf(domain.ErrCodeHTTP,
			"the response was not valid JSON: %v; body starts with %q", err, truncate(string(raw), errorBodyLimit))
	}
	return decoded, nil
}

// connectorBodyError honours OKField: an API that answers 200 while saying in
// the body that it failed. Only a field that is present and falsy fails the
// node, so an endpoint of the same app that does not carry the flag still works.
func connectorBodyError(call ConnectorCall, req *http.Request, status int, body any, raw []byte) error {
	if call.OKField == "" {
		return nil
	}
	flag, present := lookupPath(body, call.OKField)
	if !present || truthy(flag) {
		return nil
	}

	reason := ""
	if call.ErrorField != "" {
		if value, ok := lookupPath(body, call.ErrorField); ok {
			reason = stringify(value)
		}
	}
	if reason == "" {
		reason = strings.TrimSpace(truncate(string(raw), errorBodyLimit))
	}
	return &domain.NodeError{
		Code:   domain.ErrCodeHTTP,
		Status: status,
		Message: fmt.Sprintf("%s %s answered %d but reported failure: %s",
			req.Method, req.URL, status, reason),
	}
}

// connectorItems shapes the response into items: ItemsPath plucks an array and
// each element becomes one item, and an empty path makes the whole response one.
func connectorItems(call ConnectorCall, body any) (Result, error) {
	if call.ItemsPath == "" {
		return Main(connectorItem(body)), nil
	}
	picked, present := lookupPath(body, call.ItemsPath)
	if !present || picked == nil {
		// A list endpoint that matched nothing usually omits the array
		// altogether. That is an empty result, not a failure, and the workflow
		// downstream simply runs zero times.
		return MainSlice(nil), nil
	}
	elements, ok := picked.([]any)
	if !ok {
		return Result{}, domain.Errorf(domain.ErrCodeHTTP,
			"expected %q in the %s response to be an array, got %T",
			call.ItemsPath, call.Connector, picked)
	}
	out := make([]domain.Item, 0, len(elements))
	for _, element := range elements {
		out = append(out, connectorItem(element))
	}
	return MainSlice(out), nil
}

// connectorItem wraps one response value as an item. An item is always an
// object, so anything else — a Sheets row, which is an array — is carried under
// "value" rather than dropped.
func connectorItem(v any) domain.Item {
	if object, ok := v.(map[string]any); ok {
		return domain.NewItem(object)
	}
	return domain.NewItem(map[string]any{"value": v})
}

// lookupPath walks a dotted path through decoded JSON objects.
func lookupPath(v any, path string) (any, bool) {
	current := v
	for _, segment := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

/* --- the placeholder syntax ----------------------------------------------- */

// connectorPlaceholder matches a {param} reference. The identifier shape is what
// keeps a JSON body template unambiguous: the braces of an object, and {}, never
// look like a placeholder.
var connectorPlaceholder = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ConnectorPlaceholders lists, in order and with repeats, the parameter names a
// template refers to. A connector declaration checks its own templates against
// its parameters with this, so a typo is caught at start-up rather than sent to
// the API as a literal "{spreadsheeId}".
func ConnectorPlaceholders(tmpl string) []string {
	matches := connectorPlaceholder.FindAllStringSubmatch(tmpl, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// ConnectorSubstitute replaces every {param} with what value returns for it. The
// caller supplies the escaping, because the right escaping differs by position:
// URL-escaped in a path, JSON-encoded in a body.
func ConnectorSubstitute(tmpl string, value func(name string) string) string {
	return connectorPlaceholder.ReplaceAllStringFunc(tmpl, func(match string) string {
		return value(match[1 : len(match)-1])
	})
}

// connectorText renders a value for a URL, where everything is text and a
// missing value is the empty string.
func connectorText(v any) string {
	if v == nil {
		return ""
	}
	return stringify(v)
}

// quoted renders a list of ids for an error message.
func quoted(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, fmt.Sprintf("%q", v))
	}
	return out
}
