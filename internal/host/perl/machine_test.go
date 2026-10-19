package perl

import (
	"strings"
	"testing"

	"enx/internal/core"
	"enx/internal/host"
)

// keepEverything is a cost model in which no token may be English, so the
// grammar must accept the input exactly as written.
type keepEverything struct{ toks []host.Token }

func (k keepEverything) Keep(int) float64 { return 0 }

func (k keepEverything) Skip(i int) float64 {
	if k.toks[i].IsTrivia() {
		return 0
	}
	return host.ImpossibleCost
}

func decodeKeepingAll(t *testing.T, text string) (*host.Path, []host.Token) {
	t.Helper()
	src := core.NewSource("test.pl", []byte(text))
	toks, diags := New().Lex(src)
	if len(diags) != 0 {
		t.Fatalf("unexpected lex diagnostics: %v", diags)
	}
	path, ok := NewMachine().Decode(toks, keepEverything{toks: toks})
	if !ok {
		t.Fatalf("grammar rejected:\n%s", text)
	}
	return path, toks
}

// TestGrammarAcceptsRealPerl is the coverage test: every snippet must be
// accepted with nothing dropped.
func TestGrammarAcceptsRealPerl(t *testing.T) {
	snippets := []string{
		"my $x = 5;\n",
		"our $VERSION = 1.0;\n",
		"local $/ = undef;\n",
		"state $n = 0;\n",
		"my ($a, $b) = @_;\n",
		"my @list = ( 1, 2, 3 );\n",
		"my %h = ( a => 1, b => 2 );\n",
		"my $s = \"text $x\";\n",
		"my $s = 'literal';\n",
		"my $w = qw( alpha beta );\n",
		"use strict;\n",
		"use warnings;\n",
		"no warnings;\n",
		"package Foo::Bar;\n",
		"sub price_str { return 1; }\n",
		"sub add ($$) { return $_[0] + $_[1]; }\n",
		"sub helper;\n",
		"my $code = sub { my ($x) = @_; return $x * 2; };\n",
		"if ($x > 5) { print $x; } else { print $y; }\n",
		"if ($x) { 1; } elsif ($y) { 2; } else { 3; }\n",
		"unless ($x) { print \"no\"; }\n",
		"while ($x < 10) { $x++; }\n",
		"until ($x == 0) { $x--; }\n",
		"for (my $i = 0; $i < 10; $i++) { $t += $i; }\n",
		"foreach my $n (@list) { $sum += $n; }\n",
		"foreach $key (keys %h) { print $key; }\n",
		"{ my $inner = 1; }\n",
		"LOOP: while (1) { last LOOP; }\n",
		"next if $x;\n",
		"last unless $y;\n",
		"return $x;\n",
		"die \"boom\";\n",
		"warn \"careful\";\n",
		"my @sorted = sort @list;\n",
		"my @upper = map { uc $_ } @list;\n",
		"my @even = grep { $_ % 2 == 0 } @list;\n",
		"my $joined = join \",\", @list;\n",
		"my @parts = split /,/, $line;\n",
		"print \"total: $t\\n\";\n",
		"printf \"%d\\n\", $n;\n",
		"say $n;\n",
		"$x =~ s/foo/bar/g;\n",
		"$x =~ tr/a-z/A-Z/;\n",
		"$obj->method(1, 2);\n",
		"$obj->{key} = 1;\n",
		"$arr->[0] = 1;\n",
		"$h{key} = 1;\n",
		"$a[0] = 1;\n",
		"print $$ref;\n",
		"my $r = \\%h;\n",
		"print $r->{x};\n",
		"my $n = 0x1F + 0b1010 + 1_000;\n",
		"my $s = <<EOF;\nbody\nEOF\n",
		"$x = $a // $b;\n",
		"$x = $a ? 1 : 0;\n",
		"my $n = @list + 1;\n",
		"push @list, 4;\n",
		"my $f = shift @list;\n",
		"open my $fh, '<', $path;\n",
		"close $fh;\n",
		"my $len = length $s;\n",
		"my $i = index $s, \"x\";\n",
		"$x .= \"suffix\";\n",
		"$x = $a . $b . $c;\n",
		"$s =~ /pattern/i;\n",
		"my $ok = defined $x;\n",
		"$count++;\n",
		"$count--;\n",
		"eval { risky(); };\n",
		"do { $x = 1; };\n",
		"my $val = $h{$key}{$sub};\n",
		"sub foo :lvalue { $x }\n",
		"given ($x) { when (1) { print 1; } when (2) { print 2; } }\n",
		"package Foo { sub bar { 1 } }\n",
		"__DATA__\nbody text\n",
	}

	for _, text := range snippets {
		t.Run(strings.SplitN(text, "\n", 2)[0], func(t *testing.T) {
			path, toks := decodeKeepingAll(t, text)
			for i, tok := range toks {
				if tok.IsTrivia() {
					continue
				}
				if path.Actions[i] != host.KeepToken {
					t.Fatalf("dropped %v from:\n%s", tok, text)
				}
			}
		})
	}
}

