package nodes

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// failBody is long enough that the error message has to truncate it.
var failBody = `{"error":"nope","detail":"` + strings.Repeat("x", 400) + `"}`

// echoServer answers the handful of shapes the http.request tests need.
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		user, password, hasBasic := r.BasicAuth()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Trace", "t-1")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"method":        r.Method,
			"query":         r.URL.RawQuery,
			"authorization": r.Header.Get("Authorization"),
			"apiKey":        r.Header.Get("X-API-Key"),
			"custom":        r.Header.Get("X-Custom"),
			"contentType":   r.Header.Get("Content-Type"),
			"body":          string(body),
			"basicUser":     user,
			"basicPass":     password,
			"hasBasic":      hasBasic,
		}))
	})
	mux.HandleFunc("/text", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Trace", "t-1")
		_, _ = w.Write([]byte("hello world"))
	})
	// JSON body served under a text content type, which is what separates
	// auto-detection from an explicit format.
	mux.HandleFunc("/jsontext", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Trace", "t-1")
		_, _ = w.Write([]byte(`{"a":1}`))
	})
	mux.HandleFunc("/fail", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(failBody))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// runHTTP executes the node with the given parameters and one empty input item.
func runHTTP(ctx context.Context, values map[string]any) (Result, error) {
	return HTTPRequest{}.Execute(ExecContext{
		Ctx:    ctx,
		Params: params(values),
		Items:  items(map[string]any{}),
		Item:   domain.NewItem(nil),
	})
}

// echoed unwraps the JSON object the /echo handler returned.
func echoed(t *testing.T, res Result) map[string]any {
	t.Helper()
	out := res.Get(HandleMain)
	require.Len(t, out, 1)
	body, ok := out[0].JSON["body"].(map[string]any)
	require.True(t, ok, "expected a parsed JSON body, got %T", out[0].JSON["body"])
	return body
}

func TestHTTPRequestAuthentication(t *testing.T) {
	srv := echoServer(t)
	cases := []struct {
		name   string
		values map[string]any
		assert func(t *testing.T, body map[string]any)
	}{
		{
			name:   "none sends no credentials",
			values: map[string]any{"authentication": "none"},
			assert: func(t *testing.T, body map[string]any) {
				assert.Empty(t, body["authorization"])
				assert.Equal(t, false, body["hasBasic"])
			},
		},
		{
			name: "basic sends the user and password",
			values: map[string]any{
				"authentication": "basic",
				"authUser":       "alice",
				"authPassword":   "s3cr3t",
			},
			assert: func(t *testing.T, body map[string]any) {
				assert.Equal(t, true, body["hasBasic"])
				assert.Equal(t, "alice", body["basicUser"])
				assert.Equal(t, "s3cr3t", body["basicPass"])
			},
		},
		{
			name:   "bearer sends the token",
			values: map[string]any{"authentication": "bearer", "authToken": "tok-123"},
			assert: func(t *testing.T, body map[string]any) {
				assert.Equal(t, "Bearer tok-123", body["authorization"])
			},
		},
		{
			name: "custom header sends the named header",
			values: map[string]any{
				"authentication":  "header",
				"authHeaderName":  "X-API-Key",
				"authHeaderValue": "k-1",
			},
			assert: func(t *testing.T, body map[string]any) {
				assert.Equal(t, "k-1", body["apiKey"])
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.values["url"] = srv.URL + "/echo"
			res, err := runHTTP(context.Background(), tc.values)
			require.NoError(t, err)
			tc.assert(t, echoed(t, res))
		})
	}
}

func TestHTTPRequestMissingAuthSecrets(t *testing.T) {
	srv := echoServer(t)
	cases := []struct {
		name   string
		values map[string]any
	}{
		{"bearer without a token", map[string]any{"authentication": "bearer"}},
		{"custom header without a name", map[string]any{"authentication": "header"}},
		{"unknown mode", map[string]any{"authentication": "oauth9"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.values["url"] = srv.URL + "/echo"
			_, err := runHTTP(context.Background(), tc.values)
			var nodeErr *domain.NodeError
			require.ErrorAs(t, err, &nodeErr)
			assert.Equal(t, domain.ErrCodeValidation, nodeErr.Code)
		})
	}
}

func TestHTTPRequestResponseFormat(t *testing.T) {
	srv := echoServer(t)
	cases := []struct {
		name   string
		path   string
		format string
		assert func(t *testing.T, body any)
	}{
		{
			name: "auto parses a JSON content type", path: "/echo", format: "auto",
			assert: func(t *testing.T, body any) {
				parsed, ok := body.(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "GET", parsed["method"])
			},
		},
		{
			name: "auto keeps text as a string", path: "/text", format: "auto",
			assert: func(t *testing.T, body any) { assert.Equal(t, "hello world", body) },
		},
		{
			name: "auto trusts the content type over the bytes", path: "/jsontext", format: "auto",
			assert: func(t *testing.T, body any) { assert.Equal(t, `{"a":1}`, body) },
		},
		{
			name: "json parses regardless of the content type", path: "/jsontext", format: "json",
			assert: func(t *testing.T, body any) {
				assert.Equal(t, map[string]any{"a": float64(1)}, body)
			},
		},
		{
			name: "text keeps JSON as a string", path: "/echo", format: "text",
			assert: func(t *testing.T, body any) {
				raw, ok := body.(string)
				require.True(t, ok)
				assert.Contains(t, raw, `"method":"GET"`)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := runHTTP(context.Background(), map[string]any{
				"url":            srv.URL + tc.path,
				"responseFormat": tc.format,
			})
			require.NoError(t, err)
			out := res.Get(HandleMain)
			require.Len(t, out, 1)
			assert.Equal(t, 200, out[0].JSON["statusCode"])
			assert.Equal(t, "t-1", out[0].JSON["headers"].(map[string]string)["X-Trace"])
			tc.assert(t, out[0].JSON["body"])
		})
	}
}

