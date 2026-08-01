package adapters

import (
	"fmt"
	"strings"
)

type Registry struct {
	adapters map[string]Adapter
}

func NewRegistry(adapters ...Adapter) *Registry {
	r := &Registry{adapters: make(map[string]Adapter)}
	for _, adapter := range adapters {
		r.Register(adapter)
	}
	return r
}

func DefaultRegistry() *Registry {
	return NewRegistry(NewOpenAICompatible(), NewAnthropicNative(), NewAzureOpenAI())
}

func (r *Registry) Register(adapter Adapter) {
	if adapter == nil {
		return
	}
	r.adapters[adapter.Type()] = adapter
}

func (r *Registry) Get(adapterType string) (Adapter, error) {
	adapterType = NormalizeType(adapterType)
	adapter, ok := r.adapters[adapterType]
	if !ok {
		return nil, fmt.Errorf("unknown adapter_type %q", adapterType)
	}
	return adapter, nil
}

func (r *Registry) Has(adapterType string) bool {
	_, err := r.Get(adapterType)
	return err == nil
}

func (r *Registry) ValidateConfig(adapterType string, raw []byte) error {
	adapter, err := r.Get(adapterType)
	if err != nil {
		return err
	}
	validator, ok := adapter.(ConfigValidator)
	if !ok {
		return nil
	}
	return validator.ValidateConfig(raw)
}

func NormalizeType(adapterType string) string {
	adapterType = strings.TrimSpace(adapterType)
	if adapterType == "" {
		return TypeOpenAICompatible
	}
	return adapterType
}