// TestProseCannotBeACall is the strict-subs property: an English word run is
// not a valid call, because a bareword call needs parentheses.
func TestProseCannotBeACall(t *testing.T) {
	// With a cost model that allows skipping, the English must disappear and
	// the code must remain.
	src := core.NewSource("t.pl", []byte("we set the first amount;\nmy $x = 5;\n"))
	toks, _ := New().Lex(src)

	// Only the assignment may be kept: "we", "set", "the", "first", "amount"
	// are barewords and the grammar refuses them as code.
	costs := &allowSkip{toks: toks}
	path, ok := NewMachine().Decode(toks, costs)
	if !ok {
		t.Fatal("decode failed")
	}
	var kept []string
	for i, tok := range toks {
		if tok.IsTrivia() {
			continue
		}
		if path.Actions[i] == host.KeepToken {
			kept = append(kept, tok.Text)
		}
	}
	got := strings.Join(kept, " ")
	for _, word := range []string{"we", "set", "first", "amount"} {
		if strings.Contains(got, word) {
			t.Fatalf("prose word %q survived: %q", word, got)
		}
	}
	if !strings.Contains(got, "my $x = 5") {
		t.Fatalf("code was lost: %q", got)
	}
}

// allowSkip permits skipping any non-trivia token at a modest price.
type allowSkip struct{ toks []host.Token }

func (a *allowSkip) Keep(i int) float64 { return 4 }

func (a *allowSkip) Skip(i int) float64 {
	if a.toks[i].IsTrivia() {
		return 0
	}
	return 8
}

func TestSeparatorAfterStatements(t *testing.T) {
	path, toks := decodeKeepingAll(t, "my $x = 5;\nmy $y = 6;\n")
	var semi int = -1
	for i, tok := range toks {
		if tok.Text == ";" && semi < 0 {
			semi = i
		}
	}
	if semi < 0 {
		t.Fatal("no semicolon found")
	}
	if !path.SeparatorAfter[semi] {
		t.Error("a separator must be allowed after a statement terminator")
	}
}

// TestBareReturnIsNotAStatementEnd pins that a bare "return" before a closing
// brace does not get a separator-after flag.
func TestBareReturnIsNotAStatementEnd(t *testing.T) {
	path, toks := decodeKeepingAll(t, "sub f { return\n}\n")
	for i, tok := range toks {
		if tok.Kind == host.Keyword && tok.Text == "return" {
			if path.SeparatorAfter[i] {
				t.Fatal("a separator must not follow a bare return")
			}
			return
		}
	}
	t.Fatal("no return token found")
}

func TestEmptyInputDecodesToAnEmptyProgram(t *testing.T) {
	path, ok := NewMachine().Decode(nil, keepEverything{})
	if !ok {
		t.Fatal("empty input should decode")
	}
	if len(path.Actions) != 0 {
		t.Fatalf("got %d actions", len(path.Actions))
	}
}
