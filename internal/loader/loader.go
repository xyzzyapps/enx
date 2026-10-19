// Package loader turns an enx file and its YAML imports into one source.
//
// Imports are textual and depth-first: an imported file's imports are included
// before its body, and a file is included at most once. The main file's name is
// retained on the resulting Source; the imported bodies are prepended before
// it.
package loader

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"enx/internal/core"
	"enx/internal/frontmatter"
)

// Load reads name, resolves its imports recursively, strips every frontmatter
// header, and returns the combined source.
func Load(name string) (*core.Source, error) {
	l := &loader{
		seen:     make(map[string]bool),
		visiting: make(map[string]bool),
	}
	var out bytes.Buffer
	if err := l.load(name, &out); err != nil {
		return nil, err
	}
	return core.NewSource(name, out.Bytes()), nil
}

type loader struct {
	seen     map[string]bool
	visiting map[string]bool
}

func (l *loader) load(name string, out *bytes.Buffer) error {
	abs, err := filepath.Abs(name)
	if err != nil {
		return fmt.Errorf("import %s: %w", name, err)
	}
	if l.visiting[abs] {
		return fmt.Errorf("import cycle at %s", name)
	}
	if l.seen[abs] {
		return nil
	}
	l.visiting[abs] = true
	defer delete(l.visiting, abs)

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("import %s: %w", name, err)
	}
	meta, body, err := frontmatter.Split(data)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	for _, imp := range meta.Imports {
		target := imp
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(abs), target)
		}
		if err := l.load(target, out); err != nil {
			return err
		}
	}

	appendBody(out, body)
	l.seen[abs] = true
	return nil
}