func TestHTTPRequestSendsHeadersQueryAndBody(t *testing.T) {
	srv := echoServer(t)
	cases := []struct {
		name   string
		values map[string]any
		assert func(t *testing.T, body map[string]any)
	}{
		{
			name: "headers",
			values: map[string]any{
				"sendHeaders": true,
				"headers":     map[string]any{"X-Custom": "v1"},
			},
			assert: func(t *testing.T, body map[string]any) {
				assert.Equal(t, "v1", body["custom"])
			},
		},
		{
			name: "query parameters",
			values: map[string]any{
				"sendQuery": true,
				"query":     map[string]any{"a": "1", "b": 2},
			},
			assert: func(t *testing.T, body map[string]any) {
				assert.Equal(t, "a=1&b=2", body["query"])
			},
		},
		{
			name: "json body",
			values: map[string]any{
				"method":   "POST",
				"sendBody": true,
				"body":     `{"a":1}`,
			},
			assert: func(t *testing.T, body map[string]any) {
				assert.Equal(t, "POST", body["method"])
				assert.Equal(t, "application/json", body["contentType"])
				assert.Equal(t, `{"a":1}`, body["body"])
			},
		},
		{
			name: "form body",
			values: map[string]any{
				"method":          "POST",
				"sendBody":        true,
				"bodyContentType": "form",
				"body":            map[string]any{"a": 1, "b": "two"},
			},
			assert: func(t *testing.T, body map[string]any) {
				assert.Equal(t, "application/x-www-form-urlencoded", body["contentType"])
				assert.Equal(t, "a=1&b=two", body["body"])
			},
		},
		{
			name: "raw body",
			values: map[string]any{
				"method":          "PUT",
				"sendBody":        true,
				"bodyContentType": "raw",
				"body":            "plain text",
			},
			assert: func(t *testing.T, body map[string]any) {
				assert.Equal(t, "text/plain; charset=utf-8", body["contentType"])
				assert.Equal(t, "plain text", body["body"])
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.values["url"] = srv.URL + "/echo"
			res, err := runHTTP(context.Background(), tc.values)
			require.NoError(t, err)
			tc.assert(t, echoed(t, res))
		})
	}
}

func TestHTTPRequestNonSuccessIsAnError(t *testing.T) {
	srv := echoServer(t)
	_, err := runHTTP(context.Background(), map[string]any{"url": srv.URL + "/fail"})

	var nodeErr *domain.NodeError
	require.ErrorAs(t, err, &nodeErr)
	assert.Equal(t, domain.ErrCodeHTTP, nodeErr.Code)
	assert.Equal(t, http.StatusUnprocessableEntity, nodeErr.Status)
	// The message has to name the call, the status and the start of the body.
	assert.Contains(t, nodeErr.Message, "GET "+srv.URL+"/fail")
	assert.Contains(t, nodeErr.Message, "422 Unprocessable Entity")
	assert.Contains(t, nodeErr.Message, `{"error":"nope"`)
	assert.Contains(t, nodeErr.Message, "...", "a long body should be truncated")
	assert.NotContains(t, nodeErr.Message, strings.Repeat("x", 400))
}

func TestHTTPRequestIgnoreHTTPErrors(t *testing.T) {
	srv := echoServer(t)
	res, err := runHTTP(context.Background(), map[string]any{
		"url":              srv.URL + "/fail",
		"ignoreHTTPErrors": true,
	})
	require.NoError(t, err)

	out := res.Get(HandleMain)
	require.Len(t, out, 1)
	assert.Equal(t, http.StatusUnprocessableEntity, out[0].JSON["statusCode"])
	body, ok := out[0].JSON["body"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "nope", body["error"])
}

func TestHTTPRequestHonoursContextCancellation(t *testing.T) {
	srv := echoServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := runHTTP(ctx, map[string]any{"url": srv.URL + "/echo"})
	var nodeErr *domain.NodeError
	require.ErrorAs(t, err, &nodeErr)
	assert.Equal(t, domain.ErrCodeCancelled, nodeErr.Code)
}

func TestHTTPRequestRequiresURL(t *testing.T) {
	_, err := runHTTP(context.Background(), map[string]any{"url": "   "})
	var nodeErr *domain.NodeError
	require.ErrorAs(t, err, &nodeErr)
	assert.Equal(t, domain.ErrCodeValidation, nodeErr.Code)
	assert.Contains(t, nodeErr.Message, "url")
}
