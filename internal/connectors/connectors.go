// Package connectors declares the third-party applications this build can talk
// to.
//
// A connector is a declaration, not code. One generic executor —
// nodes.ConnectorNode — turns any of these into a working node, so adding an
// app is a data file rather than a Go implementation plus a frontend form. The
// descriptor a connector builds gates every operation's parameters behind the
// chosen operation, which is what lets the existing generic config drawer
// render a correct form for an app it has never heard of.
//
// The dependency runs one way, connectors -> nodes: a declaration is written in
// the node system's own vocabulary (nodes.ParamSpec, nodes.Descriptor), and the
// executor reads a connector back through the nodes.ConnectorSpec interface.
// That direction is why nodes.Default() cannot list these itself — see Register.
package connectors

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

// Connector is one app.
type Connector struct {
	ID          string // "slack" — also the node type
	Name        string // "Slack"
	Icon        string
	Description string

	// Credential is the credential type id from internal/credentials, such as
	// "slackOAuth2". Empty means the app needs no authentication.
	Credential string

	// BaseURL is the API root every operation's Path is appended to, with no
	// trailing slash.
	BaseURL string

	Operations []Operation

	// OKField and ErrorField describe an API that reports failure in the body of
	// a 200 response. Slack is the reason this exists: chat.postMessage answers
	// 200 with {"ok": false, "error": "channel_not_found"}, so without this
	// every Slack failure would look like a success and the workflow would carry
	// on with an item that means nothing.
	//
	// The check fires only when the field is present and falsy, so an endpoint
	// of the same app that does not carry it is unaffected.
	OKField    string // dotted path, e.g. "ok"
	ErrorField string // dotted path to the message, e.g. "error"
}

// Operation is one thing the app can do.
type Operation struct {
	ID          string // "postMessage"
	Name        string // "Send a message"
	Description string

	Method string // "POST"

	// Path is appended to the connector's BaseURL and may contain {param}
	// placeholders. Values substituted here are URL-escaped, so a spreadsheet
	// range with a space or a slash in it cannot invent a path segment.
	Path string

	Params []nodes.ParamSpec

	// Query entries are added to the URL. Values may contain {param}
	// placeholders; an entry whose parameters resolved to nothing is left off
	// entirely, because "?q=" and no q at all are different requests.
	Query map[string]string

	// Body is a JSON template. Placeholders are replaced with the JSON encoding
	// of the value, so they stand where a JSON value goes and are NOT wrapped in
	// quotes: {"text":{text}}, never {"text":"{text}"}. That is what stops a
	// quote or a newline in user data from breaking the document.
	//
	// A placeholder whose parameter is empty becomes null, and null members are
	// dropped before sending, so an optional field is omitted rather than sent
	// blank — which is what most APIs actually want.
	Body string

	// ItemsPath plucks a sub-array from the response so each element becomes one
	// item. Empty means the whole response is one item. A dotted path walks
	// nested objects; a path that is absent or null yields no items, which is
	// how an empty result set should read.
	ItemsPath string
}

// Operation finds one operation by id.
func (c Connector) Operation(id string) (Operation, bool) {
	for _, op := range c.Operations {
		if op.ID == id {
			return op, true
		}
	}
	return Operation{}, false
}

// OperationIDs lists the operation ids in declaration order. It implements part
// of nodes.ConnectorSpec, so an unknown operation can be reported with the real
// choices instead of a bare failure.
func (c Connector) OperationIDs() []string {
	out := make([]string, 0, len(c.Operations))
	for _, op := range c.Operations {
		out = append(out, op.ID)
	}
	return out
}

// Call flattens one operation into the request description the executor works
// from. It implements part of nodes.ConnectorSpec.
func (c Connector) Call(operation string) (nodes.ConnectorCall, bool) {
	op, ok := c.Operation(operation)
	if !ok {
		return nodes.ConnectorCall{}, false
	}
	return nodes.ConnectorCall{
		Connector:  c.ID,
		Operation:  op.ID,
		Credential: c.Credential,
		BaseURL:    c.BaseURL,
		Method:     strings.ToUpper(op.Method),
		Path:       op.Path,
		Query:      op.Query,
		Body:       op.Body,
		ItemsPath:  op.ItemsPath,
		Params:     op.Params,
		OKField:    c.OKField,
		ErrorField: c.ErrorField,
	}, true
}

// Node wraps this connector as an executable node.
func (c Connector) Node() nodes.Node { return nodes.ConnectorNode{Spec: c} }

/* --- the descriptor ------------------------------------------------------- */

