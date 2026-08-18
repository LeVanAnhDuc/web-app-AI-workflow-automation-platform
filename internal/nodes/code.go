package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"

	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/domain"
)

// defaultJSCode is what a freshly dropped Code node contains: it runs as-is and
// shows the item shape at the same time, so the user edits rather than starts.
const defaultJSCode = `// items is [{ json: { ... } }]. Return an array in the same shape.
return items.map(function (item) { return { json: item.json }; });`

// Code runs user JavaScript. Phase 1 runs it in-process under goja with no host
// bindings and a wall-clock interrupt: accepted debt for an operator-run
// deployment, to be replaced by container or WASM isolation before the platform
// is multi-tenant.
type Code struct{}

// Descriptor implements Node.
func (Code) Descriptor() Descriptor {
	return Descriptor{
		Type:        "code",
		Name:        "Code",
		Category:    CategoryCore,
		Description: "Transforms the items with a snippet of JavaScript.",
		Icon:        "code",
		Mode:        ModeOnce,
		Inputs:      MainIn,
		Outputs:     MainOut,
		Params: []ParamSpec{
			{
				Name:     "jsCode",
				Label:    "JavaScript",
				Type:     ParamCode,
				Default:  defaultJSCode,
				Required: true,
				Description: "Return an array of objects, an array of { json: ... } wrappers, " +
					"or a single object. console.log is available for debugging.",
				// Expressions are off by design: {{ }} is valid JavaScript in a
				// template literal, so interpolating the body would corrupt real
				// code. The snippet reaches the items through `items` instead.
				SupportsExpression: false,
			},
			{
				Name:    "mode",
				Label:   "Run",
				Type:    ParamSelect,
				Default: "allItems",
				Options: []ParamOption{
					{Label: "Once for all items", Value: "allItems"},
					{Label: "Once for each item", Value: "eachItem"},
				},
				Description: "All items gives the snippet `items`; each item gives it " +
					"`item` and `index` and runs it once per input item.",
			},
		},
	}
}

// Execute compiles and runs the snippet, then normalises whatever it returned
// into items.
func (Code) Execute(ec ExecContext) (Result, error) {
	source := stringify(ec.Params.Literal("jsCode"))
	if strings.TrimSpace(source) == "" {
		return Result{}, ErrRequiredParam("jsCode")
	}
	mode := ec.Params.StringOr("mode", "allItems")
	if mode != "allItems" && mode != "eachItem" {
		return Result{}, domain.Errorf(domain.ErrCodeValidation, "unknown code mode %q", mode)
	}

	vm := goja.New()
	// The snippet is wrapped in a function so a top-level `return` works, which
	// is the shape every example and every user expectation assumes.
	program, err := goja.Compile("code", "(function(){\n"+source+"\n})()", false)
	if err != nil {
		return Result{}, scriptError(err)
	}

	var logged []string
	if err := installConsole(vm, &logged); err != nil {
		return Result{}, scriptError(err)
	}
	if err := vm.Set("items", jsItems(ec.Items)); err != nil {
		return Result{}, scriptError(err)
	}

	// One interrupt window covers the whole node, eachItem loop included, so the
	// node's timeout means what it says however many items arrive.
	timeout := time.Duration(ec.Node.Settings.WithDefaults().TimeoutMs) * time.Millisecond
	stop := armInterrupt(execCtx(ec), vm, timeout)
	defer vm.ClearInterrupt()
	defer stop()

	var out []domain.Item
	if mode == "eachItem" {
		for i, item := range ec.Items {
			if err := vm.Set("item", map[string]any{"json": item.JSON}); err != nil {
				return Result{}, scriptError(err)
			}
			if err := vm.Set("index", i); err != nil {
				return Result{}, scriptError(err)
			}
			produced, err := runProgram(vm, program)
			if err != nil {
				return Result{}, err
			}
			out = append(out, produced...)
		}
	} else {
		produced, err := runProgram(vm, program)
		if err != nil {
			return Result{}, err
		}
		out = produced
	}

	// Phase 1 has nowhere in the UI to show console output yet, so it goes to the
	// node logger. The capture is in place for the log panel to read later.
	if len(logged) > 0 {
		ec.Log().Debug("code node console output", "node", ec.Node.Name, "lines", logged)
	}
	return MainSlice(out), nil
}

