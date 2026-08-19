package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

/* --- the built-in declarations -------------------------------------------- */

func TestDefaultRegistry(t *testing.T) {
	// NewRegistry validates, so this alone proves every shipped declaration is
	// well formed: no placeholder naming a parameter that does not exist, no
	// body on a GET, no parameter shared with a conflicting spec.
	reg := Default()

	want := []string{"slack", "gmail", "googleSheets"}
	var got []string
	for _, c := range reg.All() {
		got = append(got, c.ID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("connector ids = %v, want %v", got, want)
	}

	if _, ok := reg.Get("slack"); !ok {
		t.Errorf("Get(slack) found nothing")
	}
	if _, ok := reg.Get("nope"); ok {
		t.Errorf("Get(nope) found something")
	}
	if n := len(reg.Nodes()); n != len(want) {
		t.Errorf("Nodes() returned %d, want %d", n, len(want))
	}
}

func TestBuiltInOperationIDs(t *testing.T) {
	want := map[string][]string{
		"slack":        {"postMessage", "listChannels", "channelHistory"},
		"gmail":        {"listMessages", "getMessage", "sendMessage"},
		"googleSheets": {"readRange", "appendRow"},
	}
	for id, ops := range want {
		c, ok := Default().Get(id)
		if !ok {
			t.Fatalf("connector %q is missing", id)
		}
		if got := c.OperationIDs(); !reflect.DeepEqual(got, ops) {
			t.Errorf("%s operations = %v, want %v", id, got, ops)
		}
		for _, op := range ops {
			if _, found := c.Operation(op); !found {
				t.Errorf("%s.Operation(%q) found nothing", id, op)
			}
		}
	}
}

func TestBuiltInCredentialTypes(t *testing.T) {
	// The connector and its credential type must agree, or the vault hands the
	// node a credential for a different app.
	want := map[string]string{
		"slack":        "slackOAuth2",
		"gmail":        "gmailOAuth2",
		"googleSheets": "googleSheetsOAuth2",
	}
	for id, credType := range want {
		c, _ := Default().Get(id)
		if c.Credential != credType {
			t.Errorf("%s credential = %q, want %q", id, c.Credential, credType)
		}
		if got := c.Descriptor().Credential; got != credType {
			t.Errorf("%s descriptor credential = %q, want %q", id, got, credType)
		}
	}
}

/* --- the descriptor ------------------------------------------------------- */

func TestDescriptorListsTheOperations(t *testing.T) {
	c, _ := Default().Get("slack")
	d := c.Descriptor()

	if d.Type != "slack" || d.Mode != nodes.ModePerItem {
		t.Errorf("descriptor type/mode = %q/%q", d.Type, d.Mode)
	}
	if len(d.Params) == 0 {
		t.Fatal("the descriptor has no parameters")
	}

	first := d.Params[0]
	if first.Name != nodes.ConnectorOperationParam || first.Type != nodes.ParamSelect {
		t.Fatalf("the first parameter is %q (%s), want the operation select", first.Name, first.Type)
	}
	if !first.Required || first.Default != "postMessage" {
		t.Errorf("the operation select should be required and default to the first operation, got %v/%v",
			first.Required, first.Default)
	}

	var options []string
	for _, o := range first.Options {
		options = append(options, o.Value)
		if o.Label == "" {
			t.Errorf("option %q has no label", o.Value)
		}
	}
	if !reflect.DeepEqual(options, c.OperationIDs()) {
		t.Errorf("select options = %v, want the operation ids %v", options, c.OperationIDs())
	}
}

func TestDescriptorGatesEveryParameter(t *testing.T) {
	// This is the load-bearing claim of the whole design: because every
	// operation's parameters are ShowWhen-gated on the operation select, the
	// existing config drawer renders a correct form for an app it has never
	// heard of. If a parameter escapes ungated it shows up under every
	// operation, and the form is quietly wrong.
	for _, c := range Default().All() {
		ids := make(map[string]bool, len(c.Operations))
		for _, op := range c.Operations {
			ids[op.ID] = true
		}

		for _, spec := range c.Descriptor().Params[1:] {
			if spec.ShowWhen == nil {
				t.Errorf("%s: parameter %q is not gated", c.ID, spec.Name)
				continue
			}
			if spec.ShowWhen.Param != nodes.ConnectorOperationParam {
				t.Errorf("%s: parameter %q is gated on %q", c.ID, spec.Name, spec.ShowWhen.Param)
			}
			if len(spec.ShowWhen.Equals) == 0 {
				t.Errorf("%s: parameter %q is gated on nothing", c.ID, spec.Name)
			}
			for _, equals := range spec.ShowWhen.Equals {
				// The drawer compares with strict equality against the stored
				// value, so the gate has to hold the operation id as a string.
				id, isString := equals.(string)
				if !isString || !ids[id] {
					t.Errorf("%s: parameter %q is gated on %v, which is not an operation id", c.ID, spec.Name, equals)
				}
			}
		}
	}
}

func TestDescriptorMergesASharedParameter(t *testing.T) {
	// "channel" belongs to both posting and reading. It must appear once, gated
	// on both, rather than twice under the same name.
	c, _ := Default().Get("slack")

	var found []nodes.ParamSpec
	for _, spec := range c.Descriptor().Params {
		if spec.Name == "channel" {
			found = append(found, spec)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the shared parameter appears %d times, want once", len(found))
	}
	if got := found[0].ShowWhen.Equals; !reflect.DeepEqual(got, []any{"postMessage", "channelHistory"}) {
		t.Errorf("channel is gated on %v, want both operations that use it", got)
	}
}

func TestDescriptorRefusesAConflictingSharedParameter(t *testing.T) {
	// Two operations may share a parameter name only when the whole spec
	// matches. Sharing the name with a different type would render one field
	// whose editor changed with the selected operation — a build-time mistake,
	// so it fails loudly rather than shipping a subtly wrong form.
	conflicting := Connector{
		ID: "x", Name: "X", BaseURL: "https://example.com",
		Operations: []Operation{
			{
				ID: "a", Name: "A", Method: "GET", Path: "/a",
				Params: []nodes.ParamSpec{{Name: "channel", Label: "Channel", Type: nodes.ParamString}},
			},
			{
				ID: "b", Name: "B", Method: "GET", Path: "/b",
				Params: []nodes.ParamSpec{{Name: "channel", Label: "Channel", Type: nodes.ParamNumber}},
			},
		},
	}

	if err := conflicting.Validate(); err == nil {
		t.Fatal("Validate accepted two conflicting specs for one parameter")
	} else {
		for _, part := range []string{"channel", `"a"`, `"b"`, "Type"} {
			if !strings.Contains(err.Error(), part) {
				t.Errorf("the error %q does not mention %q", err, part)
			}
		}
	}

	assertPanics(t, "Descriptor", func() { conflicting.Descriptor() })
	assertPanics(t, "NewRegistry", func() { NewRegistry(conflicting) })
}

func TestDescriptorAcceptsAnIdenticalSharedParameter(t *testing.T) {
	shared := nodes.ParamSpec{Name: "channel", Label: "Channel", Type: nodes.ParamString, Required: true}
	fine := Connector{
		ID: "x", Name: "X", BaseURL: "https://example.com",
		Operations: []Operation{
			{ID: "a", Name: "A", Method: "GET", Path: "/a", Params: []nodes.ParamSpec{shared}},
			{ID: "b", Name: "B", Method: "GET", Path: "/b", Params: []nodes.ParamSpec{shared}},
		},
	}

	if err := fine.Validate(); err != nil {
		t.Fatalf("Validate rejected two identical specs: %v", err)
	}
	if got := len(fine.Descriptor().Params); got != 2 {
		t.Errorf("the descriptor has %d parameters, want the select plus one merged field", got)
	}
}

/* --- validation ----------------------------------------------------------- */

func TestValidateRejectsMalformedDeclarations(t *testing.T) {
	base := func(ops ...Operation) Connector {
		return Connector{ID: "x", Name: "X", BaseURL: "https://example.com", Operations: ops}
	}
	get := func(path string, params ...nodes.ParamSpec) Operation {
		return Operation{ID: "a", Name: "A", Method: "GET", Path: path, Params: params}
	}

	tests := []struct {
		name string
		conn Connector
		want string
	}{
		{"no id", Connector{Name: "X", BaseURL: "https://e.com", Operations: []Operation{get("/a")}}, "needs an ID"},
		{"no base url", Connector{ID: "x", Name: "X", Operations: []Operation{get("/a")}}, "needs a BaseURL"},
		{"trailing slash", Connector{ID: "x", Name: "X", BaseURL: "https://e.com/", Operations: []Operation{get("/a")}}, "must not end in a slash"},
		{"no operations", Connector{ID: "x", Name: "X", BaseURL: "https://e.com"}, "declares no operations"},
		{"relative path", base(get("a")), "must start with a slash"},
		{
			name: "a placeholder with no parameter behind it",
			conn: base(get("/a/{missing}")),
			want: "{missing}",
		},
		{
			name: "a query placeholder with no parameter behind it",
			conn: base(Operation{ID: "a", Name: "A", Method: "GET", Path: "/a", Query: map[string]string{"q": "{nope}"}}),
			want: "{nope}",
		},
		{
			name: "a body on a GET",
			conn: base(Operation{ID: "a", Name: "A", Method: "GET", Path: "/a", Body: `{}`}),
			want: "carries none",
		},
		{
			name: "a duplicate operation",
			conn: base(get("/a"), get("/a")),
			want: "twice",
		},
		{
			name: "a parameter called operation",
			conn: base(get("/a", nodes.ParamSpec{Name: "operation", Type: nodes.ParamString})),
			want: "operation select itself",
		},
		{
			name: "an error field with no ok field",
			conn: Connector{ID: "x", Name: "X", BaseURL: "https://e.com", ErrorField: "error", Operations: []Operation{get("/a")}},
			want: "without OKField",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.conn.Validate()
			if err == nil {
				t.Fatalf("Validate accepted it")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

/* --- the real declarations, end to end ------------------------------------ */

func TestSlackPostMessage(t *testing.T) {
	var got struct {
		path string
		body map[string]any
		auth string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")
		_ = json.Unmarshal(raw, &got.body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"channel":"C1","ts":"1710000000.1"}`)
	}))
	defer srv.Close()

	slack, _ := Default().Get("slack")
	res, err := execute(slack, srv.URL, map[string]any{
		"operation": "postMessage",
		"channel":   "C1",
		"text":      "shipped \"v2\"\nall good",
	}, stubCredential{credType: "slackOAuth2"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got.path != "/chat.postMessage" {
		t.Errorf("path = %q, want the operation path appended to the base", got.path)
	}
	if got.auth != "Bearer stub" {
		t.Errorf("Authorization = %q", got.auth)
	}
	if got.body["text"] != "shipped \"v2\"\nall good" {
		t.Errorf("text round-tripped as %#v", got.body["text"])
	}
	if _, present := got.body["thread_ts"]; present {
		t.Errorf("the unset optional thread_ts was sent: %#v", got.body)
	}
	if n := len(res.Get(nodes.HandleMain)); n != 1 {
		t.Errorf("got %d items, want the whole response as one", n)
	}
}

func TestSlackReportsOKFalse(t *testing.T) {
	// Slack answers 200 for a rejected call. Without OKField this would look
	// like a success and the workflow would carry on with a meaningless item.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":false,"error":"channel_not_found"}`)
	}))
	defer srv.Close()

	slack, _ := Default().Get("slack")
	_, err := execute(slack, srv.URL, map[string]any{
		"operation": "postMessage", "channel": "C1", "text": "hi",
	}, stubCredential{credType: "slackOAuth2"})

	if err == nil {
		t.Fatal("a Slack failure was reported as a success")
	}
	ne := domain.AsNodeError(err)
	if ne.Code != domain.ErrCodeHTTP || !strings.Contains(ne.Message, "channel_not_found") {
		t.Errorf("error = %s: %s", ne.Code, ne.Message)
	}
}

func TestSheetsReadRangeEscapesTheRange(t *testing.T) {
	var escaped string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escaped = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"range":"x","values":[["a","b"],["c","d"]]}`)
	}))
	defer srv.Close()

	sheets, _ := Default().Get("googleSheets")
	res, err := execute(sheets, srv.URL, map[string]any{
		"operation":     "readRange",
		"spreadsheetId": "1BxiMVs",
		// A sheet name with a space and a slash is legal A1 notation, and is
		// exactly what would address a different resource unescaped.
		"range": "'Q1/2026 Data'!A:D",
	}, stubCredential{credType: "googleSheetsOAuth2"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if strings.Count(escaped, "/") != 4 {
		t.Errorf("the range invented path segments: %q", escaped)
	}
	if !strings.Contains(escaped, "%2F") || !strings.Contains(escaped, "%20") {
		t.Errorf("the range was not escaped: %q", escaped)
	}

	items := res.Get(nodes.HandleMain)
	if len(items) != 2 {
		t.Fatalf("got %d items, want one per row", len(items))
	}
	// A Sheets row is an array, and an item is always an object, so the row
	// arrives under "value".
	if !reflect.DeepEqual(items[0].JSON["value"], []any{"a", "b"}) {
		t.Errorf("row 0 = %#v", items[0].JSON)
	}
}

func TestGmailListMessagesOmitsAnEmptySearch(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"messages":[{"id":"1"},{"id":"2"}]}`)
	}))
	defer srv.Close()

	gmail, _ := Default().Get("gmail")
	res, err := execute(gmail, srv.URL, map[string]any{
		"operation": "listMessages", "maxResults": 10,
	}, stubCredential{credType: "gmailOAuth2"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if query != "maxResults=10" {
		t.Errorf("query = %q, want the empty search left off entirely", query)
	}
	if n := len(res.Get(nodes.HandleMain)); n != 2 {
		t.Errorf("got %d items, want one per message", n)
	}
}

/* --- helpers -------------------------------------------------------------- */

// execute runs a connector against a test server standing in for its API.
func execute(c Connector, baseURL string, values map[string]any, cred nodes.CredentialResolver) (nodes.Result, error) {
	node := nodes.ConnectorNode{Spec: c, BaseURL: baseURL}
	return node.Execute(nodes.ExecContext{
		Ctx:        context.Background(),
		Params:     &stubParams{specs: c.Descriptor().Params, values: values},
		Item:       domain.NewItem(map[string]any{}),
		Credential: cred,
	})
}

// stubCredential stands in for the vault: it proves the node signs its request
// without any secret existing.
type stubCredential struct{ credType string }

func (s stubCredential) Apply(_ context.Context, req *http.Request) error {
	req.Header.Set("Authorization", "Bearer stub")
	return nil
}
func (s stubCredential) Type() string { return s.credType }
func (s stubCredential) Name() string { return "stub" }

// stubParams is a nodes.ParamResolver over a plain map. Expression evaluation
// belongs to internal/expr and the engine's resolver, both tested elsewhere;
// what a connector needs is the values those would already have produced, plus
// the declared defaults, which is what makes a test of an omitted optional
// meaningful.
type stubParams struct {
	specs  []nodes.ParamSpec
	values map[string]any
}

func (p *stubParams) raw(name string) (any, bool) {
	if v, ok := p.values[name]; ok {
		return v, true
	}
	for _, spec := range p.specs {
		if spec.Name == name && spec.Default != nil {
			return spec.Default, true
		}
	}
	return nil, false
}

func (p *stubParams) String(name string) (string, error) {
	v, ok := p.raw(name)
	if !ok || v == nil {
		return "", nil
	}
	if s, isString := v.(string); isString {
		return s, nil
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func (p *stubParams) Int(name string) (int, error) {
	f, err := p.Float(name)
	return int(f), err
}

func (p *stubParams) Float(name string) (float64, error) {
	v, ok := p.raw(name)
	if !ok {
		return 0, nil
	}
	switch t := v.(type) {
	case int:
		return float64(t), nil
	case float64:
		return t, nil
	case string:
		return strconv.ParseFloat(t, 64)
	}
	return 0, fmt.Errorf("parameter %q is not a number", name)
}

func (p *stubParams) Bool(name string) (bool, error) {
	v, _ := p.raw(name)
	b, _ := v.(bool)
	return b, nil
}

func (p *stubParams) Raw(name string) (any, error) {
	v, _ := p.raw(name)
	return v, nil
}

func (p *stubParams) KeyValues(string) ([]nodes.KeyValue, error)   { return nil, nil }
func (p *stubParams) Conditions(string) ([]nodes.Condition, error) { return nil, nil }

func (p *stubParams) StringOr(name, def string) string {
	if s, err := p.String(name); err == nil && s != "" {
		return s
	}
	return def
}

func (p *stubParams) IntOr(name string, def int) int {
	if _, ok := p.raw(name); !ok {
		return def
	}
	n, err := p.Int(name)
	if err != nil {
		return def
	}
	return n
}

func (p *stubParams) BoolOr(name string, def bool) bool {
	if _, ok := p.raw(name); !ok {
		return def
	}
	b, _ := p.Bool(name)
	return b
}

func (p *stubParams) Literal(name string) any {
	v, _ := p.raw(name)
	return v
}

// assertPanics fails unless fn panics, which is how a build-time mistake in a
// declaration is supposed to behave.
func assertPanics(t *testing.T, what string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s did not panic on a malformed declaration", what)
		}
	}()
	fn()
}
