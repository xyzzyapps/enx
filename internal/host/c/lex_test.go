package c

import (
	"strings"
	"testing"

	"enx/internal/core"
	"enx/internal/host"
)

func lex(t *testing.T, text string) []host.Token {
	t.Helper()
	src := core.NewSource("test.c", []byte(text))
	toks, diags := New().Lex(src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	return toks
}

func significant(toks []host.Token) []host.Token {
	out := make([]host.Token, 0, len(toks))
	for _, tok := range toks {
		if !tok.IsTrivia() {
			out = append(out, tok)
		}
	}
	return out
}

// TestUnicodePunctuationIsNotAnIdentifier checks that a non-ASCII punctuation
// rune is one punctuation token, not a letter-led identifier or a byte split.
func TestUnicodePunctuationIsNotAnIdentifier(t *testing.T) {
	toks := significant(lex(t, "int x = 1 — 2;\n"))
	for _, tok := range toks {
		if tok.Text != "—" {
			continue
		}
		if tok.Kind != host.Punct {
			t.Fatalf("em dash lexed as %v, want punct", tok.Kind)
		}
		return
	}
	t.Fatalf("em dash token not found: %v", toks)
}

// TestTotality is the contract the emitter depends on: every byte belongs to
// exactly one token, in order.
func TestTotality(t *testing.T) {
	inputs := []string{
		"",
		"int x = 5;\n",
		"#include <stdio.h>\n",
		"#include \"local.h\"\n",
		"#define MAX 10\n",
		"#define LONG(a) \\\n  ((a) + 1)\n",
		"/* block\n   comment */\nint x;\n",
		"// line comment\nint x;\n",
		"double d = 1.5e-3;\n",
		"long n = 0x1FUL;\n",
		"char c = '\\n';\n",
		"const char *s = \"a\\\"b\";\n",
		"struct S { int a; };\n",
		"int *p = &x;\n",
		"p->x = arr[i++];\n",
		"int f(void) { return 0; }\n",
		"a = b ? c : d;\n",
		"x <<= 2;\n",
		"int (*fp)(int, int);\n",
		"#ifdef X\nint y;\n#endif\n",
		"long long big = 1LL;\n",
		"int n = 0b1010;\n",
		"#define STR \"multi \\\n  part\"\n",
	}

	for _, in := range inputs {
		toks := lex(t, in)
		var b strings.Builder
		last := 0
		for _, tok := range toks {
			if tok.Span.Start != last {
				t.Fatalf("input %q: gap at byte %d, token %s starts at %d", in, last, tok, tok.Span.Start)
			}
			b.WriteString(tok.Text)
			last = tok.Span.End
		}
		if b.String() != in {
			t.Fatalf("input %q: round trip produced %q", in, b.String())
		}
		if last != len(in) {
			t.Fatalf("input %q: stopped at %d of %d bytes", in, last, len(in))
		}
	}
}

func TestPreprocessorIsOneToken(t *testing.T) {
	toks := significant(lex(t, "#include <stdio.h>\nint x;\n"))
	if len(toks) == 0 || toks[0].Kind != host.Preproc {
		t.Fatalf("first token = %v, want a preproc token", toks)
	}
	if toks[0].Text != "#include <stdio.h>" {
		t.Fatalf("preproc text = %q", toks[0].Text)
	}
}

func TestPreprocessorContinuation(t *testing.T) {
	toks := significant(lex(t, "#define LONG(a) \\\n  ((a) + 1)\nint x;\n"))
	if toks[0].Kind != host.Preproc {
		t.Fatalf("first token = %v", toks[0])
	}
	if !strings.Contains(toks[0].Text, "((a) + 1)") {
		t.Fatalf("continuation was not joined: %q", toks[0].Text)
	}
	if toks[1].Kind != host.Keyword || toks[1].Text != "int" {
		t.Fatalf("lexing did not resume after the directive: %v", toks)
	}
}

// TestHashNotAtLineStart proves a "#" only begins a directive at the start of a
// line, so a stray one does not swallow the rest of the file.
func TestHashNotAtLineStart(t *testing.T) {
	toks := significant(lex(t, "int x; # int y;\n"))
	for _, tok := range toks {
		if tok.Kind == host.Preproc {
			t.Fatalf("misread a directive from mid-line: %v", toks)
		}
	}
}

func TestKeywords(t *testing.T) {
	for _, kw := range []string{"if", "else", "while", "for", "do", "switch", "case", "default", "break", "continue", "return", "goto", "static", "const", "long", "short", "signed", "unsigned", "float", "void", "auto", "register", "extern", "union", "enum", "struct", "sizeof", "typedef"} {
		toks := significant(lex(t, kw+" "))
		if len(toks) == 0 || toks[0].Kind != host.Keyword {
			t.Fatalf("%q lexed as %v, want keyword", kw, toks)
		}
	}
}

// TestEnglishWordsAreNotKeywords is the point of section 12.3 of the spec: most
// C keywords are ordinary English, and the decoder cannot rely on the lexer to
// separate them.
func TestEnglishWordsAreNotKeywords(t *testing.T) {
	for _, w := range []string{"the", "total", "value", "result", "count", "sum", "compute", "answer"} {
		toks := significant(lex(t, w))
		if len(toks) != 1 {
			t.Fatalf("%q lexed as %v", w, toks)
		}
		if toks[0].Kind == host.Keyword {
			t.Fatalf("%q should not be a keyword", w)
		}
	}
}

func TestNumbers(t *testing.T) {
	cases := map[string]host.Kind{
		"0":      host.Int,
		"0x1F":   host.Int,
		"0b1010": host.Int,
		"017":    host.Int,
		"1UL":    host.Int,
		"1LL":    host.Int,
		"1.5":    host.Float,
		"1.5e-3": host.Float,
		".5":     host.Float,
		"1.0f":   host.Float,
	}
	for text, want := range cases {
		toks := significant(lex(t, text+";"))
		if len(toks) == 0 || toks[0].Kind != want {
			t.Fatalf("%q lexed as %v, want %v", text, toks, want)
		}
	}
}

func TestCharVersusString(t *testing.T) {
	toks := significant(lex(t, "char c = 'a'; char *s = \"a\";"))
	var kinds []host.Kind
	for _, tok := range toks {
		kinds = append(kinds, tok.Kind)
	}
	if !contains(kinds, host.Char) || !contains(kinds, host.String) {
		t.Fatalf("kinds = %v, want both char and string", kinds)
	}
}

func TestOperators(t *testing.T) {
	cases := map[string]string{
		"a <<= 1;": "<<=",
		"a >> 1;":  ">>",
		"p->x;":    "->",
		"i++;":     "++",
		"a != b;":  "!=",
		"a && b;":  "&&",
		"a ... b;": "...",
	}
	for text, want := range cases {
		toks := significant(lex(t, text))
		found := false
		for _, tok := range toks {
			if tok.Text == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("%q: operator %q not found in %v", text, want, toks)
		}
	}
}

func TestLineSpliceOutsideMacroIsWhitespace(t *testing.T) {
	toks := lex(t, "int \\\nx = 1;\n")
	var b strings.Builder
	for _, tok := range toks {
		b.WriteString(tok.Text)
	}
	if b.String() != "int \\\nx = 1;\n" {
		t.Fatalf("round trip = %q", b.String())
	}
}

func contains(kinds []host.Kind, want host.Kind) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}
