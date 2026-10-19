package ruby

import (
	"strings"
	"testing"

	"enx/internal/core"
	"enx/internal/host"
	"enx/internal/host/parse"
)

// keepEverything is a cost model in which no token may be English, so the
// grammar must accept the input exactly as written. It is how the grammar's
// coverage is measured without the cost model in the way.
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
	src := core.NewSource("test.rb", []byte(text))
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

// TestGrammarAcceptsRealRuby is the coverage test: every snippet must be
// accepted with nothing dropped.
func TestGrammarAcceptsRealRuby(t *testing.T) {
	snippets := []string{
		"x = 5\n",
		"sum = lambda { |a, b| a + b }\n",
		"def f(a, b)\n  a + b\nend\n",
		"def to_s = format(\"%d\", n)\n",
		"def self.run\n  go\nend\n",
		"def <=>(other)\n  cents <=> other.cents\nend\n",
		"class A\n  def initialize(x)\n    @x = x\n  end\nend\n",
		"class A < B\n  include Comparable\nend\n",
		"class << self\n  def rules\n    @rules ||= {}\n  end\nend\n",
		"module M\n  V = 1\nend\n",
		"module A::B\n  C = 2\nend\n",
		"if x > 5\n  puts x\nelsif x > 1\n  puts y\nelse\n  puts z\nend\n",
		"unless x\n  y\nend\n",
		"while x < 10\n  x += 1\nend\n",
		"until x.zero?\n  x -= 1\nend\n",
		"for n in [1, 2, 3]\n  puts n\nend\n",
		"a.each do |n|\n  puts n\nend\n",
		"a.map { |n| n * 2 }\n",
		"h = { a: 1, \"b\" => 2 }\n",
		"arr = [1, 2, 3]\n",
		"arr = []\n",
		"s = %w[a b]\n",
		"r = /ab+c/i\n",
		"begin\n  risky\nrescue StandardError => e\n  warn e\nensure\n  done\nend\n",
		"case n\nwhen 1 then puts \"one\"\nelse puts \"other\"\nend\n",
		"return x\n",
		"break\n",
		"next if x\n",
		"puts \"hi\" if debug\n",
		"x = a ? b : c\n",
		"obj.method(arg).chain { |v| v }\n",
		"attr_reader :a, :b\n",
		"@lines << [product, count]\n",
		"@applied.values.reduce(Money.new(0)) { |acc, m| acc + m }\n",
		"puts \"total: #{$total} dollars\"\n",
		"yield x\n",
		"super\n",
		"x&.y\n",
		"a = b\nc = d\n",
		"a, b = 1, 2\n",
		"x, y = y, x\n",
		"first, *rest = list\n",
		"h[:key], h[:other] = 1, 2\n",
		"return a, b\n",
		"order.add(coffee, 2).add(mug, 1)\n",
		"arr.select { |x| x > 1 }.map { |x| x * 2 }.reduce(0) { |a, b| a + b }\n",
		"puts price_str(order.subtotal)\n",
		"if subtotal > 3000\n  puts \"big\"\nend\n",
		"sql = <<~SQL\n  select 1\nSQL\n",
		"n = 0x1F\nf = 1.5e3\ns = :name\n",
		"obj = Object.new\ndef obj.foo\n  1\nend\n",
		"def name=(value)\n  @name = value\nend\n",
		"case x\nin [a, b]\n  a\nelse\n  b\nend\n",
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

// TestIfExprAndUnlessStmtDoNotShareMemo pins the memo key separation. Sharing
// a slot would let a failed if-expression attempt at an "unless" position
// poison the unless statement that follows at the same position.
func TestIfExprAndUnlessStmtDoNotShareMemo(t *testing.T) {
	src := core.NewSource("test.rb", []byte("unless x\n  y\nend\n"))
	toks, _ := New().Lex(src)
	p := &parser{Parser: parse.New(toks, keepEverything{toks: toks}, rubyStop)}
	p.initForms()

	if r := p.ifExpr(0); r.OK {
		t.Fatal("ifExpr accepted an unless")
	}
	if r := p.unlessStmt(0); !r.OK {
		t.Fatal("unlessStmt was poisoned by the ifExpr memo entry")
	}
}

// TestKeptTokensPlusTriviaReproduceTheInput restates the core guarantee at the
// grammar level: whatever is kept, together with the trivia that is always
// preserved, is the input in order, byte for byte.
func TestKeptTokensPlusTriviaReproduceTheInput(t *testing.T) {
	text := "x = 5\nputs x\n"
	path, toks := decodeKeepingAll(t, text)
	var b strings.Builder
	for i, tok := range toks {
		if tok.IsTrivia() || path.Actions[i] == host.KeepToken {
			b.WriteString(tok.Text)
		}
	}
	if b.String() != text {
		t.Fatalf("kept text = %q, want %q", b.String(), text)
	}
}

// TestSeparatorAfterStatements pins the boundary map that lets the emitter put
// a newline where one is legal and a space where it is not.
func TestSeparatorAfterStatements(t *testing.T) {
	// The last token of "x = 5" ends a statement; the "5" does not.
	path, toks := decodeKeepingAll(t, "x = 5\ny = 6\n")

	indexOf := func(text string) int {
		for i, tok := range toks {
			if tok.Kind == host.Int && tok.Text == text {
				return i
			}
			if tok.Kind == host.Identifier && tok.Text == text {
				return i
			}
		}
		return -1
	}
	if i := indexOf("x"); i >= 0 && path.SeparatorAfter[i] {
		t.Error("a separator must not follow the left hand side of an assignment")
	}
	firstFive := -1
	for i, tok := range toks {
		if tok.Kind == host.Int && tok.Text == "5" {
			firstFive = i
			break
		}
	}
	if firstFive < 0 || !path.SeparatorAfter[firstFive] {
		t.Error("a separator must be allowed after the end of a statement")
	}
}

// TestBareReturnIsNotAStatementEnd pins that a bare "return" does not get a
// separator-after flag. If it did, an English region on the same line would be
// replaced by a newline, changing "return x" into a bare return.
func TestBareReturnIsNotAStatementEnd(t *testing.T) {
	path, toks := decodeKeepingAll(t, "return\n")
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

// TestReturnKeepsOperandOnSameLine covers the hazard that forced the machine to
// exist: a newline after "return" would turn the statement into a bare return.
func TestReturnKeepsOperandOnSameLine(t *testing.T) {
	path, toks := decodeKeepingAll(t, "def f\n  return x\nend\n")
	for i, tok := range toks {
		if tok.Kind == host.Keyword && tok.Text == "return" {
			if path.SeparatorAfter[i] {
				t.Fatal("a separator must not be allowed directly after return")
			}
			return
		}
	}
	t.Fatal("no return token found")
}

// TestEmptyInputDecodesToAnEmptyProgram checks the degenerate case: a file with
// no code is a valid empty program, not a failure.
func TestEmptyInputDecodesToAnEmptyProgram(t *testing.T) {
	path, ok := NewMachine().Decode(nil, forbidAll{})
	if !ok {
		t.Fatal("empty input should decode")
	}
	if len(path.Actions) != 0 {
		t.Fatalf("got %d actions", len(path.Actions))
	}
}

type forbidAll struct{ toks []host.Token }

func (f forbidAll) Keep(i int) float64 { return host.ImpossibleCost }
func (f forbidAll) Skip(i int) float64 { return 0 }
