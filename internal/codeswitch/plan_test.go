package codeswitch

import (
	"strings"
	"testing"

	"enx/internal/core"
	"enx/internal/host"
	"enx/internal/host/ruby"
)

func planFor(t *testing.T, text string, lex *Lexicon) (*Plan, []host.Token, []Label) {
	t.Helper()
	src := core.NewSource("test.rb", []byte(text))
	toks, _ := ruby.New().Lex(src)
	lab := ClassifyLexical(lex, toks)
	plan := &Plan{Source: src, Runs: Split(toks, lab.Labels(), LexicalSeparator{})}
	return plan, toks, lab.Labels()
}

// TestIdentityWhenNothingIsEnglish is the emitter's central guarantee: the tool
// never rewrites code. With an empty lexicon no token can be English, so the
// output must equal the input byte for byte.
func TestIdentityWhenNothingIsEnglish(t *testing.T) {
	inputs := []string{
		"",
		"x = 5\n",
		"sum = lambda { |a, b| a + b }\n",
		"module Shop\n  TAX_RATE = 0.08\nend\n",
		"sql = <<~SQL\n  select 1\nSQL\n",
		"# comment only\n",
		"r = /ab+c/i\nq = a / b\n",
		"w = %w[a b]\n",
	}
	empty := NewLexicon(nil)
	for _, in := range inputs {
		plan, _, _ := planFor(t, in, empty)
		if got := plan.Text(); got != in {
			t.Errorf("identity failed for %q:\n got %q", in, got)
		}
		if got := plan.CodeText(); got != in {
			t.Errorf("code text failed for %q:\n got %q", in, got)
		}
	}
}

// TestCommentsSurviveProseRemoval pins the promise that trivia is always
// preserved. A comment is trivia, but it is also content: it must not be
// absorbed into an English region and deleted with the prose around it.
func TestCommentsSurviveProseRemoval(t *testing.T) {
	lex := NewLexicon(map[string]float64{
		"we": 100, "compute": 100, "more": 100, "prose": 100,
	})
	cases := []string{
		"we compute\n# keep me\nmore prose\nx = 1\n# and me\ny = 2\n",
		"we compute # keep me\nx = 1\n",
		"# before\nwe compute\nx = 1\n",
	}
	for _, in := range cases {
		plan, _, _ := planFor(t, in, lex)
		got := plan.Text()
		if !strings.Contains(got, "# keep me") && !strings.Contains(got, "# before") {
			t.Errorf("input %q: comments were deleted: %q", in, got)
		}
		if strings.Contains(got, "compute") || strings.Contains(got, "more prose") {
			t.Errorf("input %q: English survived: %q", in, got)
		}
		if !strings.Contains(got, "x = 1") {
			t.Errorf("input %q: code was lost: %q", in, got)
		}
	}
}

// TestMachineSeparatorPreservesExistingLineBreaks pins the rule that a region
// which already spanned a line break keeps one, whether or not the grammar
// recorded a statement boundary before it. That is what protects a bare
// "return" followed by prose on the next line.
func TestMachineSeparatorPreservesExistingLineBreaks(t *testing.T) {
	sep := MachineSeparator{}
	if got := sep.Replacement(" words\nmore prose\n", 3, 4); got != "\n" {
		t.Fatalf("a region with a line break became %q, want a newline", got)
	}
	if got := sep.Replacement(" words ", 3, 4); got != " " {
		t.Fatalf("a same-line region became %q, want a space", got)
	}
	sepWithBoundary := MachineSeparator{sepAfter: map[int]bool{3: true}}
	if got := sepWithBoundary.Replacement(" words ", 3, 4); got != "\n" {
		t.Fatalf("a statement boundary became %q, want a newline", got)
	}
}

