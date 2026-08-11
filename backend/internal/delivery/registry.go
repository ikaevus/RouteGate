package delivery

import (
	"fmt"
	"strings"
)

type Registry struct {
	providers map[string]Provider
}

func NewRegistry(providers ...Provider) (*Registry, error) {
	registry := &Registry{providers: make(map[string]Provider, len(providers))}
	for _, provider := range providers {
		if err := registry.Register(provider); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Register(provider Provider) error {
	if provider == nil {
		return fmt.Errorf("delivery provider is nil")
	}
	name := strings.ToLower(strings.TrimSpace(provider.Name()))
	channel := strings.ToLower(strings.TrimSpace(provider.Channel()))
	if name == "" || channel == "" {
		return fmt.Errorf("delivery provider name and channel are required")
	}
	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("delivery provider %q is already registered", name)
	}
	r.providers[name] = provider
	return nil
}

func (r *Registry) Get(name string) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	provider, ok := r.providers[strings.ToLower(strings.TrimSpace(name))]
	return provider, ok
}
