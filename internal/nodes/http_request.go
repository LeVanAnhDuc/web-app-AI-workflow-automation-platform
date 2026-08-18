package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// errorBodyLimit is how much of a failing response body goes into the error
// message. Enough to show an API's own error JSON, short enough that a stray
// HTML error page does not fill the log panel.
const errorBodyLimit = 300

// HTTPRequest calls an HTTP endpoint. It runs perItem so a list of records fans
// out into one call each with no loop node.
type HTTPRequest struct{}

// Descriptor implements Node.
func (HTTPRequest) Descriptor() Descriptor {
	authShown := func(modes ...any) *ShowWhen {
		return &ShowWhen{Param: "authentication", Equals: modes}
	}
	return Descriptor{
		Type:        "http.request",
		Name:        "HTTP Request",
		Category:    CategoryCore,
		Description: "Calls an HTTP endpoint once per input item and returns the response.",
		Icon:        "globe",
		Mode:        ModePerItem,
		Inputs:      MainIn,
		Outputs:     MainOut,
		Params: []ParamSpec{
			{
				Name:    "method",
				Label:   "Method",
				Type:    ParamSelect,
				Default: "GET",
				Options: []ParamOption{
					{Label: "GET", Value: "GET"},
					{Label: "POST", Value: "POST"},
					{Label: "PUT", Value: "PUT"},
					{Label: "PATCH", Value: "PATCH"},
					{Label: "DELETE", Value: "DELETE"},
					{Label: "HEAD", Value: "HEAD"},
				},
			},
			{
				Name:               "url",
				Label:              "URL",
				Type:               ParamString,
				Required:           true,
				Placeholder:        "https://api.example.com/users/{{ $json.id }}",
				Description:        "Absolute URL to call. An expression lets every item hit a different URL.",
				SupportsExpression: true,
			},
			{
				Name:    "authentication",
				Label:   "Authentication",
				Type:    ParamSelect,
				Default: "none",
				Options: []ParamOption{
					{Label: "None", Value: "none"},
					{Label: "Basic Auth", Value: "basic"},
					{Label: "Bearer Token", Value: "bearer"},
					{Label: "Custom Header", Value: "header"},
				},
				Description: "Phase 1 takes the secret inline; stored credentials arrive with the vault.",
			},
			{
				Name:               "authUser",
				Label:              "Username",
				Type:               ParamString,
				ShowWhen:           authShown("basic"),
				SupportsExpression: true,
			},
			{
				Name:               "authPassword",
				Label:              "Password",
				Type:               ParamString,
				ShowWhen:           authShown("basic"),
				SupportsExpression: true,
			},
			{
				Name:               "authToken",
				Label:              "Token",
				Type:               ParamString,
				Description:        "Sent as Authorization: Bearer <token>.",
				ShowWhen:           authShown("bearer"),
				SupportsExpression: true,
			},
			{
				Name:               "authHeaderName",
				Label:              "Header Name",
				Type:               ParamString,
				Placeholder:        "X-API-Key",
				ShowWhen:           authShown("header"),
				SupportsExpression: true,
			},
			{
				Name:               "authHeaderValue",
				Label:              "Header Value",
				Type:               ParamString,
				ShowWhen:           authShown("header"),
				SupportsExpression: true,
			},
			{
				Name:    "sendHeaders",
				Label:   "Send Headers",
				Type:    ParamBoolean,
				Default: false,
			},
			{
				Name:               "headers",
				Label:              "Headers",
				Type:               ParamKeyValue,
				Description:        "One header name and value per row.",
				ShowWhen:           &ShowWhen{Param: "sendHeaders", Equals: []any{true}},
				SupportsExpression: true,
			},
			{
				Name:    "sendQuery",
				Label:   "Send Query Parameters",
				Type:    ParamBoolean,
				Default: false,
			},
			{
				Name:               "query",
				Label:              "Query Parameters",
				Type:               ParamKeyValue,
				Description:        "Added on top of any query string the URL already carries.",
				ShowWhen:           &ShowWhen{Param: "sendQuery", Equals: []any{true}},
				SupportsExpression: true,
			},
			{
				Name:        "sendBody",
				Label:       "Send Body",
				Type:        ParamBoolean,
				Default:     false,
				Description: "Leave this off for GET and HEAD, which carry no body.",
			},
			{
				Name:    "bodyContentType",
				Label:   "Body Content Type",
				Type:    ParamSelect,
				Default: "json",
				Options: []ParamOption{
					{Label: "JSON", Value: "json"},
					{Label: "Form URL-Encoded", Value: "form"},
					{Label: "Raw", Value: "raw"},
				},
				ShowWhen: &ShowWhen{Param: "sendBody", Equals: []any{true}},
			},
			{
				Name:               "body",
				Label:              "Body",
				Type:               ParamJSON,
				Placeholder:        `{ "name": "{{ $json.name }}" }`,
				Description:        "Request payload. Expressions are resolved before it is sent.",
				ShowWhen:           &ShowWhen{Param: "sendBody", Equals: []any{true}},
				SupportsExpression: true,
			},
			{
				Name:    "responseFormat",
				Label:   "Response Format",
				Type:    ParamSelect,
				Default: "auto",
				Options: []ParamOption{
					{Label: "Auto-detect", Value: "auto"},
					{Label: "JSON", Value: "json"},
					{Label: "Text", Value: "text"},
				},
				Description: "Auto parses JSON when the response says it is JSON, and keeps text otherwise.",
			},
			{
				Name:    "ignoreHTTPErrors",
				Label:   "Ignore HTTP Errors",
				Type:    ParamBoolean,
				Default: false,
				Description: "Treat a non-2xx status as a normal response instead of an error, " +
					"so the workflow can branch on statusCode itself.",
			},
		},
	}
}

