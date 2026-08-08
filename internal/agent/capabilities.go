package agent

import "context"

// Capability is the common audited boundary for GitHub, browser, MCP and CI
// integrations. Adapters must declare whether they can mutate external state.
type Capability interface {
	Name() string
	ReadOnly() bool
	Invoke(context.Context, string, map[string]any) (any, error)
}

type CapabilityRegistry struct{ items map[string]Capability }

func NewCapabilityRegistry() *CapabilityRegistry {
	return &CapabilityRegistry{items: map[string]Capability{}}
}
func (r *CapabilityRegistry) Register(capability Capability) {
	if capability != nil {
		r.items[capability.Name()] = capability
	}
}
func (r *CapabilityRegistry) Get(name string) (Capability, bool) {
	value, ok := r.items[name]
	return value, ok
}
