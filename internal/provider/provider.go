package provider

import (
	"context"
	"fmt"

	"github.com/plombardi89/codebox/internal/state"
)

// Provider defines the interface that cloud providers must implement to manage
// remote box lifecycles.
type Provider interface {
	Up(ctx context.Context, st *state.Box, pubKey string, opts map[string]string) (*state.Box, error)
	Down(ctx context.Context, st *state.Box) (*state.Box, error)
	Delete(ctx context.Context, st *state.Box) error
	// DestroyVM deletes only the VM (and its boot disk) while leaving other
	// infrastructure (networks, keys, etc.) intact. This allows a subsequent
	// Up() call to recreate just the VM with fresh cloud-init config.
	DestroyVM(ctx context.Context, st *state.Box) error
	Status(ctx context.Context, st *state.Box) (*state.Box, error)
}

// Registry holds named Provider implementations and allows lookup by name.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

// Register adds a provider to the registry.
func (r *Registry) Register(name string, p Provider) {
	r.providers[name] = p
}

// Get returns a registered provider by name, or an error if no provider with
// that name has been registered.
func (r *Registry) Get(name string) (Provider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", name)
	}

	return p, nil
}

// OptOrDefault returns opts[key] if present and non-empty, otherwise def.
func OptOrDefault(opts map[string]string, key, def string) string {
	if v, ok := opts[key]; ok && v != "" {
		return v
	}

	return def
}
