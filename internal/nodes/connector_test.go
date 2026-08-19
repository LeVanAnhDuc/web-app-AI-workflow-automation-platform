package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

/* ---------------------------------------------------------------------------
   These tests exercise the generic executor against a fake declaration rather
   than the real Slack/Gmail/Sheets ones. That is not a shortcut: a connector
   declaration is written in ParamSpec, so internal/connectors imports this
   package and a test here cannot import it back. The real declarations are
   driven end to end from internal/connectors, where both halves are reachable.
   --------------------------------------------------------------------------- */

// fakeConnector is a ConnectorSpec over a fixed set of calls.
type fakeConnector struct {
	descriptor Descriptor
	calls      map[string]ConnectorCall
	order      []string
}

func (f fakeConnector) Descriptor() Descriptor { return f.descriptor }

func (f fakeConnector) Call(operation string) (ConnectorCall, bool) {
	c, ok := f.calls[operation]
	return c, ok
}

func (f fakeConnector) OperationIDs() []string { return f.order }

// connectorSpec wraps one call as a single-operation connector.
func connectorSpec(call ConnectorCall) fakeConnector {
	if call.Operation == "" {
		call.Operation = "run"
	}
	if call.Connector == "" {
		call.Connector = "fake"
	}
	if call.Method == "" {
		call.Method = http.MethodGet
	}
	return fakeConnector{
		descriptor: Descriptor{Type: call.Connector, Name: call.Connector, Mode: ModePerItem},
		calls:      map[string]ConnectorCall{call.Operation: call},
		order:      []string{call.Operation},
	}
}

// fakeCredential is a CredentialResolver that stamps a header, so a test can
// prove the credential reached the wire without any secret existing.
type fakeCredential struct {
	credType string
	name     string
	header   string
	err      error
}

func (f fakeCredential) Apply(_ context.Context, req *http.Request) error {
	if f.err != nil {
		return f.err
	}
	req.Header.Set("Authorization", f.header)
	return nil
}

func (f fakeCredential) Type() string { return f.credType }
func (f fakeCredential) Name() string { return f.name }

// recorder is an httptest server that remembers the request it was given.
type recorder struct {
	*httptest.Server

	status      int
	contentType string
	reply       string

	method      string
	escapedPath string
	rawQuery    string
	body        string
	authHeader  string
	calls       int
}

func newRecorder(t *testing.T, reply string) *recorder {
	t.Helper()
	rec := &recorder{status: http.StatusOK, contentType: "application/json", reply: reply}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.calls++
		rec.method = r.Method
		// EscapedPath, not Path: the point of escaping a placeholder is that a
		// value containing a slash stays inside one segment, which only the
		// escaped form can show.
		rec.escapedPath = r.URL.EscapedPath()
		rec.rawQuery = r.URL.RawQuery
		rec.body = string(body)
		rec.authHeader = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", rec.contentType)
		w.WriteHeader(rec.status)
		_, _ = io.WriteString(w, rec.reply)
	}))
	t.Cleanup(rec.Close)
	return rec
}

// runConnector executes a node against the recorder.
func runConnector(node ConnectorNode, values map[string]any, cred CredentialResolver) (Result, error) {
	return node.Execute(ExecContext{
		Ctx:        context.Background(),
		Params:     params(values),
		Item:       domain.NewItem(map[string]any{}),
		Items:      items(map[string]any{}),
		Credential: cred,
	})
}

/* --- request shaping ------------------------------------------------------ */

