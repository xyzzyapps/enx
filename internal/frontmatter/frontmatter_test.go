package frontmatter

import (
	"strings"
	"testing"
)

func TestNoFrontmatterIsUnchanged(t *testing.T) {
	data := []byte("x = 1\nputs x\n")
	meta, body, err := Split(data)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(meta.Imports) != 0 || string(body) != string(data) {
		t.Fatalf("got meta=%v body=%q", meta, body)
	}
}

func TestBlockImports(t *testing.T) {
	data := []byte("---\nimports:\n  - helpers.enx\n  - lib/math.enx\n---\nbody\n")
	meta, body, err := Split(data)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	want := []string{"helpers.enx", "lib/math.enx"}
	if strings.Join(meta.Imports, ",") != strings.Join(want, ",") {
		t.Fatalf("imports = %v, want %v", meta.Imports, want)
	}
	if string(body) != "body\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestInlineImports(t *testing.T) {
	data := []byte("---\nimports: [a.enx, \"b, c.enx\"]\n---\n")
	meta, _, err := Split(data)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	want := []string{"a.enx", "b, c.enx"}
	if strings.Join(meta.Imports, "|") != strings.Join(want, "|") {
		t.Fatalf("imports = %v, want %v", meta.Imports, want)
	}
}

func TestScalarImportAndUnknownKeys(t *testing.T) {
	data := []byte("---\ntitle: A note\nimports: helpers.enx\nnested:\n  - ignored\n---\ncode\n")
	meta, body, err := Split(data)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(meta.Imports) != 1 || meta.Imports[0] != "helpers.enx" {
		t.Fatalf("imports = %v", meta.Imports)
	}
	if string(body) != "code\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestCRLFAndComments(t *testing.T) {
	data := []byte("---\r\n# a comment\r\nimports:\r\n  - 'a.enx' # trailing\r\n---\r\nbody\r\n")
	meta, body, err := Split(data)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(meta.Imports) != 1 || meta.Imports[0] != "a.enx" {
		t.Fatalf("imports = %v", meta.Imports)
	}
	if string(body) != "body\r\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestUnterminatedFrontmatter(t *testing.T) {
	if _, _, err := Split([]byte("---\nimports: a.enx\nbody\n")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestMalformedImport(t *testing.T) {
	if _, _, err := Split([]byte("---\nimports: [a.enx\n---\n")); err == nil {
		t.Fatal("expected an error")
	}
}
