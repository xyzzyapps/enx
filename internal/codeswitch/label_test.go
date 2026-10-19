package codeswitch

import (
	"testing"

	"enx/internal/core"
	"enx/internal/host"
	"enx/internal/host/ruby"
)

func lexRuby(t *testing.T, text string) []host.Token {
	t.Helper()
	src := core.NewSource("test.rb", []byte(text))
	toks, diags := ruby.New().Lex(src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	return toks
}

func TestLooksLikeCode(t *testing.T) {
	cases := map[string]bool{
		"x":                        false,
		"sum":                      false,
		"answer":                   false,
		"We":                       false,
		"The":                      false,
		"user_id":                  true,
		"n1":                       true,
		"MAX_SIZE":                 true,
		"CamelCase":                true,
		"ThisIsLongerThanFourteen": true,
		"a":                        false,
		"":                         false,
	}
	for text, want := range cases {
		if got := LooksLikeCode(text); got != want {
			t.Errorf("LooksLikeCode(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestEnglishWordsGetTheEnglishLabel(t *testing.T) {
	lex := NewLexicon(map[string]float64{
		"we": 1000, "compute": 500, "the": 5000, "sum": 800, "is": 4000,
	})
	toks := lexRuby(t, "we compute the sum\n")

	lab := ClassifyLexical(lex, toks)
	for _, d := range lab.Decisions {
		if d.Token.IsTrivia() {
			if d.Label != Trivia {
				t.Errorf("trivia %q got label %v", d.Token.Text, d.Label)
			}
			continue
		}
		if d.Label != English {
			t.Errorf("word %q got label %v (%s), want english", d.Token.Text, d.Label, d.Reason)
		}
	}
}

// TestReceiverAndOperandNeighbours covers two contextual rules that keep code
// from being priced as prose: a word immediately before a member access is a
// receiver, and a word before a binary operator is an operand.
func TestReceiverAndOperandNeighbours(t *testing.T) {
	lex := NewLexicon(nil)
	toks := lexRuby(t, "other.cents + a + b\n")
	costs := NewCosts(toks, lex, ruby.NewVocabulary())

	for i, tok := range toks {
		switch tok.Text {
		case "other":
			if got := costs.Keep(i); got != 0 {
				t.Errorf("receiver %q Keep = %v, want 0", tok.Text, got)
			}
		case "a":
			if got := costs.Keep(i); got != CodeBias {
				t.Errorf("operand %q Keep = %v, want %v", tok.Text, got, CodeBias)
			}
		}
	}
}

func TestCodeShapedIdentifiersStayCode(t *testing.T) {
	lex := NewLexicon(map[string]float64{"we": 1000})
	toks := lexRuby(t, "user_id = 1\n")
	lab := ClassifyLexical(lex, toks)

	var sawIdentifier bool
	for _, d := range lab.Decisions {
		if d.Token.Kind == host.Identifier {
			sawIdentifier = true
			if d.Label != Code {
				t.Errorf("identifier %q got label %v, want code", d.Token.Text, d.Label)
			}
		}
	}
	if !sawIdentifier {
		t.Fatal("no identifier token found")
	}
}

func TestOperatorsAndLiteralsAreCode(t *testing.T) {
	lex := NewLexicon(nil)
	toks := lexRuby(t, `x = "hello" + 1.5`)
	lab := ClassifyLexical(lex, toks)
	for _, d := range lab.Decisions {
		if d.Token.IsTrivia() {
			continue
		}
		if d.Label != Code {
			t.Errorf("token %v got label %v, want code", d.Token, d.Label)
		}
	}
}

// TestProseEscapeWinsWithoutAGrammar pins the provisional path: an author's
// ~ ... ~ span is English even when no machine is available to enforce it.
func TestProseEscapeWinsWithoutAGrammar(t *testing.T) {
	lex := NewLexicon(map[string]float64{"we": 100})
	toks := lexRuby(t, "~ a note ~\nx = 1\n")
	lab := ClassifyLexical(lex, toks)

	var sawProse bool
	for _, d := range lab.Decisions {
		if d.Token.Kind != host.Prose {
			continue
		}
		sawProse = true
		if d.Label != English {
			t.Fatalf("prose %q got label %v, want english", d.Token.Text, d.Label)
		}
	}
	if !sawProse {
		t.Fatal("no prose escape token found")
	}
}

func TestLexicalClassifierCountsAndCosts(t *testing.T) {
	lex := NewLexicon(map[string]float64{"we": 100, "other": 100})
	toks := lexRuby(t, "we x\n")
	lab := ClassifyLexical(lex, toks)

	if lab.Mode != "lexical" {
		t.Fatalf("mode = %q", lab.Mode)
	}
	code, english, trivia := lab.Counts()
	if code != 1 || english != 1 {
		t.Fatalf("code=%d english=%d trivia=%d, want 1,1,1", code, english, trivia)
	}
	if lab.EnglishCost <= 0 {
		t.Fatalf("english cost total = %v", lab.EnglishCost)
	}
}
