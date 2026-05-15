package cloud

import (
	"fmt"
	"sort"
	"sync"
)

// Registry holds all registered cloud adapters.
type Registry interface {
	Register(a Adapter) error
	Get(name string) (Adapter, bool)
	List() []Adapter
}

// NewRegistry returns an empty Registry.
func NewRegistry() Registry {
	return &registry{adapters: map[string]Adapter{}}
}

type registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

func (r *registry) Register(a Adapter) error {
	if a == nil {
		return fmt.Errorf("cloud: Register nil adapter")
	}
	name := a.Name()
	if name == "" {
		return fmt.Errorf("cloud: adapter has empty Name()")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.adapters[name]; exists {
		return fmt.Errorf("cloud: adapter %q already registered", name)
	}
	r.adapters[name] = a
	return nil
}

func (r *registry) Get(name string) (Adapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[name]
	return a, ok
}

func (r *registry) List() []Adapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Adapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}
