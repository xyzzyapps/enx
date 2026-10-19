package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestLoadWithoutImports(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "main.enx", "x = 1\n")
	src, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(src.Bytes()) != "x = 1\n" {
		t.Fatalf("source = %q", src.Bytes())
	}
	if src.Name != path {
		t.Fatalf("name = %q, want %q", src.Name, path)
	}
}

func TestLoadImportsDepthFirstAndDeduplicated(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "c.enx", "c\n")
	writeFile(t, dir, "a.enx", "---\nimports: c.enx\n---\na\n")
	writeFile(t, dir, "b.enx", "b\n")
	path := writeFile(t, dir, "main.enx", "---\nimports:\n  - a.enx\n  - b.enx\n---\nmain\n")

	src, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := "c\na\nb\nmain\n"
	if string(src.Bytes()) != want {
		t.Fatalf("source = %q, want %q", src.Bytes(), want)
	}
}

func TestLoadRecursiveFromSubdirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "lib/inner.enx", "inner\n")
	writeFile(t, dir, "lib/helper.enx", "---\nimports: inner.enx\n---\nhelper\n")
	path := writeFile(t, dir, "main.enx", "---\nimports: lib/helper.enx\n---\nmain\n")

	src, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := "inner\nhelper\nmain\n"
	if string(src.Bytes()) != want {
		t.Fatalf("source = %q, want %q", src.Bytes(), want)
	}
}

func TestLoadCycle(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.enx", "---\nimports: b.enx\n---\na\n")
	writeFile(t, dir, "b.enx", "---\nimports: a.enx\n---\nb\n")

	if _, err := Load(a); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("err = %v, want an import-cycle error", err)
	}
}

func TestLoadMissingImport(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "main.enx", "---\nimports: missing.enx\n---\nmain\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected a missing-import error")
	}
}
