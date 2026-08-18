package nodes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// paletteOrder is the order GET /api/v1/node-types must return, because the
// frontend renders the palette straight from that list.
var paletteOrder = []string{
	"trigger.manual",
	"trigger.webhook",
	"trigger.schedule",
	"http.request",
	"code",
	"if",
	"set",
	"merge",
	"llm.chat",
	"ai.agent",
}

func TestDefaultRegistryOrder(t *testing.T) {
	registry := Default()
	assert.Equal(t, paletteOrder, registry.Types())

	descriptors := registry.Descriptors()
	require.Len(t, descriptors, len(paletteOrder))
	for i, want := range paletteOrder {
		assert.Equal(t, want, descriptors[i].Type)
	}
}

func TestDefaultRegistryLookup(t *testing.T) {
	registry := Default()
	for _, nodeType := range paletteOrder {
		node, ok := registry.Get(nodeType)
		require.True(t, ok, "%s should be registered", nodeType)
		assert.Equal(t, nodeType, node.Descriptor().Type)
	}
	_, ok := registry.Get("nope")
	assert.False(t, ok)
}

// TestDescriptorsAreRenderable checks the parts of the contract the frontend
// cannot work around: a descriptor with no icon or no output would render as a
// broken palette entry rather than fail a build.
func TestDescriptorsAreRenderable(t *testing.T) {
	for _, d := range Default().Descriptors() {
		t.Run(d.Type, func(t *testing.T) {
			assert.NotEmpty(t, d.Type)
			assert.NotEmpty(t, d.Name)
			assert.NotEmpty(t, d.Category)
			assert.NotEmpty(t, d.Icon)
			assert.NotEmpty(t, d.Description)
			assert.NotEmpty(t, d.Outputs, "every node must produce something")
			assert.Contains(t, []ExecMode{ModeOnce, ModePerItem}, d.Mode)

			if d.IsTrigger {
				assert.Empty(t, d.Inputs, "a trigger starts the graph, so it takes no input")
			} else {
				assert.NotEmpty(t, d.Inputs)
			}

			seen := map[string]bool{}
			for _, p := range d.Params {
				assert.NotEmpty(t, p.Name)
				assert.False(t, seen[p.Name], "duplicate parameter %q", p.Name)
				seen[p.Name] = true
				assert.NotEmpty(t, p.Type)
				if p.Type == ParamSelect {
					assert.NotEmpty(t, p.Options, "a select needs choices to render")
				}
				// A notice is the only parameter allowed to have no label,
				// since it renders as prose rather than as a field.
				if p.Type != ParamNotice {
					assert.NotEmpty(t, p.Label)
				}
				if p.ShowWhen != nil {
					assert.True(t, seen[p.ShowWhen.Param],
						"%q depends on %q, which must be declared before it", p.Name, p.ShowWhen.Param)
					assert.NotEmpty(t, p.ShowWhen.Equals)
				}
			}

			handles := map[string]bool{}
			for _, h := range append(append([]Handle{}, d.Inputs...), d.Outputs...) {
				assert.NotEmpty(t, h.Name)
				assert.NotEmpty(t, h.Label)
			}
			for _, h := range d.Outputs {
				assert.False(t, handles[h.Name], "duplicate output handle %q", h.Name)
				handles[h.Name] = true
			}
		})
	}
}

// TestSelectDefaultsAreOptions catches a default that no longer matches any
// choice, which would leave the config drawer showing an empty select.
func TestSelectDefaultsAreOptions(t *testing.T) {
	for _, d := range Default().Descriptors() {
		for _, p := range d.Params {
			if p.Type != ParamSelect || p.Default == nil {
				continue
			}
			values := make([]string, 0, len(p.Options))
			for _, o := range p.Options {
				values = append(values, o.Value)
			}
			assert.Contains(t, values, p.Default, "%s.%s", d.Type, p.Name)
		}
	}
}