// Descriptor builds the node descriptor: an "operation" select listing every
// operation, followed by every operation's parameters, each visible only when
// its own operation is chosen.
//
// This is the whole trick of the phase. ShowWhen is already evaluated by the
// config drawer, so a connector nobody wrote a form for renders a complete and
// correct one, with no frontend change at all.
//
// It panics on a malformed declaration; see mergedParams for why that is the
// right severity.
func (c Connector) Descriptor() nodes.Descriptor {
	params, err := c.mergedParams()
	if err != nil {
		panic("connectors: " + err.Error())
	}
	return nodes.Descriptor{
		Type: c.ID,
		Name: c.Name,
		// Connectors would read better in an "Apps" group of their own, but the
		// palette's category rail is a fixed list in the frontend, and this phase
		// promises to need no frontend change. Core keeps them visible and
		// filterable today.
		Category:    nodes.CategoryCore,
		Description: c.Description,
		Icon:        c.Icon,
		Mode:        nodes.ModePerItem,
		Inputs:      nodes.MainIn,
		Outputs:     nodes.MainOut,
		Params:      params,
		Credential:  c.Credential,
	}
}

// mergedParams builds the parameter list. Two operations may share a parameter —
// "channel" belongs to both posting and reading — in which case it appears once,
// gated on both operations. Sharing a *name* while differing in any other way is
// refused: the drawer would show one field whose type or label depended on which
// operation happened to be selected, and the resulting form would be wrong in a
// way nobody would trace back to the declaration.
//
// That refusal is a programming error, never user input, so the caller panics on
// it and Registry construction forces the check at start-up rather than at the
// first execution of an operation nobody happened to test.
func (c Connector) mergedParams() ([]nodes.ParamSpec, error) {
	options := make([]nodes.ParamOption, 0, len(c.Operations))
	for _, op := range c.Operations {
		options = append(options, nodes.ParamOption{Label: op.Name, Value: op.ID})
	}

	defaultOp := ""
	if len(c.Operations) > 0 {
		defaultOp = c.Operations[0].ID
	}
	out := []nodes.ParamSpec{{
		Name:        nodes.ConnectorOperationParam,
		Label:       "Operation",
		Type:        nodes.ParamSelect,
		Default:     defaultOp,
		Required:    true,
		Options:     options,
		Description: "Which " + c.Name + " call this node makes. The fields below follow it.",
	}}

	// index maps a parameter name to its position in out, so a repeat is merged
	// into the spec already there instead of appended a second time.
	index := make(map[string]int, 8)
	owner := make(map[string]string, 8)
	for _, op := range c.Operations {
		for _, spec := range op.Params {
			if spec.Name == nodes.ConnectorOperationParam {
				return nil, fmt.Errorf(
					"connector %q, operation %q: a parameter may not be called %q, which is the operation select itself",
					c.ID, op.ID, nodes.ConnectorOperationParam)
			}
			at, seen := index[spec.Name]
			if !seen {
				gated := spec
				gated.ShowWhen = &nodes.ShowWhen{
					Param:  nodes.ConnectorOperationParam,
					Equals: []any{op.ID},
				}
				index[spec.Name] = len(out)
				owner[spec.Name] = op.ID
				out = append(out, gated)
				continue
			}
			if diff := specDiff(out[at], spec); len(diff) > 0 {
				return nil, fmt.Errorf(
					"connector %q declares parameter %q differently in operations %q and %q (differing: %s); "+
						"two operations may share a parameter only when its whole spec is identical",
					c.ID, spec.Name, owner[spec.Name], op.ID, strings.Join(diff, ", "))
			}
			out[at].ShowWhen.Equals = append(out[at].ShowWhen.Equals, op.ID)
		}
	}
	return out, nil
}

// specDiff names the fields in which two specs for the same parameter disagree,
// ignoring ShowWhen, which this package owns. Naming them turns a panic into
// something the connector author can act on without reading this file.
func specDiff(a, b nodes.ParamSpec) []string {
	a.ShowWhen, b.ShowWhen = nil, nil
	if reflect.DeepEqual(a, b) {
		return nil
	}
	var diff []string
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	for i := 0; i < va.NumField(); i++ {
		if !reflect.DeepEqual(va.Field(i).Interface(), vb.Field(i).Interface()) {
			diff = append(diff, va.Type().Field(i).Name)
		}
	}
	return diff
}

/* --- validation ----------------------------------------------------------- */

