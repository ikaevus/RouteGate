package delivery

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type ProviderCapabilities struct {
	HTML             bool
	Attachments      bool
	DeliveryReceipts bool
}

type ProviderInfo struct {
	Name               string
	Channel            string
	Configured         bool
	ConfigurationError string
	Capabilities       ProviderCapabilities
	Source             string
	SecretConfigured   bool
}

type configurableProvider interface {
	Configured() bool
	ConfigurationErrorCode() string
}

type capableProvider interface {
	Capabilities() ProviderCapabilities
}

type testableProvider interface {
	Test(context.Context) ProviderResult
}

type providerResolver interface {
	Resolve(context.Context, string) (Provider, bool, error)
	List(context.Context) ([]ProviderInfo, error)
}

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
	if r.providers == nil {
		r.providers = make(map[string]Provider)
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

func (r *Registry) Resolve(_ context.Context, name string) (Provider, bool, error) {
	provider, ok := r.Get(name)
	return provider, ok, nil
}

func (r *Registry) List(_ context.Context) ([]ProviderInfo, error) {
	return r.Info(), nil
}

func (r *Registry) Info() []ProviderInfo {
	if r == nil {
		return []ProviderInfo{}
	}
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)

	items := make([]ProviderInfo, 0, len(names))
	for _, name := range names {
		provider := r.providers[name]
		item := ProviderInfo{
			Name:       name,
			Channel:    strings.ToLower(strings.TrimSpace(provider.Channel())),
			Configured: true,
			Source:     "static",
		}
		if configurable, ok := provider.(configurableProvider); ok {
			item.Configured = configurable.Configured()
			if !item.Configured {
				item.ConfigurationError = normalizeSafeCode(configurable.ConfigurationErrorCode())
			}
		}
		if capable, ok := provider.(capableProvider); ok {
			item.Capabilities = capable.Capabilities()
		}
		items = append(items, item)
	}
	return items
}
