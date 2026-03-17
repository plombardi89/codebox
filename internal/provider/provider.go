package provider

import (
	"context"
	"fmt"

	"github.com/plombardi89/codebox/internal/state"
)

// Provider defines the interface that cloud providers must implement to manage
// remote box lifecycles.
type Provider interface {
	Name() string
	Up(ctx context.Context, st *state.BoxState, pubKey string, opts map[string]string) (*state.BoxState, error)
	Down(ctx context.Context, st *state.BoxState) (*state.BoxState, error)
	Delete(ctx context.Context, st *state.BoxState) error
	Status(ctx context.Context, st *state.BoxState) (*state.BoxState, error)
}

var registry = map[string]Provider{}

// Register adds a provider to the global registry. It is typically called from
// an init() function in the provider's package.
func Register(name string, p Provider) {
	registry[name] = p
}

// Get returns a registered provider by name, or an error if no provider with
// that name has been registered.
func Get(name string) (Provider, error) {
	p, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
	return p, nil
}
