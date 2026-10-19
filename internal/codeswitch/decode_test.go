package codeswitch_test

import (
	"strings"
	"testing"

	"enx/internal/codeswitch"
	"enx/internal/core"
	"enx/internal/host"
	"enx/internal/host/ruby"
)

func decode(t *testing.T, text string) (*codeswitch.Labelling, *host.Path, string) {
	t.Helper()
	lex, err := codeswitch.LoadLexicon()
	if err != nil {
		t.Fatalf("LoadLexicon: %v", err)
	}
	src := core.NewSource("t.rb", []byte(text))
	toks, _ := ruby.New().Lex(src)
	lab, path, ok := codeswitch.Decode(ruby.NewMachine(), lex, ruby.NewVocabulary(), toks)
	if !ok {
		t.Fatalf("Decode failed for:\n%s", text)
	}
	plan := &codeswitch.Plan{
		Source: src,
		Runs:   codeswitch.Split(toks, lab.Labels(), codeswitch.NewMachineSeparator(path, src, toks)),
	}
	return lab, path, plan.Text()
}

func TestDecodeDropsProseKeepsCode(t *testing.T) {
	text := "we start with x as five, that is x = 5, and y as five too: y = 5\n" +
		"and their sum is a function of two arguments, so sum = lambda { |a, b| a + b }\n" +
		"finally we call sum with x and five and print the answer: answer = sum.call(x, 5)\n" +
		"and then we show it: puts answer\n"

	_, _, got := decode(t, text)
	want := "x = 5\ny = 5\nsum = lambda { |a, b| a + b }\nanswer = sum.call(x, 5)\nputs answer"
	if strings.TrimRight(got, "\n") != want {
		t.Fatalf("emit mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestDecodeKeepsAdjacentCall is the case that needs host knowledge: two
// adjacent lower case words are a call, not two English words, when the first
// is a known Ruby name.
func TestDecodeKeepsAdjacentCall(t *testing.T) {
	_, _, got := decode(t, "puts answer\n")
	if strings.TrimSpace(got) != "puts answer" {
		t.Fatalf("got %q, want %q", got, "puts answer")
	}
}

// TestDecodeKeepsTheAnswerAsProse is the same shape as the previous test but
// with "the", a function word, which makes the run prose instead of a call.
func TestDecodeKeepsTheAnswerAsProse(t *testing.T) {
	_, _, got := decode(t, "we print the answer\n")
	if strings.Contains(got, "print") || strings.Contains(got, "answer") {
		t.Fatalf("prose survived: %q", got)
	}
}

func TestDecodeKeepsConstructorCall(t *testing.T) {
	_, _, got := decode(t, "order = Order.new\n")
	if strings.TrimSpace(got) != "order = Order.new" {
		t.Fatalf("got %q", got)
	}
}

func TestDecodeKeepsMethodChain(t *testing.T) {
	_, _, got := decode(t, "total = order.subtotal + tax\n")
	if strings.TrimSpace(got) != "total = order.subtotal + tax" {
		t.Fatalf("got %q", got)
	}
}

// TestDecodeInsertsNewlineAtStatementBoundary checks the separator: prose
// between two statements on one line must become a statement break, not a
// space, or the result is not valid Ruby.
// TestMultipleAssignmentIsOneStatement pins the case that forced statement
// forms to reject readings the grammar knows are impossible.
//
// "a" on its own is cheaper than the whole of "a, b = 1, 2", so a statement
// chosen purely on its own cost picks the prefix and leaves the rest of the
// line to be skipped. The comma guard is what stops that.
func TestMultipleAssignmentIsOneStatement(t *testing.T) {
	for _, text := range []string{
		"a, b = 1, 2\n",
		"x, y = y, x\n",
		"first, *rest = list\n",
		"h[:key], h[:other] = 1, 2\n",
	} {
		_, _, got := decode(t, text)
		if strings.TrimRight(got, "\n") != strings.TrimRight(text, "\n") {
			t.Errorf("emission differs:\n got %q\nwant %q", got, text)
		}
	}
}

// TestStatementBeforeAProseComma is the other side of that guard: in mixed text
// a comma after code is usually prose punctuation, and the statement before it
// is perfectly good.
func TestStatementBeforeAProseComma(t *testing.T) {
	_, _, got := decode(t, "x = 5, and y as five too: y = 5\n")
	if !strings.Contains(got, "x = 5") || !strings.Contains(got, "y = 5") {
		t.Fatalf("a statement before a prose comma was lost: %q", got)
	}
}

func TestDecodeInsertsNewlineAtStatementBoundary(t *testing.T) {
	_, _, got := decode(t, "x = 5 and then some words here and y = 6\n")
	if !strings.Contains(got, "x = 5") || !strings.Contains(got, "y = 6") {
		t.Fatalf("code lost: %q", got)
	}
	if strings.Contains(got, "x = 5 y = 6") {
		t.Fatalf("statements were joined with a space: %q", got)
	}
}

func TestDecodeSurvivesProseOnlyFile(t *testing.T) {
	lab, _, got := decode(t, "there is nothing to see here at all\n")
	if strings.TrimSpace(got) != "" {
		t.Fatalf("expected empty output, got %q", got)
	}
	code, _, _ := lab.Counts()
	if code != 0 {
		t.Fatalf("expected no code tokens, got %d", code)
	}
}

func TestDecodeIsIdentityForPureRuby(t *testing.T) {
	texts := []string{
		"x = 5\n",
		"def f(a, b)\n  a + b\nend\n",
		"class A\n  def initialize\n    @x = 1\n  end\nend\n",
		"arr.each do |n|\n  puts n\nend\n",
	}
	for _, text := range texts {
		_, _, got := decode(t, text)
		if got != text {
			t.Errorf("identity failed:\n--- got ---\n%q\n--- want ---\n%q", got, text)
		}
	}
}

func TestDecodeNilMachine(t *testing.T) {
	if _, _, ok := codeswitch.Decode(nil, nil, nil, nil); ok {
		t.Fatal("a nil machine must not decode")
	}
}
