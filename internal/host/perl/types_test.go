package perl

import (
	"testing"

	"enx/internal/core"
	"enx/internal/host"
)

func lexForTypes(t *testing.T, text string) []host.Token {
	t.Helper()
	src := core.NewSource("types.pl", []byte(text))
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

func TestAssignedType(t *testing.T) {
	cases := []struct {
		text string
		name string
		typ  string
		ok   bool
	}{
		{"my $p = Point->new;\n", "$p", "Point", true},
		{"my $p = Point->new(1);\n", "$p", "Point", true},
		{"my $p = Point->new->name;\n", "$p", "scalar", true},
		{"my $p = Point->new + 1;\n", "$p", "scalar", true},
		{"my @list = (1, 2);\n", "@list", "array", true},
		{"my %map = (a => 1);\n", "%map", "hash", true},
	}
	for _, tc := range cases {
		toks := lexForTypes(t, tc.text)
		name := tokenIndex(toks, 0, tc.name)
		eq := tokenIndex(toks, name+1, "=")
		if name < 0 || eq < 0 {
			t.Fatalf("%q: could not find target %q", tc.text, tc.name)
		}
		got, _, ok := NewTypes().AssignedType(toks, name, eq)
		if ok != tc.ok || got != tc.typ {
			t.Errorf("%q: got (%q, %v), want (%q, %v)", tc.text, got, ok, tc.typ, tc.ok)
		}
	}
}