// TestMachineSeparatorUsesNextCodeIndent checks the seam formatter: when a
// newline is inserted, the next code token's own line indentation wins over the
// indentation of the prose line that was deleted.
func TestMachineSeparatorUsesNextCodeIndent(t *testing.T) {
	src := core.NewSource("t.rb", []byte("we compute\n    x = 1\n"))
	toks, _ := ruby.New().Lex(src)
	x := -1
	for i, tok := range toks {
		if tok.Text == "x" {
			x = i
			break
		}
	}
	if x < 0 {
		t.Fatal("no x token")
	}
	sep := NewMachineSeparator(&host.Path{SeparatorAfter: map[int]bool{0: true}}, src, toks)
	if got := sep.Replacement(" words\n  ", 0, x); got != "\n    " {
		t.Fatalf("replacement = %q, want %q", got, "\n    ")
	}
}

// TestRunsCoverTheFile checks that the plan is a partition of the input, so
// concatenating verbatim runs plus replacements loses nothing.
func TestRunsCoverTheFile(t *testing.T) {
	inputs := []string{
		"x = 5\n",
		"we compute the sum\nx = 5\n",
		"a = 1 and the total is b = 2\n",
		"# only a comment\n",
		"",
	}
	lex := NewLexicon(map[string]float64{"we": 100, "compute": 100, "the": 100, "sum": 100, "and": 50, "total": 50, "is": 50})
	for _, in := range inputs {
		plan, toks, _ := planFor(t, in, lex)
		if len(toks) == 0 {
			continue
		}
		pos := 0
		for i, r := range plan.Runs {
			if r.Span.Start != pos {
				t.Fatalf("input %q: run %d starts at %d, want %d", in, i, r.Span.Start, pos)
			}
			if r.Span.End < r.Span.Start {
				t.Fatalf("input %q: run %d has inverted span %v", in, i, r.Span)
			}
			pos = r.Span.End
		}
		if pos != len(in) {
			t.Fatalf("input %q: runs cover %d of %d bytes", in, pos, len(in))
		}
	}
}

// TestCodeTextIsASubsequence proves the emitted code is always a subsequence
// of the input: nothing is reordered, invented, or substituted.
func TestCodeTextIsASubsequence(t *testing.T) {
	lex := NewLexicon(map[string]float64{"we": 100, "compute": 100, "the": 100, "sum": 100})
	in := "x = 5\nwe compute the sum\ny = x + 1\n"
	plan, _, _ := planFor(t, in, lex)

	code := plan.CodeText()
	if !isSubsequence(code, in) {
		t.Fatalf("code text %q is not a subsequence of %q", code, in)
	}
	if strings.Contains(code, "compute") {
		t.Fatalf("English survived into the output: %q", code)
	}
	if !strings.Contains(code, "x = 5") || !strings.Contains(code, "y = x + 1") {
		t.Fatalf("code was lost: %q", code)
	}
}

func TestEnglishOnItsOwnLineDisappears(t *testing.T) {
	lex := NewLexicon(map[string]float64{"we": 100, "compute": 100, "the": 100, "sum": 100})
	in := "x = 5\nwe compute the sum\n"
	plan, _, _ := planFor(t, in, lex)
	got := plan.Text()
	if strings.Contains(got, "compute") {
		t.Fatalf("output still contains English: %q", got)
	}
	if !strings.Contains(got, "x = 5") {
		t.Fatalf("output lost the code: %q", got)
	}
}

func TestKeepProseCommentsOutEnglish(t *testing.T) {
	lex := NewLexicon(map[string]float64{"we": 100, "compute": 100, "the": 100, "sum": 100})
	in := "x = 5\nwe compute the sum\ny = 6\n"
	plan, _, _ := planFor(t, in, lex)
	got := plan.TextWithProse("#")
	if !strings.Contains(got, "# we compute the sum") {
		t.Fatalf("prose was not commented out: %q", got)
	}
	if !strings.Contains(got, "x = 5") || !strings.Contains(got, "y = 6") {
		t.Fatalf("code was lost: %q", got)
	}
}

// isSubsequence reports whether sub can be obtained by deleting bytes from
// full.
func isSubsequence(sub, full string) bool {
	i := 0
	for j := 0; j < len(full) && i < len(sub); j++ {
		if full[j] == sub[i] {
			i++
		}
	}
	return i == len(sub)
}