// Validate checks a declaration for the mistakes that would otherwise surface as
// a puzzling request at run time: a placeholder naming a parameter that does not
// exist, a body on a method that carries none, a duplicated operation id.
func (c Connector) Validate() error {
	switch {
	case strings.TrimSpace(c.ID) == "":
		return fmt.Errorf("a connector needs an ID")
	case strings.TrimSpace(c.Name) == "":
		return fmt.Errorf("connector %q needs a Name", c.ID)
	case strings.TrimSpace(c.BaseURL) == "":
		return fmt.Errorf("connector %q needs a BaseURL", c.ID)
	case len(c.Operations) == 0:
		return fmt.Errorf("connector %q declares no operations", c.ID)
	case strings.HasSuffix(c.BaseURL, "/"):
		return fmt.Errorf("connector %q: BaseURL %q must not end in a slash, because every Path starts with one",
			c.ID, c.BaseURL)
	case c.ErrorField != "" && c.OKField == "":
		return fmt.Errorf("connector %q sets ErrorField without OKField, so nothing would ever read it", c.ID)
	}

	seen := make(map[string]bool, len(c.Operations))
	for _, op := range c.Operations {
		switch {
		case strings.TrimSpace(op.ID) == "":
			return fmt.Errorf("connector %q has an operation with no ID", c.ID)
		case seen[op.ID]:
			return fmt.Errorf("connector %q declares operation %q twice", c.ID, op.ID)
		case strings.TrimSpace(op.Name) == "":
			return fmt.Errorf("connector %q, operation %q needs a Name", c.ID, op.ID)
		case strings.TrimSpace(op.Method) == "":
			return fmt.Errorf("connector %q, operation %q needs a Method", c.ID, op.ID)
		case !strings.HasPrefix(op.Path, "/"):
			return fmt.Errorf("connector %q, operation %q: Path %q must start with a slash", c.ID, op.ID, op.Path)
		}
		seen[op.ID] = true

		if method := strings.ToUpper(op.Method); op.Body != "" &&
			(method == http.MethodGet || method == http.MethodHead) {
			return fmt.Errorf("connector %q, operation %q declares a Body but is a %s, which carries none",
				c.ID, op.ID, method)
		}

		declared := make(map[string]bool, len(op.Params))
		for _, spec := range op.Params {
			if spec.Name == "" {
				return fmt.Errorf("connector %q, operation %q has a parameter with no Name", c.ID, op.ID)
			}
			if declared[spec.Name] {
				return fmt.Errorf("connector %q, operation %q declares parameter %q twice", c.ID, op.ID, spec.Name)
			}
			declared[spec.Name] = true
		}

		templates := map[string]string{"Path": op.Path, "Body": op.Body}
		for key, value := range op.Query {
			templates["query parameter "+key] = value
		}
		for where, tmpl := range templates {
			for _, ref := range nodes.ConnectorPlaceholders(tmpl) {
				if !declared[ref] {
					return fmt.Errorf("connector %q, operation %q: %s references {%s}, which the operation does not declare",
						c.ID, op.ID, where, ref)
				}
			}
		}
	}

	_, err := c.mergedParams()
	return err
}

/* --- the registry --------------------------------------------------------- */

// Registry holds the connectors a build knows about, in declaration order so
// the palette does not shuffle between requests.
type Registry struct {
	byID  map[string]Connector
	order []string
}

// NewRegistry builds a registry, refusing a malformed connector. A declaration
// is written by a developer and read at start-up, so a mistake in one is a build
// error in every sense but the compiler's — panicking here is how it behaves
// like one, at the moment the process starts rather than at 3am when somebody
// finally selects that operation.
func NewRegistry(cs ...Connector) *Registry {
	r := &Registry{byID: make(map[string]Connector, len(cs))}
	for _, c := range cs {
		if err := c.Validate(); err != nil {
			panic("connectors: " + err.Error())
		}
		if _, seen := r.byID[c.ID]; !seen {
			r.order = append(r.order, c.ID)
		}
		r.byID[c.ID] = c
	}
	return r
}

// Default returns every built-in connector.
func Default() *Registry {
	return NewRegistry(
		Slack(),
		Gmail(),
		GoogleSheets(),
	)
}

// Get looks a connector up by id.
func (r *Registry) Get(id string) (Connector, bool) {
	if r == nil {
		return Connector{}, false
	}
	c, ok := r.byID[id]
	return c, ok
}

// All returns every connector in declaration order.
func (r *Registry) All() []Connector {
	if r == nil {
		return nil
	}
	out := make([]Connector, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.byID[id])
	}
	return out
}

// Nodes wraps every connector as an executable node.
func (r *Registry) Nodes() []nodes.Node {
	all := r.All()
	out := make([]nodes.Node, 0, len(all))
	for _, c := range all {
		out = append(out, c.Node())
	}
	return out
}

// Register adds every built-in connector to a node registry and returns it, so
// wiring the apps in is one line at the composition root:
//
//	Registry: connectors.Register(nodes.Default())
//
// It lives here rather than inside nodes.Default() because a connector
// declaration is written in terms of nodes.ParamSpec: connectors imports nodes,
// so nodes cannot import connectors back.
func Register(reg *nodes.Registry) *nodes.Registry {
	for _, n := range Default().Nodes() {
		reg.Register(n)
	}
	return reg
}
