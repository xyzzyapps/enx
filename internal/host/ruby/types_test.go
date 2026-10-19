package ruby

import (
	"testing"

	"enx/internal/core"
	"enx/internal/host"
)

func lexForTypes(t *testing.T, text string) []host.Token {
	t.Helper()
	src := core.NewSource("types.rb", []byte(text))
	toks, diags := New().Lex(src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	return toks
}

func tokenIndex(toks []host.Token, from int, text string) int {
	for i := from; i < len(toks); i++ {
		if toks[i].Text == text {
			return i
		}
	}
	return -1
}

func TestAssignedTypeReadsWrittenValues(t *testing.T) {
	cases := []struct {
		text string
		typ  string
		ok   bool
	}{
		{"x = 1\n", "Integer", true},
		{"x = 1.5\n", "Float", true},
		{"x = \"s\"\n", "String", true},
		{"x = [1, 2]\n", "Array", true},
		{"x = %w[a b]\n", "Array", true},
		{"x = %i[a b]\n", "Array", true},
		{"x = %q{literal}\n", "String", true},
		{"x = { a: 1, b: 2 }\n", "Hash", true},
		{"x = Point.new(1, 2)\n", "Point", true},
		{"x = nil\n", "NilClass", true},
		{"x = true\n", "TrueClass", true},
		{"x = false\n", "FalseClass", true},
		{"x = 1 + 2\n", "", false},
		{"x = [1, 2].first\n", "", false},
		{"x = Point.new.foo\n", "", false},
		{"x = 1, 2\n", "", false},
		{"a, b = 1, \"x\"\n", "", false},
		{"x = Point.new, 2\n", "", false},
	}
	for _, tc := range cases {
		toks := lexForTypes(t, tc.text)
		name := tokenIndex(toks, 0, "x")
		if name < 0 {
			name = tokenIndex(toks, 0, "a")
		}
		eq := tokenIndex(toks, name+1, "=")
		if name < 0 || eq < 0 {
			t.Fatalf("%q: could not find the target", tc.text)
		}
		got, _, ok := NewTypes().AssignedType(toks, name, eq)
		if ok != tc.ok || got != tc.typ {
			t.Errorf("%q: got (%q, %v), want (%q, %v)", tc.text, got, ok, tc.typ, tc.ok)
		}
	}
}
