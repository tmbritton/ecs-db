package agent

import (
	"fmt"
	"sort"
)

// ParamSchema describes a single parameter that an action or guard accepts.
type ParamSchema struct {
	Name     string
	Type     string // "string", "number", "boolean", etc.
	Required bool
	Default  any
}

// ActionMeta is the metadata stored alongside an ActionHandler in the registry.
// Exposed via Registry.Actions() for tooling such as a visual machine editor.
type ActionMeta struct {
	Name        string
	Description string
	Params      []ParamSchema
}

// GuardMeta is the metadata stored alongside a GuardHandler in the registry.
// Exposed via Registry.Guards() for tooling such as a visual machine editor.
type GuardMeta struct {
	Name        string
	Description string
	Params      []ParamSchema
}

type actionEntry struct {
	meta    ActionMeta
	handler ActionHandler
}

type guardEntry struct {
	meta    GuardMeta
	handler GuardHandler
}

// Registry maps action and guard names to their handlers and metadata.
// Duplicate registration panics — intended to be called from init() so
// misconfiguration is caught at startup, not at dispatch time.
type Registry struct {
	actions map[string]actionEntry
	guards  map[string]guardEntry
}

func NewRegistry() *Registry {
	return &Registry{
		actions: make(map[string]actionEntry),
		guards:  make(map[string]guardEntry),
	}
}

// RegisterAction panics if an action with the same name is already registered.
func (r *Registry) RegisterAction(meta ActionMeta, handler ActionHandler) {
	if _, exists := r.actions[meta.Name]; exists {
		panic(fmt.Sprintf("agent registry: action %q already registered", meta.Name))
	}
	r.actions[meta.Name] = actionEntry{meta: meta, handler: handler}
}

// RegisterGuard panics if a guard with the same name is already registered.
func (r *Registry) RegisterGuard(meta GuardMeta, handler GuardHandler) {
	if _, exists := r.guards[meta.Name]; exists {
		panic(fmt.Sprintf("agent registry: guard %q already registered", meta.Name))
	}
	r.guards[meta.Name] = guardEntry{meta: meta, handler: handler}
}

func (r *Registry) GetAction(name string) (ActionHandler, bool) {
	e, ok := r.actions[name]
	if !ok {
		return nil, false
	}
	return e.handler, true
}

func (r *Registry) GetGuard(name string) (GuardHandler, bool) {
	e, ok := r.guards[name]
	if !ok {
		return nil, false
	}
	return e.handler, true
}

// Actions returns all registered action metadata sorted by name.
func (r *Registry) Actions() []ActionMeta {
	metas := make([]ActionMeta, 0, len(r.actions))
	for _, e := range r.actions {
		metas = append(metas, e.meta)
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Name < metas[j].Name })
	return metas
}

// Guards returns all registered guard metadata sorted by name.
func (r *Registry) Guards() []GuardMeta {
	metas := make([]GuardMeta, 0, len(r.guards))
	for _, e := range r.guards {
		metas = append(metas, e.meta)
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Name < metas[j].Name })
	return metas
}