func TestConnectorNodeShapesTheRequest(t *testing.T) {
	tests := []struct {
		name     string
		call     ConnectorCall
		values   map[string]any
		wantPath string
		wantQry  string
		wantBody map[string]any
	}{
		{
			name: "path placeholders are url-escaped",
			call: ConnectorCall{
				Operation: "readRange",
				Path:      "/spreadsheets/{spreadsheetId}/values/{range}",
				Params: []ParamSpec{
					{Name: "spreadsheetId", Type: ParamString, Required: true},
					{Name: "range", Type: ParamString, Required: true},
				},
			},
			values: map[string]any{
				"operation":     "readRange",
				"spreadsheetId": "1BxiMVs",
				// A slash and a space are the two characters that would quietly
				// address a different resource if they went through unescaped.
				"range": "Q1/2026 Data!A:D",
			},
			wantPath: "/spreadsheets/1BxiMVs/values/Q1%2F2026%20Data%21A:D",
		},
		{
			name: "query placeholders are substituted",
			call: ConnectorCall{
				Operation: "listMessages",
				Path:      "/users/me/messages",
				Query:     map[string]string{"q": "{q}", "maxResults": "{maxResults}"},
				Params: []ParamSpec{
					{Name: "q", Type: ParamString},
					{Name: "maxResults", Type: ParamNumber},
				},
			},
			values: map[string]any{
				"operation":  "listMessages",
				"q":          "is:unread from:a@b.com",
				"maxResults": 25,
			},
			wantQry: "maxResults=25&q=is%3Aunread+from%3Aa%40b.com",
		},
		{
			name: "a query entry whose parameter is empty is left off",
			call: ConnectorCall{
				Operation: "listMessages",
				Path:      "/users/me/messages",
				Query:     map[string]string{"q": "{q}", "maxResults": "{maxResults}"},
				Params: []ParamSpec{
					{Name: "q", Type: ParamString},
					{Name: "maxResults", Type: ParamNumber},
				},
			},
			values:  map[string]any{"operation": "listMessages", "maxResults": 25},
			wantQry: "maxResults=25",
		},
		{
			name: "body placeholders are json-encoded",
			call: ConnectorCall{
				Operation: "postMessage",
				Method:    http.MethodPost,
				Path:      "/chat.postMessage",
				Body:      `{"channel":{channel},"text":{text},"thread_ts":{thread_ts}}`,
				Params: []ParamSpec{
					{Name: "channel", Type: ParamString, Required: true},
					{Name: "text", Type: ParamString, Required: true},
					{Name: "thread_ts", Type: ParamString},
				},
			},
			values: map[string]any{
				"operation": "postMessage",
				"channel":   "C123",
				// The whole reason placeholders carry JSON encodings: this text
				// would end the string early and break the document if it were
				// substituted into a quoted slot.
				"text": "he said \"ship it\"\nthen left",
			},
			wantBody: map[string]any{
				"channel": "C123",
				"text":    "he said \"ship it\"\nthen left",
			},
		},
		{
			name: "a typed body parameter keeps its type",
			call: ConnectorCall{
				Operation: "appendRow",
				Method:    http.MethodPost,
				Path:      "/spreadsheets/{id}/values/{range}:append",
				Body:      `{"values":{values}}`,
				Params: []ParamSpec{
					{Name: "id", Type: ParamString, Required: true},
					{Name: "range", Type: ParamString, Required: true},
					{Name: "values", Type: ParamJSON, Required: true},
				},
			},
			values: map[string]any{
				"operation": "appendRow",
				"id":        "sheet1",
				"range":     "Sheet1!A:B",
				"values":    `[["a","b"]]`,
			},
			wantPath: "/spreadsheets/sheet1/values/Sheet1%21A:B:append",
			wantBody: map[string]any{"values": []any{[]any{"a", "b"}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newRecorder(t, `{"ok":true}`)
			node := ConnectorNode{Spec: connectorSpec(tc.call), BaseURL: srv.URL}

			if _, err := runConnector(node, tc.values, nil); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if tc.wantPath != "" && srv.escapedPath != tc.wantPath {
				t.Errorf("path = %q, want %q", srv.escapedPath, tc.wantPath)
			}
			if tc.wantQry != "" && srv.rawQuery != tc.wantQry {
				t.Errorf("query = %q, want %q", srv.rawQuery, tc.wantQry)
			}
			if tc.wantBody != nil {
				var got map[string]any
				if err := json.Unmarshal([]byte(srv.body), &got); err != nil {
					t.Fatalf("the body was not valid JSON (%v): %s", err, srv.body)
				}
				if !reflect.DeepEqual(got, tc.wantBody) {
					t.Errorf("body = %#v, want %#v", got, tc.wantBody)
				}
			}
		})
	}
}

func TestConnectorNodeOmitsAnEmptyOptionalBodyField(t *testing.T) {
	srv := newRecorder(t, `{"ok":true}`)
	node := ConnectorNode{
		Spec: connectorSpec(ConnectorCall{
			Operation: "postMessage",
			Method:    http.MethodPost,
			Path:      "/chat.postMessage",
			Body:      `{"channel":{channel},"thread_ts":{thread_ts}}`,
			Params: []ParamSpec{
				{Name: "channel", Type: ParamString, Required: true},
				{Name: "thread_ts", Type: ParamString},
			},
		}),
		BaseURL: srv.URL,
	}

	if _, err := runConnector(node, map[string]any{"operation": "postMessage", "channel": "C1"}, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(srv.body, "thread_ts") {
		t.Errorf("an unset optional field was sent anyway: %s", srv.body)
	}
}

/* --- output shaping ------------------------------------------------------- */

func TestConnectorNodeShapesTheOutput(t *testing.T) {
	tests := []struct {
		name      string
		itemsPath string
		reply     string
		want      []map[string]any
	}{
		{
			name:      "ItemsPath plucks an array, one item per element",
			itemsPath: "messages",
			reply:     `{"ok":true,"messages":[{"ts":"1"},{"ts":"2"}]}`,
			want:      []map[string]any{{"ts": "1"}, {"ts": "2"}},
		},
		{
			name:      "a dotted ItemsPath walks into the response",
			itemsPath: "data.rows",
			reply:     `{"data":{"rows":[{"id":1}]}}`,
			want:      []map[string]any{{"id": float64(1)}},
		},
		{
			name:      "a non-object element is carried under value",
			itemsPath: "values",
			reply:     `{"values":[["a","b"],["c"]]}`,
			want: []map[string]any{
				{"value": []any{"a", "b"}},
				{"value": []any{"c"}},
			},
		},
		{
			name:      "an absent ItemsPath is an empty result, not a failure",
			itemsPath: "messages",
			reply:     `{"ok":true}`,
			want:      []map[string]any{},
		},
		{
			name:      "no ItemsPath makes the whole response one item",
			itemsPath: "",
			reply:     `{"ok":true,"ts":"1710000000.1"}`,
			want:      []map[string]any{{"ok": true, "ts": "1710000000.1"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newRecorder(t, tc.reply)
			node := ConnectorNode{
				Spec:    connectorSpec(ConnectorCall{Operation: "run", Path: "/x", ItemsPath: tc.itemsPath}),
				BaseURL: srv.URL,
			}

			res, err := runConnector(node, map[string]any{"operation": "run"}, nil)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			got := res.Get(HandleMain)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d items, want %d: %#v", len(got), len(tc.want), got)
			}
			for i, want := range tc.want {
				if !reflect.DeepEqual(got[i].JSON, want) {
					t.Errorf("item %d = %#v, want %#v", i, got[i].JSON, want)
				}
			}
		})
	}
}

func TestConnectorNodeRejectsANonArrayItemsPath(t *testing.T) {
	srv := newRecorder(t, `{"messages":"nope"}`)
	node := ConnectorNode{
		Spec:    connectorSpec(ConnectorCall{Operation: "run", Path: "/x", ItemsPath: "messages"}),
		BaseURL: srv.URL,
	}

	_, err := runConnector(node, map[string]any{"operation": "run"}, nil)
	assertNodeError(t, err, domain.ErrCodeHTTP, "array")
}

/* --- failures ------------------------------------------------------------- */

func TestConnectorNodeReportsFailures(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		reply     string
		okField   string
		errField  string
		wantCode  string
		wantParts []string
	}{
		{
			name:      "a 4xx quotes the status and the body",
			status:    http.StatusNotFound,
			reply:     `{"error":"no such spreadsheet"}`,
			wantCode:  domain.ErrCodeHTTP,
			wantParts: []string{"404", "no such spreadsheet"},
		},
		{
			// Slack's whole failure mode: 200 OK with the verdict in the body.
			name:      "ok:false in a 200 body is a failure",
			status:    http.StatusOK,
			reply:     `{"ok":false,"error":"channel_not_found"}`,
			okField:   "ok",
			errField:  "error",
			wantCode:  domain.ErrCodeHTTP,
			wantParts: []string{"reported failure", "channel_not_found"},
		},
		{
			name:      "ok:false with no error field still fails, quoting the body",
			status:    http.StatusOK,
			reply:     `{"ok":false}`,
			okField:   "ok",
			errField:  "error",
			wantCode:  domain.ErrCodeHTTP,
			wantParts: []string{"reported failure", `{"ok":false}`},
		},
		{
			name:     "a reply that is not JSON is a failure",
			status:   http.StatusOK,
			reply:    `<html>maintenance</html>`,
			wantCode: domain.ErrCodeHTTP,
			// The snippet is what tells the author they hit a proxy, not the API.
			wantParts: []string{"not valid JSON", "maintenance"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newRecorder(t, tc.reply)
			srv.status = tc.status
			node := ConnectorNode{
				Spec: connectorSpec(ConnectorCall{
					Operation:  "run",
					Path:       "/x",
					OKField:    tc.okField,
					ErrorField: tc.errField,
				}),
				BaseURL: srv.URL,
			}

			_, err := runConnector(node, map[string]any{"operation": "run"}, nil)
			assertNodeError(t, err, tc.wantCode, tc.wantParts...)
		})
	}
}

func TestConnectorNodeAcceptsOKTrue(t *testing.T) {
	srv := newRecorder(t, `{"ok":true,"ts":"1"}`)
	node := ConnectorNode{
		Spec:    connectorSpec(ConnectorCall{Operation: "run", Path: "/x", OKField: "ok", ErrorField: "error"}),
		BaseURL: srv.URL,
	}

	res, err := runConnector(node, map[string]any{"operation": "run"}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := len(res.Get(HandleMain)); got != 1 {
		t.Fatalf("got %d items, want 1", got)
	}
}

func TestConnectorNodeIgnoresAMissingOKField(t *testing.T) {
	// An endpoint of the same app that does not carry the flag must not be
	// treated as a silent failure.
	srv := newRecorder(t, `{"ts":"1"}`)
	node := ConnectorNode{
		Spec:    connectorSpec(ConnectorCall{Operation: "run", Path: "/x", OKField: "ok", ErrorField: "error"}),
		BaseURL: srv.URL,
	}

	if _, err := runConnector(node, map[string]any{"operation": "run"}, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestConnectorNodeRejectsAnUnknownOperation(t *testing.T) {
	srv := newRecorder(t, `{}`)
	node := ConnectorNode{
		Spec:    connectorSpec(ConnectorCall{Operation: "postMessage", Path: "/x"}),
		BaseURL: srv.URL,
	}

	_, err := runConnector(node, map[string]any{"operation": "postMassage"}, nil)
	assertNodeError(t, err, domain.ErrCodeValidation, "postMassage", "postMessage")
	if srv.calls != 0 {
		t.Errorf("a request was sent for an operation that does not exist")
	}
}

func TestConnectorNodeRejectsAMissingRequiredParam(t *testing.T) {
	srv := newRecorder(t, `{}`)
	node := ConnectorNode{
		Spec: connectorSpec(ConnectorCall{
			Operation: "postMessage",
			Method:    http.MethodPost,
			Path:      "/chat.postMessage",
			Body:      `{"channel":{channel},"text":{text}}`,
			Params: []ParamSpec{
				{Name: "channel", Type: ParamString, Required: true},
				{Name: "text", Type: ParamString, Required: true},
			},
		}),
		BaseURL: srv.URL,
	}

	_, err := runConnector(node, map[string]any{"operation": "postMessage", "channel": "C1"}, nil)
	assertNodeError(t, err, domain.ErrCodeValidation, `"text"`, "required")
	if srv.calls != 0 {
		t.Errorf("an incomplete request was sent anyway")
	}
}

/* --- credentials ---------------------------------------------------------- */

func TestConnectorNodeAppliesTheCredential(t *testing.T) {
	srv := newRecorder(t, `{}`)
	node := ConnectorNode{
		Spec:    connectorSpec(ConnectorCall{Operation: "run", Path: "/x", Credential: "slackOAuth2"}),
		BaseURL: srv.URL,
	}
	cred := fakeCredential{credType: "slackOAuth2", name: "Team Slack", header: "Bearer xoxb-test"}

	if _, err := runConnector(node, map[string]any{"operation": "run"}, cred); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if srv.authHeader != "Bearer xoxb-test" {
		t.Errorf("Authorization = %q, want the header the resolver set", srv.authHeader)
	}
}

// Resolution is lazy, so a resolver reads an empty Type() until its credential
// has actually been opened. Treating that as "none chosen" told users to pick a
// credential they had already picked, on every first request.
func TestConnectorNodeTrustsAResolverThatHasNotResolvedYet(t *testing.T) {
	srv := newRecorder(t, `{"ok":true}`)
	node := ConnectorNode{
		Spec:    connectorSpec(ConnectorCall{Operation: "run", Path: "/x", Credential: "slackOAuth2"}),
		BaseURL: srv.URL,
	}

	// An empty Type() with a working Apply: exactly what the engine's lazy
	// binding looks like before anything has resolved.
	_, err := runConnector(node, map[string]any{"operation": "run"},
		fakeCredential{header: "Bearer signed-anyway"})
	if err != nil {
		t.Fatalf("an unresolved-but-present credential must be trusted: %v", err)
	}
	if srv.calls != 1 {
		t.Fatalf("the request was not sent: %d calls", srv.calls)
	}
	if got := srv.authHeader; got != "Bearer signed-anyway" {
		t.Fatalf("Authorization %q — Apply is what proves a credential, not Type()", got)
	}
}

func TestConnectorNodeCredentialProblems(t *testing.T) {
	tests := []struct {
		name      string
		cred      CredentialResolver
		wantParts []string
	}{
		{
			name:      "none configured names the type to add",
			cred:      nil,
			wantParts: []string{"slackOAuth2", "Credentials"},
		},
		{
			// The real "configured but never authorised" case: the resolver
			// exists and its Apply is what reports the problem.
			name: "an unconnected credential is reported with its own reason",
			cred: fakeCredential{
				credType: "slackOAuth2",
				name:     "Acme Slack",
				err:      errors.New(`the credential "Acme Slack" has not been connected yet`),
			},
			wantParts: []string{"slackOAuth2", "has not been connected"},
		},
		{
			name:      "the wrong type is named rather than silently used",
			cred:      fakeCredential{credType: "gmailOAuth2", name: "My Gmail"},
			wantParts: []string{"slackOAuth2", "gmailOAuth2", "My Gmail"},
		},
		{
			name:      "a resolver that fails is reported with its reason",
			cred:      fakeCredential{credType: "slackOAuth2", err: errors.New("has not been connected yet")},
			wantParts: []string{"slackOAuth2", "has not been connected yet"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newRecorder(t, `{}`)
			node := ConnectorNode{
				Spec:    connectorSpec(ConnectorCall{Operation: "run", Path: "/x", Credential: "slackOAuth2"}),
				BaseURL: srv.URL,
			}

			_, err := runConnector(node, map[string]any{"operation": "run"}, tc.cred)
			assertNodeError(t, err, domain.ErrCodeValidation, tc.wantParts...)
			if srv.calls != 0 {
				t.Errorf("an unauthenticated request was sent anyway")
			}
		})
	}
}

func TestConnectorNodeSkipsTheCredentialWhenNoneIsDeclared(t *testing.T) {
	srv := newRecorder(t, `{}`)
	node := ConnectorNode{Spec: connectorSpec(ConnectorCall{Operation: "run", Path: "/x"}), BaseURL: srv.URL}

	if _, err := runConnector(node, map[string]any{"operation": "run"}, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

/* --- descriptor and the placeholder syntax -------------------------------- */

func TestConnectorNodeDescriptorComesFromTheDeclaration(t *testing.T) {
	// The descriptor is built by the declaration package — the gating and the
	// collision check are tested there, where Connector is reachable. What this
	// node owes is to pass it through untouched.
	want := Descriptor{Type: "slack", Name: "Slack", Mode: ModePerItem, Credential: "slackOAuth2"}
	node := ConnectorNode{Spec: fakeConnector{descriptor: want}}

	if got := node.Descriptor(); !reflect.DeepEqual(got, want) {
		t.Errorf("Descriptor() = %#v, want %#v", got, want)
	}
}

func TestConnectorPlaceholders(t *testing.T) {
	tests := []struct {
		tmpl string
		want []string
	}{
		{`/users/me/messages/{messageId}`, []string{"messageId"}},
		{`{"channel":{channel},"text":{text}}`, []string{"channel", "text"}},
		// Object braces and an empty object are not placeholders, which is what
		// lets a JSON template hold both.
		{`{"a":{},"b":{"c":1}}`, nil},
		{`no placeholders here`, nil},
		{`{ spaced }`, nil},
	}

	for _, tc := range tests {
		got := ConnectorPlaceholders(tc.tmpl)
		if len(got) == 0 && len(tc.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ConnectorPlaceholders(%q) = %v, want %v", tc.tmpl, got, tc.want)
		}
	}
}

func TestConnectorSubstitute(t *testing.T) {
	got := ConnectorSubstitute(`/a/{one}/b/{two}`, func(name string) string {
		return map[string]string{"one": "1", "two": "2"}[name]
	})
	if want := "/a/1/b/2"; got != want {
		t.Errorf("ConnectorSubstitute = %q, want %q", got, want)
	}
}

/* --- helpers -------------------------------------------------------------- */

// assertNodeError checks the code and that the message says enough for a
// workflow author to act on it.
func assertNodeError(t *testing.T, err error, code string, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with code %q, got none", code)
	}
	var ne *domain.NodeError
	if !errors.As(err, &ne) {
		t.Fatalf("expected a *domain.NodeError, got %T: %v", err, err)
	}
	if ne.Code != code {
		t.Fatalf("error code = %q, want %q (message: %s)", ne.Code, code, ne.Message)
	}
	for _, part := range parts {
		if !strings.Contains(ne.Message, part) {
			t.Errorf("error message %q does not mention %q", ne.Message, part)
		}
	}
}
