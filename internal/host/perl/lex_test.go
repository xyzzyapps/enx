package perl

import (
	"strings"
	"testing"

	"enx/internal/core"
	"enx/internal/host"
)

func lex(t *testing.T, text string) []host.Token {
	t.Helper()
	src := core.NewSource("test.pl", []byte(text))
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
	toks := significant(lex(t, "my $x = 1 — 2;\n"))
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
		"my $x = 5;\n",
		"my @items = ( 1, 2, 3 );\n",
		"my %h = ( a => 1, b => 2 );\n",
		"print \"total: $total\\n\";\n",
		"print 'literal $x\\n';\n",
		"sub price_str { my ($cents) = @_; return sprintf '$%d.%02d', $cents / 100, $cents % 100; }\n",
		"if ($x > 5) { print $x; } else { print $y; }\n",
		"foreach my $n (@list) { $sum += $n; }\n",
		"for (my $i = 0; $i < 10; $i++) { $t += $i; }\n",
		"$obj->method(1, 2)->next;\n",
		"my @sorted = sort { $a cmp $b } keys %h;\n",
		"$x =~ s/foo/bar/g;\n",
		"$y = /pattern/i ? 1 : 0;\n",
		"q = $a / $b;\n",
		"my $w = qw( alpha beta );\n",
		"my $q = q{ text };\n",
		"my $s = <<EOF;\nbody text\nEOF\n",
		"# a comment\n$x = 1; # trailing\n",
		"=pod\n\ndocumentation here\n\n=cut\n$x = 1;\n",
		"my $ref = \\%h;\nprint $$ref{x};\n",
		"my $n = ${name} . @{ $list };\n",
		"$num = 0x1F + 0b1010 + 0o17 + 1_000;\n",
		"LOOP: while (1) { last LOOP; }\n",
		"my $f = sub { return 1; };\n",
		"$h{x}[0]{y} = 1;\n",
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

func TestVariables(t *testing.T) {
	cases := []string{"$x", "@list", "%h", "&sub", "*glob", "$_", "$@", "$0", "$/", "$^W", "$#arr", "${name}"}
	for _, text := range cases {
		toks := significant(lex(t, text+";"))
		if len(toks) != 2 {
			t.Fatalf("%q lexed as %v", text, toks)
		}
		if toks[0].Kind != host.Variable {
			t.Fatalf("%q kind = %v, want variable", text, toks[0].Kind)
		}
		if toks[0].Text != text {
			t.Fatalf("%q text = %q", text, toks[0].Text)
		}
	}
}

func TestKeywords(t *testing.T) {
	for _, kw := range []string{"if", "else", "elsif", "sub", "my", "our", "foreach", "return", "use", "package", "while", "unless"} {
		toks := significant(lex(t, kw+";"))
		if len(toks) == 0 || toks[0].Kind != host.Keyword {
			t.Fatalf("%q lexed as %v", kw, toks)
		}
	}
}

func TestQuoteLikeOperatorsAreSingleTokens(t *testing.T) {
	cases := map[string]host.Kind{
		"qw( a b )":  host.String,
		"q{ text }":  host.String,
		"qq{ text }": host.String,
		"qr{ a+b }":  host.Regex,
		"m{ a+b }":   host.Regex,
	}
	for text, want := range cases {
		toks := significant(lex(t, text))
		if len(toks) != 1 {
			t.Fatalf("%q lexed as %v", text, toks)
		}
		if toks[0].Kind != want {
			t.Fatalf("%q kind = %v, want %v", text, toks[0].Kind, want)
		}
	}
}

func TestSubstitutionIsOneToken(t *testing.T) {
	toks := significant(lex(t, "$x =~ s/foo/bar/g;"))
	var found bool
	for _, tok := range toks {
		if strings.HasPrefix(tok.Text, "s/") {
			found = true
			if tok.Text != "s/foo/bar/g" {
				t.Fatalf("substitution text = %q", tok.Text)
			}
		}
	}
	if !found {
		t.Fatalf("no substitution token in %v", toks)
	}
}

func TestRegexVersusDivision(t *testing.T) {
	toks := significant(lex(t, "$q = $a / $b;\n$m = /pattern/;"))
	for _, tok := range toks {
		if tok.Text == "/" && tok.Kind != host.Operator {
			t.Fatalf("division classified as %v", tok.Kind)
		}
	}
	var sawRegex bool
	for _, tok := range toks {
		if tok.Kind == host.Regex && tok.Text == "/pattern/" {
			sawRegex = true
		}
	}
	if !sawRegex {
		t.Fatalf("no regex token in %v", toks)
	}
}

// TestDataMarkerIsTrivia checks that __DATA__ and its payload are preserved
// without the grammar trying to parse the payload as statements.
func TestDataMarkerIsTrivia(t *testing.T) {
	toks := lex(t, "__DATA__\nraw body\n")
	if len(toks) != 1 || toks[0].Kind != host.Comment {
		t.Fatalf("tokens = %v, want one comment", toks)
	}
	if toks[0].Text != "__DATA__\nraw body\n" {
		t.Fatalf("text = %q", toks[0].Text)
	}
}

func TestPodIsTrivia(t *testing.T) {
	toks := lex(t, "=pod\n\ntext here\n\n=cut\n$x = 1;\n")
	var pods int
	for _, tok := range toks {
		if tok.Kind == host.Comment {
			pods++
			if !strings.HasPrefix(tok.Text, "=pod") {
				t.Fatalf("POD token = %q", tok.Text)
			}
		}
	}
	if pods != 1 {
		t.Fatalf("POD count = %d", pods)
	}
}

func TestHeredocSwallowsBody(t *testing.T) {
	toks := significant(lex(t, "my $s = <<EOF;\nbody text\nEOF\n$x = 1;\n"))
	var bodies int
	var sawX bool
	for _, tok := range toks {
		if tok.Kind == host.Heredoc {
			bodies++
		}
		if tok.Text == "$x" {
			sawX = true
		}
	}
	if bodies < 2 {
		t.Fatalf("expected marker and body, got %v", toks)
	}
	if !sawX {
		t.Fatalf("lexing did not resume after the heredoc: %v", toks)
	}
}

func TestNumbers(t *testing.T) {
	cases := map[string]host.Kind{
		"0":      host.Int,
		"1_000":  host.Int,
		"0x1F":   host.Int,
		"0b1010": host.Int,
		"0o17":   host.Int,
		"1.5":    host.Float,
		"1.5e3":  host.Float,
		".5":     host.Float,
	}
	for text, want := range cases {
		toks := significant(lex(t, text+";"))
		if len(toks) == 0 || toks[0].Kind != want {
			t.Fatalf("%q lexed as %v, want %v", text, toks, want)
		}
	}
}

func TestLabelsAreNotVariables(t *testing.T) {
	toks := significant(lex(t, "LOOP: while (1) { last LOOP; }"))
	if toks[0].Kind != host.Constant && toks[0].Kind != host.Identifier {
		t.Fatalf("label lexed as %v", toks[0])
	}
}
