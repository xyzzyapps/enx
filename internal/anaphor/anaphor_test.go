package anaphor

import (
	"testing"

	"enx/internal/core"
	"enx/internal/host"
	"enx/internal/host/ruby"
)

func lex(t *testing.T, text string, marker byte) (*core.Source, []host.Token) {
	t.Helper()
	src := core.NewSource("t.rb", []byte(text))
	toks, diags := ruby.New().LexWithAnaphora(src, marker)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	return src, toks
}

// allKept marks every non-trivia token as kept, which isolates the resolver from
// the labelling.
func allKept(toks []host.Token) []bool {
	kept := make([]bool, len(toks))
	for i, tok := range toks {
		kept[i] = !tok.IsTrivia()
	}
	return kept
}

func resolve(t *testing.T, text string, marker byte) (*Result, []host.Token) {
	t.Helper()
	_, toks := lex(t, text, marker)
	return Resolve(toks, allKept(toks), nil, nil), toks
}

// resolvedTexts returns "marker=referent" for each anaphor, in source order.
func resolvedTexts(res *Result, toks []host.Token) []string {
	var out []string
	for i, tok := range toks {
		if tok.Kind == host.Anaphor {
			out = append(out, tok.Text+"="+res.Substitute(i))
		}
	}
	return out
}

func wantTexts(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("resolved %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("resolved %v, want %v", got, want)
		}
	}
}

// TestResolvesMostRecentFirst is the whole feature: !-1 is the most recent
// assignment, !-2 the one before it.
func TestResolvesMostRecentFirst(t *testing.T) {
	res, toks := resolve(t, "stock = 10\nmugs = stock - 3\ntotal = !-1 + !-2\n", '!')
	wantTexts(t, resolvedTexts(res, toks), []string{"!-1=mugs", "!-2=stock"})
	if len(res.Unresolved) != 0 {
		t.Fatalf("unexpected unresolved: %v", res.Unresolved)
	}
}

// TestStatementCannotSeeItsOwnTarget is the bug that made !-1 resolve to the name
// being assigned. A binding only becomes visible once its statement ends.
func TestStatementCannotSeeItsOwnTarget(t *testing.T) {
	res, toks := resolve(t, "stock = 10\ntotal = !-1\n", '!')
	wantTexts(t, resolvedTexts(res, toks), []string{"!-1=stock"})
}

// TestOnlyAssignmentsEnterTheHistory is the design decision that keeps the
// referent a bare name.
func TestOnlyAssignmentsEnterTheHistory(t *testing.T) {
	res, toks := resolve(t, "stock = 10\nputs stock\nmugs = 3\ntotal = !-1\n", '!')
	// "puts stock" is not an assignment, so !-1 is mugs, not stock.
	wantTexts(t, resolvedTexts(res, toks), []string{"!-1=mugs"})
}

func TestUnresolvedIsReportedNotGuessed(t *testing.T) {
	res, toks := resolve(t, "stock = 10\ntotal = !-7\n", '!')
	if len(res.Unresolved) != 1 {
		t.Fatalf("unresolved = %v, want one entry", res.Unresolved)
	}
	// The marker is left as written, so the host reports it.
	wantTexts(t, resolvedTexts(res, toks), []string{"!-7=!-7"})
}

func TestPositionZeroNeverResolves(t *testing.T) {
	res, _ := resolve(t, "stock = 10\ntotal = !-0\n", '!')
	if len(res.Unresolved) != 1 {
		t.Fatalf("unresolved = %v, want one entry", res.Unresolved)
	}
}

func TestNoMarkerClaimedMeansNoAnaphora(t *testing.T) {
	_, toks := lex(t, "total = !-1\n", 0)
	for _, tok := range toks {
		if tok.Kind == host.Anaphor {
			t.Fatal("a marker was claimed when none was asked for")
		}
	}
}

// TestConfigurableMarker checks the marker character is the caller's choice.
func TestConfigurableMarker(t *testing.T) {
	res, toks := resolve(t, "stock = 10\ntotal = @-1\n", '@')
	wantTexts(t, resolvedTexts(res, toks), []string{"@-1=stock"})
}

// TestMultipleAssignmentTargetsAreBindings covers "a, b = 1, 2", where both
// names are results.
func TestMultipleAssignmentTargetsAreBindings(t *testing.T) {
	res, toks := resolve(t, "a, b = 1, 2\ntotal = !-1 + !-2\n", '!')
	wantTexts(t, resolvedTexts(res, toks), []string{"!-1=b", "!-2=a"})
}

// TestCompoundAssignmentCounts checks "x += 1" is a result.
func TestCompoundAssignmentCounts(t *testing.T) {
	res, toks := resolve(t, "count = 0\ncount += 1\ntotal = !-1\n", '!')
	wantTexts(t, resolvedTexts(res, toks), []string{"!-1=count"})
}

// TestAttributeAssignmentsAreNotBindings covers "obj.attr = 1": attr is not a
// bare variable name, so an anaphor must not resolve to it.
func TestAttributeAssignmentsAreNotBindings(t *testing.T) {
	res, toks := resolve(t, "x = 1\nobj.attr = 2\ntotal = !-1\n", '!')
	wantTexts(t, resolvedTexts(res, toks), []string{"!-1=x"})
}

// TestCallArgumentListIsNotMultipleAssignment covers "f(a, b = 1)": b is an
// assignment result, but a is merely an argument being read.
func TestCallArgumentListIsNotMultipleAssignment(t *testing.T) {
	res, _ := resolve(t, "f(a, b = 1)\ntotal = !-2\n", '!')
	if len(res.Unresolved) != 1 {
		t.Fatalf("unresolved = %v, want one entry for the call argument", res.Unresolved)
	}
}

// TestCommaListDoesNotCrossANewline checks that a comma at the end of a line
// is prose punctuation, not the start of a multiple assignment on the next.
func TestCommaListDoesNotCrossANewline(t *testing.T) {
	res, _ := resolve(t, "a,\nb = 1\ntotal = !-2\n", '!')
	if len(res.Unresolved) != 1 {
		t.Fatalf("unresolved = %v, want one entry for the comma list", res.Unresolved)
	}
}

// TestCompoundAssignmentDoesNotRetype checks that "x += 1" records a result
// but does not read a type from its right hand side.
func TestCompoundAssignmentDoesNotRetype(t *testing.T) {
	_, toks := lex(t, "x = 1.5\nx += 1\ntotal = !-1:Integer\n", '!')
	res := Resolve(toks, allKept(toks), nil, ruby.NewTypes())
	if len(res.Unresolved) != 1 {
		t.Fatalf("unresolved = %v, want the typed marker to miss", res.Unresolved)
	}
}

// TestScopedTrackerIsolatesBlocks checks that a tracker closing a scope hides
// its bindings from the code after it.
func TestScopedTrackerIsolatesBlocks(t *testing.T) {
	_, toks := lex(t, "def f\n  inner = 1\nend\ntotal = !-1\n", '!')
	res := Resolve(toks, allKept(toks), rubyTracker{}, nil)
	if len(res.Unresolved) != 1 {
		t.Fatalf("a binding inside a closed scope was still visible: %+v", res.Resolved)
	}
}

// rubyTracker is a minimal scope tracker for the test: "def" opens, "end"
// closes.
type rubyTracker struct{}

func (rubyTracker) Opens(tok host.Token) bool  { return tok.Text == "def" }
func (rubyTracker) Closes(tok host.Token) bool { return tok.Text == "end" }