// Execute performs one request and returns one item describing the response.
func (HTTPRequest) Execute(ec ExecContext) (Result, error) {
	req, err := buildHTTPRequest(ec)
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
				Message: fmt.Sprintf("request to %s was interrupted: %v", req.URL, cause),
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
	closeErr := resp.Body.Close()
	if readErr != nil {
		return Result{}, &domain.NodeError{
			Code:    domain.ErrCodeHTTP,
			Status:  resp.StatusCode,
			Message: fmt.Sprintf("%s %s: reading the response body failed: %v", req.Method, req.URL, readErr),
		}
	}
	if closeErr != nil {
		ec.Log().Warn("http.request: closing the response body failed", "error", closeErr)
	}

	succeeded := resp.StatusCode >= 200 && resp.StatusCode < 300
	if !succeeded && !ec.Params.BoolOr("ignoreHTTPErrors", false) {
		return Result{}, &domain.NodeError{
			Code:    domain.ErrCodeHTTP,
			Status:  resp.StatusCode,
			Message: httpErrorMessage(req, resp.Status, raw),
		}
	}

	body, err := decodeResponseBody(raw, resp.Header.Get("Content-Type"), ec.Params.StringOr("responseFormat", "auto"))
	if err != nil {
		return Result{}, err
	}
	return Main(domain.NewItem(map[string]any{
		"statusCode": resp.StatusCode,
		"headers":    flattenHeaders(resp.Header),
		"body":       body,
	})), nil
}

