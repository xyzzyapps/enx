package c

import (
	"testing"

	"enx/internal/core"
	"enx/internal/host"
)

func lexForTypes(t *testing.T, text string) []host.Token {
	t.Helper()
	src := core.NewSource("types.c", []byte(text))
	toks, diags := New().Lex(src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	return toks
}

// indexOf returns the index of the first token with the given text at or after
// from, or -1.
func indexOf(toks []host.Token, from int, text string) int {
	for i := from; i < len(toks); i++ {
		if toks[i].Text == text {
			return i
		}
	}
	return -1
}

// TestPointerIsPerDeclarator pins the case that a single "*" used to leak
// across a comma: "char *a, b;" declares a pointer and a plain char.
func TestPointerIsPerDeclarator(t *testing.T) {
	toks := lexForTypes(t, "char *a = 0, b = 0;\n")
	types := NewTypes()

	nameA := indexOf(toks, 0, "a")
	nameB := indexOf(toks, nameA+1, "b")
	eqB := indexOf(toks, nameB+1, "=")
	if nameA < 0 || nameB < 0 || eqB < 0 {
		t.Fatalf("could not find a, b, = in %v", toks)
	}

	if name, pointer, ok := types.AssignedType(toks, nameA, -1); !ok || name != "char" || !pointer {
		t.Fatalf("a: got (%q, %v, %v), want (char, true, true)", name, pointer, ok)
	}
	if name, pointer, ok := types.AssignedType(toks, nameB, eqB); !ok || name != "char" || pointer {
		t.Fatalf("b: got (%q, %v, %v), want (char, false, true)", name, pointer, ok)
	}
}

// TestPointerAfterCommaIsStillAPointer is the other side: an explicit "*" on
// the second declarator must still be recognised.
func TestPointerAfterCommaIsStillAPointer(t *testing.T) {
	toks := lexForTypes(t, "char *a = 0, *b = 0;\n")
	types := NewTypes()

	nameB := indexOf(toks, 0, "b")
	eqB := indexOf(toks, nameB+1, "=")
	if nameB < 0 || eqB < 0 {
		t.Fatalf("could not find b and = in %v", toks)
	}
	if name, pointer, ok := types.AssignedType(toks, nameB, eqB); !ok || name != "char" || !pointer {
		t.Fatalf("b: got (%q, %v, %v), want (char, true, true)", name, pointer, ok)
	}
}

// TestBuiltinType covers the ordinary declaration path.
func TestBuiltinType(t *testing.T) {
	toks := lexForTypes(t, "double ratio = 2.5;\n")
	types := NewTypes()

	name := indexOf(toks, 0, "ratio")
	eq := indexOf(toks, name+1, "=")
	if name < 0 || eq < 0 {
		t.Fatalf("could not find ratio and = in %v", toks)
	}
	if got, pointer, ok := types.AssignedType(toks, name, eq); !ok || got != "double" || pointer {
		t.Fatalf("ratio: got (%q, %v, %v), want (double, false, true)", got, pointer, ok)
	}
}