// runProgram executes one pass of the snippet and converts its return value.
func runProgram(vm *goja.Runtime, program *goja.Program) ([]domain.Item, error) {
	value, err := vm.RunProgram(program)
	if err != nil {
		return nil, scriptError(err)
	}
	return scriptItems(value)
}

// jsItems exposes the input as the array of { json: ... } wrappers the snippet
// receives, matching the shape it is expected to return.
func jsItems(items []domain.Item) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{"json": item.JSON})
	}
	return out
}

// installConsole gives the snippet a console.log that appends to a captured
// slice instead of writing anywhere. Nothing else about the host is reachable.
func installConsole(vm *goja.Runtime, sink *[]string) error {
	console := vm.NewObject()
	log := func(call goja.FunctionCall) goja.Value {
		parts := make([]string, 0, len(call.Arguments))
		for _, arg := range call.Arguments {
			parts = append(parts, stringify(arg.Export()))
		}
		*sink = append(*sink, strings.Join(parts, " "))
		return goja.Undefined()
	}
	for _, name := range []string{"log", "info", "warn", "error", "debug"} {
		if err := console.Set(name, log); err != nil {
			return err
		}
	}
	return vm.Set("console", console)
}

// armInterrupt starts the watchdog that stops a runaway snippet. goja has no
// instruction budget, so a wall-clock interrupt from another goroutine is the
// only way out of `while (true) {}`. The returned function stops the watchdog.
func armInterrupt(ctx context.Context, vm *goja.Runtime, timeout time.Duration) func() {
	done := make(chan struct{})
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			vm.Interrupt(fmt.Sprintf("the script did not finish within its %s timeout", timeout))
		case <-ctx.Done():
			vm.Interrupt(fmt.Sprintf("the execution was stopped: %v", ctx.Err()))
		}
	}()
	return func() { close(done) }
}

// scriptItems normalises the three return shapes a snippet may use into items.
// Everything goes through JSON first: it forces goja's numbers into the same
// float64 the rest of the platform sees after a database round trip, and it is
// what rejects a returned function or symbol with a message the user can act on.
func scriptItems(value goja.Value) ([]domain.Item, error) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, errScriptShape("the script returned nothing")
	}
	encoded, err := json.Marshal(value.Export())
	if err != nil {
		return nil, errScriptShape(fmt.Sprintf("the script returned a value that is not data (%v)", err))
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, errScriptShape(fmt.Sprintf("the returned value could not be read back as JSON (%v)", err))
	}

	switch shape := decoded.(type) {
	case []any:
		out := make([]domain.Item, 0, len(shape))
		for i, element := range shape {
			fields, ok := element.(map[string]any)
			if !ok {
				return nil, errScriptShape(fmt.Sprintf("element %d of the returned array is a %s, not an object",
					i, jsTypeName(element)))
			}
			out = append(out, domain.NewItem(unwrapJSONField(fields)))
		}
		return out, nil
	case map[string]any:
		return []domain.Item{domain.NewItem(unwrapJSONField(shape))}, nil
	default:
		return nil, errScriptShape(fmt.Sprintf("the script returned a %s", jsTypeName(decoded)))
	}
}

// unwrapJSONField accepts both accepted item shapes. An object whose only key is
// "json" is the wrapper form; anything else is taken as the item's own fields,
// so a payload that happens to contain a "json" field is not mangled.
func unwrapJSONField(fields map[string]any) map[string]any {
	if len(fields) == 1 {
		if inner, ok := fields["json"].(map[string]any); ok {
			return inner
		}
	}
	return fields
}

// jsTypeName names a decoded value the way the user would in JavaScript.
func jsTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

// errScriptShape states what was returned and what was expected, because the
// shape contract is the mistake users make most in this node.
func errScriptShape(what string) error {
	return &domain.NodeError{
		Code: domain.ErrCodeScript,
		Message: what + "; return an array of objects, an array of { json: ... } " +
			"wrappers, or a single object",
	}
}

// scriptError turns a goja failure into the persisted node error shape.
func scriptError(err error) error {
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		return &domain.NodeError{Code: domain.ErrCodeScript, Message: stringify(interrupted.Value())}
	}
	var thrown *goja.Exception
	if errors.As(err, &thrown) {
		return &domain.NodeError{Code: domain.ErrCodeScript, Message: thrown.String()}
	}
	return &domain.NodeError{Code: domain.ErrCodeScript, Message: err.Error()}
}