// buildHTTPRequest turns the resolved parameters into a request. It is split out
// so that every failure before the call is reported as a validation error and
// only the call itself can produce an http_error.
func buildHTTPRequest(ec ExecContext) (*http.Request, error) {
	p := ec.Params
	method := strings.ToUpper(p.StringOr("method", http.MethodGet))

	target, err := p.String("url")
	if err != nil {
		return nil, err
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, ErrRequiredParam("url")
	}
	u, err := url.Parse(target)
	if err != nil {
		return nil, domain.Errorf(domain.ErrCodeValidation, "url %q is not a valid URL: %v", target, err)
	}
	if p.BoolOr("sendQuery", false) {
		rows, err := p.KeyValues("query")
		if err != nil {
			return nil, err
		}
		q := u.Query()
		for _, row := range rows {
			if row.Key == "" {
				continue
			}
			q.Add(row.Key, row.Value)
		}
		u.RawQuery = q.Encode()
	}

	var body io.Reader
	contentType := ""
	if p.BoolOr("sendBody", false) {
		raw, err := p.Raw("body")
		if err != nil {
			return nil, err
		}
		body, contentType, err = encodeRequestBody(raw, p.StringOr("bodyContentType", "json"))
		if err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequestWithContext(execCtx(ec), method, u.String(), body)
	if err != nil {
		return nil, domain.Errorf(domain.ErrCodeValidation, "could not build the request: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if p.BoolOr("sendHeaders", false) {
		rows, err := p.KeyValues("headers")
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.Key == "" {
				continue
			}
			req.Header.Set(row.Key, row.Value)
		}
	}
	// Auth goes on after the user's own headers so a stale Authorization row
	// cannot silently defeat the configured mode.
	if err := applyAuth(req, p); err != nil {
		return nil, err
	}
	return req, nil
}

// applyAuth adds the credentials for the selected authentication mode.
func applyAuth(req *http.Request, p ParamResolver) error {
	switch mode := p.StringOr("authentication", "none"); mode {
	case "", "none":
		return nil
	case "basic":
		req.SetBasicAuth(p.StringOr("authUser", ""), p.StringOr("authPassword", ""))
	case "bearer":
		token := strings.TrimSpace(p.StringOr("authToken", ""))
		if token == "" {
			return ErrRequiredParam("authToken")
		}
		req.Header.Set("Authorization", "Bearer "+token)
	case "header":
		name := strings.TrimSpace(p.StringOr("authHeaderName", ""))
		if name == "" {
			return ErrRequiredParam("authHeaderName")
		}
		req.Header.Set(name, p.StringOr("authHeaderValue", ""))
	default:
		return domain.Errorf(domain.ErrCodeValidation, "unknown authentication mode %q", mode)
	}
	return nil
}

// encodeRequestBody serialises the body parameter for the chosen content type.
// The parameter arrives either as the string the user typed or, when a whole-value
// expression resolved to a typed value, as a map or a slice.
func encodeRequestBody(raw any, contentType string) (io.Reader, string, error) {
	switch contentType {
	case "", "json":
		if s, ok := raw.(string); ok {
			// A hand-written JSON body is sent byte for byte, so a syntax error
			// surfaces as the server's own complaint rather than ours.
			return strings.NewReader(s), "application/json", nil
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, "", domain.Errorf(domain.ErrCodeValidation, "body is not JSON-encodable: %v", err)
		}
		return bytes.NewReader(encoded), "application/json", nil
	case "form":
		switch v := raw.(type) {
		case string:
			return strings.NewReader(v), "application/x-www-form-urlencoded", nil
		case map[string]any:
			form := url.Values{}
			for key, value := range v {
				form.Set(key, stringify(value))
			}
			return strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", nil
		default:
			return nil, "", domain.Errorf(domain.ErrCodeValidation,
				"a form body must be an object or an already-encoded string, got %T", raw)
		}
	case "raw":
		return strings.NewReader(stringify(raw)), "text/plain; charset=utf-8", nil
	default:
		return nil, "", domain.Errorf(domain.ErrCodeValidation, "unknown body content type %q", contentType)
	}
}

// decodeResponseBody turns the response bytes into the value placed on the
// output item's "body" key.
func decodeResponseBody(raw []byte, contentType, format string) (any, error) {
	text := string(raw)
	switch format {
	case "text":
		return text, nil
	case "json":
		if len(bytes.TrimSpace(raw)) == 0 {
			return nil, nil
		}
		var parsed any
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, domain.Errorf(domain.ErrCodeHTTP,
				"the response was not valid JSON: %v; body starts with %q", err, truncate(text, errorBodyLimit))
		}
		return parsed, nil
	default: // auto
		if !looksJSON(contentType) || len(bytes.TrimSpace(raw)) == 0 {
			return text, nil
		}
		var parsed any
		if err := json.Unmarshal(raw, &parsed); err != nil {
			// Auto-detect is a convenience: a mislabelled body degrades to text
			// instead of failing a node the user never asked to parse strictly.
			return text, nil
		}
		return parsed, nil
	}
}

// looksJSON reports whether a Content-Type announces JSON, covering the +json
// suffix that API-specific media types use.
func looksJSON(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "application/json") || strings.Contains(ct, "+json")
}

// flattenHeaders keeps the first value of each response header, which is the
// shape an expression can read without index juggling.
func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for name, values := range h {
		if len(values) > 0 {
			out[name] = values[0]
		}
	}
	return out
}

// httpErrorMessage builds the sentence shown in the execution log. It names the
// call and quotes the start of the body, because "404 Not Found" on its own
// never tells the user which of their calls broke or why.
func httpErrorMessage(req *http.Request, status string, body []byte) string {
	msg := fmt.Sprintf("%s %s returned %s", req.Method, req.URL, status)
	if snippet := strings.TrimSpace(truncate(string(body), errorBodyLimit)); snippet != "" {
		msg += ": " + snippet
	}
	return msg
}

// truncate cuts a string to n bytes, marking that it was cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// execCtx defaults a zero ExecContext's context so a node stays usable from a
// test or from POST /nodes/{type}/test, neither of which runs under the engine.
func execCtx(ec ExecContext) context.Context {
	if ec.Ctx == nil {
		return context.Background()
	}
	return ec.Ctx
}
