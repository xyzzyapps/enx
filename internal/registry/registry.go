// Package registry is the service locator for host-language implementations.
//
// The pipeline depends only on this package and on the host interfaces, never
// on a concrete language. Adding a language is therefore a matter of
// registering its spec at startup.
package registry

import (
	"sort"

	"enx/internal/host"
)

// Registry maps language names to host specs.
type Registry struct {
	entries map[string]host.Spec
	order   []string
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{entries: make(map[string]host.Spec)}
}

// Register adds or replaces a spec. Registering without a language name or a
// lexer is a programming error and panics.
func (r *Registry) Register(s host.Spec) {
	if s.Language == "" {
		panic("registry: empty language name")
	}
	if s.Lexer == nil {
		panic("registry: language " + s.Language + " has no lexer")
	}
	if _, seen := r.entries[s.Language]; !seen {
		r.order = append(r.order, s.Language)
	}
	r.entries[s.Language] = s
}

// Lookup returns the spec for lang.
func (r *Registry) Lookup(lang string) (host.Spec, bool) {
	s, ok := r.entries[lang]
	return s, ok
}

// Languages returns the registered language names in registration order.
func (r *Registry) Languages() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// SortedLanguages returns the registered language names in alphabetical order.
func (r *Registry) SortedLanguages() []string {
	out := r.Languages()
	sort.Strings(out)
	return out
}
