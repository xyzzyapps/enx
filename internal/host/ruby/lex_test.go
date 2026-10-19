package ruby

import (
	"strings"
	"testing"

	"enx/internal/core"
	"enx/internal/host"
)

func lex(t *testing.T, text string) []host.Token {
	t.Helper()
	src := core.NewSource("test.rb", []byte(text))
	toks, diags := New().Lex(src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	return toks
}

// significant drops trivia so tests can assert on code tokens only.
func significant(toks []host.Token) []host.Token {
	out := make([]host.Token, 0, len(toks))
	for _, tok := range toks {
		if !tok.IsTrivia() {
			out = append(out, tok)
		}
	}
	return out
}

func kinds(toks []host.Token) []host.Kind {
	out := make([]host.Kind, len(toks))
	for i, tok := range toks {
		out[i] = tok.Kind
	}
	return out
}

// TestTotality is the contract the emitter depends on: every byte of the input
// belongs to exactly one token, in order.
func TestTotality(t *testing.T) {
	inputs := []string{
		"",
		"x = 5\n",
		"sum = lambda { |a, b| a + b }\n",
		"we start with x as five, that is x = 5, and y as five too: y = 5\n",
		"module Shop\n  TAX_RATE = 0.08\nend\n",
		"class Money\n  def <=>(other) = (cents <=> other.cents)\nend\n",
		"@cents = Integer(cents)\nfreeze\n",
		"puts \"total: #{$total} dollars\"\n",
		"s = :sum\nt = :\"quoted name\"\n",
		"r = /ab+c/i\nq = a / b\n",
		"w = %w[pantry hot]\ni = %i[kitchen]\nq = %q{literal}\n",
		"sql = <<~SQL\n  select * from t\nSQL\n",
		"# a comment\nx = 1 # trailing\n",
		"=begin\nblock\n=end\nx = 1\n",
		"h = { name: \"x\", n: 0x1F, f: 1.5e3, b: 0b1010 }\n",
		"arr = [1, 2, 3].map { |n| n * 2 }\n",
		"unless x.nil? then puts x! else puts x? end\n",
		"a << b\nc <<~SQL\n  body\nSQL\n",
		"pie = 3.14159\ntau = 2 * pie\n",
		"x = y ? 1 : 2\n",
	}

	for _, in := range inputs {
		toks := lex(t, in)
		var b strings.Builder
		last := 0
		for _, tok := range toks {
			if tok.Span.Start != last {
				t.Fatalf("input %q: gap at byte %d, token %s starts at %d", in, last, tok, tok.Span.Start)
			}
			if tok.Text != string([]byte(in)[tok.Span.Start:tok.Span.End]) {
				t.Fatalf("input %q: token text %q does not match span", in, tok.Text)
			}
			b.WriteString(tok.Text)
			last = tok.Span.End
		}
		if b.String() != in {
			t.Fatalf("input %q: round trip produced %q", in, b.String())
		}
		if last != len(in) {
			t.Fatalf("input %q: lexer stopped at %d of %d bytes", in, last, len(in))
		}
	}
}

// TestHeredocTerminatorWithCRLF covers the Windows line-ending case: the
// terminator line carries "\r" before the newline is consumed, and it must
// still match for a non-indented heredoc.
func TestHeredocTerminatorWithCRLF(t *testing.T) {
	for _, text := range []string{
		"sql = <<SQL\r\nbody\r\nSQL\r\nx = 1\r\n",
		"sql = <<~SQL\r\n  body\r\nSQL\r\nx = 1\r\n",
	} {
		toks := lex(t, text)
		var sawX bool
		for _, tok := range toks {
			if tok.Kind == host.Identifier && tok.Text == "x" {
				sawX = true
			}
		}
		if !sawX {
			t.Fatalf("the heredoc swallowed the rest of %q: %v", text, toks)
		}
	}
}

// TestUnicodePunctuationIsNotAnIdentifier checks that a non-ASCII punctuation
// rune is one punctuation token, not a letter-led identifier or a byte split.
func TestUnicodePunctuationIsNotAnIdentifier(t *testing.T) {
	toks := significant(lex(t, "x — y\n"))
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

func TestAssignment(t *testing.T) {
	got := kinds(significant(lex(t, "x = 5\n")))
	want := []host.Kind{host.Identifier, host.Operator, host.Int}
	if len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", got, want)
		}
	}
}

func TestLambdaAndBlock(t *testing.T) {
	toks := significant(lex(t, "sum = lambda { |a, b| a + b }\n"))
	want := []host.Kind{
		host.Identifier, host.Operator, host.Identifier,
		host.Punct, host.Operator, host.Identifier, host.Punct, host.Identifier,
		host.Operator, host.Identifier, host.Operator, host.Identifier, host.Punct,
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens (%v)", len(toks), kinds(toks))
	}
	for i := range want {
		if toks[i].Kind != want[i] {
			t.Fatalf("token %d = %v, want %v (all: %v)", i, toks[i], want[i], kinds(toks))
		}
	}
}

func TestKeywordsAreKeywordKind(t *testing.T) {
	for _, kw := range []string{"if", "end", "and", "return", "do", "while", "class", "def"} {
		toks := significant(lex(t, kw))
		if len(toks) != 1 || toks[0].Kind != host.Keyword {
			t.Fatalf("%q lexed as %v, want keyword", kw, toks)
		}
	}
}

// TestEnglishWordsAreIdentifierKind proves why the decoder cannot rely on the
// lexer to separate the languages: these are all ordinary English words.
func TestEnglishWordsAreIdentifierKind(t *testing.T) {
	for _, w := range []string{"we", "start", "sum", "answer", "the", "finally", "compute", "first", "numbers"} {
		toks := significant(lex(t, w))
		if len(toks) != 1 {
			t.Fatalf("%q lexed as %v", w, toks)
		}
		if toks[0].Kind == host.Keyword {
			t.Fatalf("%q should not be a keyword", w)
		}
	}
}

func TestConstantsAndVariables(t *testing.T) {
	toks := significant(lex(t, "@cents @@count $global MAX_SIZE Foo"))
	want := []host.Kind{host.Variable, host.Variable, host.Variable, host.Constant, host.Constant}
	if len(toks) != len(want) {
		t.Fatalf("got %v", kinds(toks))
	}
	for i := range want {
		if toks[i].Kind != want[i] {
			t.Fatalf("token %d (%q) = %v, want %v", i, toks[i].Text, toks[i].Kind, want[i])
		}
	}
}

func TestStringInterpolationIsOneToken(t *testing.T) {
	toks := significant(lex(t, `puts "total: #{$total} dollars"`))
	if len(toks) != 2 {
		t.Fatalf("got %v", toks)
	}
	if toks[1].Kind != host.String {
		t.Fatalf("string = %v", toks[1])
	}
	if toks[1].Text != `"total: #{$total} dollars"` {
		t.Fatalf("string text = %q", toks[1].Text)
	}
}

func TestRegexVersusDivision(t *testing.T) {
	toks := significant(lex(t, "q = a / b\nr = /ab+c/"))
	k := kinds(toks)
	if k[3] != host.Operator {
		t.Fatalf("division token = %v, want operator (%v)", toks[3], toks)
	}
	found := false
	for _, tok := range toks {
		if tok.Kind == host.Regex {
			found = true
			if tok.Text != "/ab+c/" {
				t.Fatalf("regex text = %q", tok.Text)
			}
		}
	}
	if !found {
		t.Fatalf("no regex token in %v", toks)
	}
}

func TestRegexAllowedAtExpressionStart(t *testing.T) {
	toks := significant(lex(t, "x =~ /foo/"))
	if toks[len(toks)-1].Kind != host.Regex {
		t.Fatalf("got %v", toks)
	}
}

func TestPercentLiterals(t *testing.T) {
	toks := significant(lex(t, "%w[pantry hot] %i[kitchen] %q{literal} %r{a+b}"))
	if len(toks) != 4 {
		t.Fatalf("got %v", toks)
	}
	want := []host.Kind{host.String, host.String, host.String, host.Regex}
	for i := range want {
		if toks[i].Kind != want[i] {
			t.Fatalf("token %d = %v, want %v", i, toks[i], want[i])
		}
	}
}

func TestHeredocSwallowsBody(t *testing.T) {
	toks := significant(lex(t, "sql = <<~SQL\n  select * from t\nSQL\nx = 1\n"))
	var bodies int
	var sawIdentifier bool
	for _, tok := range toks {
		if tok.Kind == host.Heredoc {
			bodies++
		}
		if tok.Kind == host.Identifier && tok.Text == "x" {
			sawIdentifier = true
		}
	}
	if bodies < 2 {
		t.Fatalf("expected heredoc marker and body, got %v", toks)
	}
	if !sawIdentifier {
		t.Fatalf("lexing did not resume after the heredoc: %v", toks)
	}
}

func TestShiftIsNotHeredoc(t *testing.T) {
	toks := significant(lex(t, "a << b\n"))
	for _, tok := range toks {
		if tok.Kind == host.Heredoc {
			t.Fatalf("shift operator misread as heredoc: %v", toks)
		}
	}
}

func TestCommentsAndBlockComments(t *testing.T) {
	toks := lex(t, "# one\nx = 1 # two\n=begin\nthree\n=end\ny = 2\n")
	var comments int
	for _, tok := range toks {
		if tok.Kind == host.Comment {
			comments++
		}
	}
	if comments != 3 {
		t.Fatalf("comment count = %d, want 3 (%v)", comments, toks)
	}
}

func TestNumbers(t *testing.T) {
	cases := map[string]host.Kind{
		"0":      host.Int,
		"1_000":  host.Int,
		"0x1F":   host.Int,
		"0b1010": host.Int,
		"0o777":  host.Int,
		"1.5":    host.Float,
		"1.5e3":  host.Float,
		"2.0e-4": host.Float,
		"42r":    host.Int,
	}
	for text, want := range cases {
		toks := significant(lex(t, text))
		if len(toks) != 1 || toks[0].Kind != want {
			t.Fatalf("%q lexed as %v, want %v", text, toks, want)
		}
	}
}

func TestRangeIsNotFloat(t *testing.T) {
	toks := significant(lex(t, "1..5"))
	if len(toks) != 3 {
		t.Fatalf("got %v", toks)
	}
	if toks[0].Kind != host.Int || toks[1].Kind != host.Operator || toks[2].Kind != host.Int {
		t.Fatalf("got %v", kinds(toks))
	}
}

func TestUnterminatedStringIsTotal(t *testing.T) {
	// An unterminated literal must not lose bytes; the emitter's identity
	// property still has to hold.
	toks := lex(t, "x = \"unterminated")
	var b strings.Builder
	for _, tok := range toks {
		b.WriteString(tok.Text)
	}
	if b.String() != "x = \"unterminated" {
		t.Fatalf("round trip = %q", b.String())
	}
}
