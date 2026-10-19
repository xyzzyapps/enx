package perl

import (
	"enx/internal/host"
	"enx/internal/host/parse"
)

// expression is the entry point for expressions.
func (p *parser) expression(pos int) parse.Result {
	return p.Memo(slotExpression, pos, false, func() parse.Result { return p.assignExpr(pos) })
}

var assignOps = []string{
	"=", "+=", "-=", "*=", "/=", "%=", "**=", ".=", "x=", "||=", "&&=",
	"//=", "|=", "&=", "^=", "<<=", ">>=",
}

func (p *parser) assignExpr(pos int) parse.Result {
	left := p.ternaryExpr(pos)
	if !left.OK {
		return parse.Fail
	}
	op := p.keepIf(left.Next, assignOps...)
	if !op.OK {
		return left
	}
	right := p.assignExpr(op.Next)
	if !right.OK {
		return parse.Fail
	}
	return parse.Combine(left, parse.Combine(op, right))
}

func (p *parser) ternaryExpr(pos int) parse.Result {
	cond := p.binaryChain(pos, 0)
	if !cond.OK {
		return parse.Fail
	}
	q := p.keepIf(cond.Next, "?")
	if !q.OK {
		return cond
	}
	thenArm := p.ternaryExpr(q.Next)
	if !thenArm.OK {
		return cond
	}
	colon := p.keepIf(thenArm.Next, ":")
	if !colon.OK {
		return cond
	}
	elseArm := p.ternaryExpr(colon.Next)
	if !elseArm.OK {
		return cond
	}
	return parse.Combine(cond, parse.Combine(q, parse.Combine(thenArm, parse.Combine(colon, elseArm))))
}

// binLevels lists binary operators from loosest to tightest. Perl's precedence
// is approximated: acceptance is what matters, since nothing is executed.
var binLevels = [][]string{
	{"or", "||", "//"},
	{"and", "&&"},
	{"..", "..."},
	{"==", "!=", "<=>", "eq", "ne", "cmp"},
	{"<", ">", "<=", ">=", "lt", "gt", "le", "ge"},
	{"=~", "!~"},
	{"|", "^"},
	{"&"},
	{"<<", ">>"},
	{"+", "-", "."},
	{"*", "/", "%", "x"},
}

func (p *parser) binaryChain(pos, level int) parse.Result {
	if level >= len(binLevels) {
		return p.unaryExpr(pos)
	}
	left := p.binaryChain(pos, level+1)
	if !left.OK {
		return parse.Fail
	}
	for {
		op := p.keepIf(left.Next, binLevels[level]...)
		if !op.OK {
			break
		}
		right := p.binaryChain(op.Next, level+1)
		if !right.OK {
			break
		}
		left = parse.Combine(left, parse.Combine(op, right))
	}
	return left
}

var unaryOperators = map[string]bool{
	"!": true, "not": true, "-": true, "+": true, "~": true,
	"\\": true, "++": true, "--": true,
}

func (p *parser) unaryExpr(pos int) parse.Result {
	if i := p.peekStmt(pos); i >= 0 {
		tok := p.Toks[i]
		if unaryOperators[tok.Text] && (tok.Kind == host.Operator || tok.Kind == host.Keyword) {
			pre := p.keepAt(i)
			if pre.OK {
				if operand := p.unaryExpr(pre.Next); operand.OK {
					return parse.Combine(pre, operand)
				}
			}
		}
	}
	return p.powerExpr(pos)
}

func (p *parser) powerExpr(pos int) parse.Result {
	left := p.postfixExpr(pos)
	if !left.OK {
		return parse.Fail
	}
	op := p.keepIf(left.Next, "**")
	if !op.OK {
		return left
	}
	right := p.unaryExpr(op.Next)
	if !right.OK {
		return left
	}
	return parse.Combine(left, parse.Combine(op, right))
}

func (p *parser) postfixExpr(pos int) parse.Result {
	cur := p.primaryExpr(pos)
	if !cur.OK {
		return parse.Fail
	}
	for {
		at := cur.Next
		r := p.Memo(slotSuffix, at, false, func() parse.Result { return p.suffix(at) })
		if !r.OK || r.Next <= cur.Next {
			break
		}
		cur = parse.Combine(cur, r)
	}
	return cur
}

func (p *parser) primaryExpr(pos int) parse.Result {
	return p.Memo(slotPrimary, pos, false, func() parse.Result {
		return p.altAt(pos, p.primForms...)
	})
}

// suffix parses one postfix operation: a method call or dereference through
// "->", or a subscript written against the preceding token.
func (p *parser) suffix(pos int) parse.Result {
	if arrow := p.keepIf(pos, "->"); arrow.OK {
		i := p.peekStmt(arrow.Next)
		if i < 0 {
			return parse.Fail
		}
		switch p.Toks[i].Text {
		case "{", "[":
			close := "}"
			if p.Toks[i].Text == "[" {
				close = "]"
			}
			group := p.balanced(arrow.Next, p.Toks[i].Text, close)
			if !group.OK {
				return parse.Fail
			}
			cur := parse.Combine(arrow, group)
			if args := p.parenArgs(cur.Next); args.OK {
				cur = parse.Combine(cur, args)
			}
			return cur
		}
		name := p.expectKind(arrow.Next, host.Identifier, host.Constant, host.Variable)
		if !name.OK {
			return parse.Fail
		}
		cur := parse.Combine(arrow, name)
		if args := p.parenArgs(cur.Next); args.OK {
			cur = parse.Combine(cur, args)
		}
		return cur
	}

	// A subscript must be written against the preceding token, with no space,
	// which is what stops a bare block being read as an index.
	if p.adjacent(pos) {
		i := p.peekStmt(pos)
		if i >= 0 {
			switch p.Toks[i].Text {
			case "[":
				group := p.balanced(pos, "[", "]")
				if group.OK {
					return group
				}
			case "{":
				group := p.balanced(pos, "{", "}")
				if group.OK {
					return group
				}
			}
		}
	}

	if inc := p.keepIf(pos, "++", "--"); inc.OK {
		return inc
	}
	return parse.Fail
}

// adjacent reports whether the next significant token touches the token before
// it, with no bytes between. It is what distinguishes "$h{key}", a subscript,
// from "$h { ... }", which would be a block.
func (p *parser) adjacent(pos int) bool {
	i := p.peek(pos)
	if i <= 0 {
		return false
	}
	return p.Toks[i-1].Span.End == p.Toks[i].Span.Start
}

func (p *parser) literalExpr(pos int) parse.Result {
	i := p.peekStmt(pos)
	if i < 0 {
		return parse.Fail
	}
	switch p.Toks[i].Kind {
	case host.Int, host.Float, host.String, host.Regex, host.Heredoc, host.Char, host.Anaphor:
		// An anaphor is a value, so it belongs wherever a literal does.
		return p.keepAt(i)
	}
	return parse.Fail
}

// barewordFilehandles are the capitals Perl allows as barewords even under
// strict subs.
var barewordFilehandles = map[string]bool{
	"STDIN": true, "STDOUT": true, "STDERR": true,
	"DATA": true, "ARGV": true, "ARGVOUT": true,
}

// attached reports whether the next significant token touches the token at pos
// and its text is one of texts. It is how "Foo::Bar" and "$obj->method" are
// told from a bareword.
func (p *parser) attached(pos int, texts ...string) bool {
	i := p.peek(pos)
	if i <= 0 {
		return false
	}
	if p.Toks[i-1].Span.End != p.Toks[i].Span.Start {
		return false
	}
	return parse.MatchText(p.Toks[i], texts)
}

// dispatchExpr covers a value or a call.
//
// Under "use strict" a bare name is only ever a call, and a call needs
// parentheses. That is the rule this function enforces, and it is why ordinary
// English cannot be parsed as code: "we set the first amount" has no
// parentheses, so every word in it is refused and dropped. The exceptions are
// the built-in list operators, which Perl does allow to take a bare list, and
// bare capitals, which serve as filehandles such as STDOUT.
func (p *parser) dispatchExpr(pos int) parse.Result {
	i := p.peek(pos)
	if i < 0 {
		return parse.Fail
	}
	tok := p.Toks[i]

	switch tok.Kind {
	case host.Variable:
		cur := p.keepAt(i)
		if blk := p.block(cur.Next); blk.OK {
			cur = parse.Combine(cur, blk)
		}
		return cur

	case host.Constant:
		// Under strict subs a bare capital is only a filehandle, or a package
		// name that is already in scope. So a constant is code only when it is
		// doing work: a receiver, a call, or one of the standard filehandles.
		// Otherwise it is a bareword and the grammar refuses it, which is what
		// removes acronyms such as "SKU" from prose.
		if !barewordFilehandles[tok.Text] && !p.attached(i+1, "->", "::", "(", "[", "{") {
			return parse.Fail
		}
		return p.keepAt(i)

	case host.Identifier, host.Keyword:
		head := p.keepAt(i)
		if !head.OK {
			return parse.Fail
		}
		if !listOps[tok.Text] {
			if tok.Kind != host.Identifier {
				return parse.Fail
			}
			// A plain call: the parenthesis must be written against the name.
			if args := p.parenArgs(head.Next); args.OK {
				return parse.Combine(head, args)
			}
			if blk := p.block(head.Next); blk.OK {
				return parse.Combine(head, blk)
			}
			// A bareword is auto-quoted as a hash key by a fat comma, so
			// "name => value" is legal under strict subs. The fat comma itself
			// is left for the list parser to consume as a separator.
			if fat := p.keepIf(head.Next, "=>"); fat.OK {
				return head
			}
			return parse.Fail
		}
		cur := head
		// Only the operators that really take a block may take one here. map,
		// grep and sort do; bless does not, and treating "bless {}, shift" as a
		// block call made the whole statement fail and the sub disappear.
		if blockTakers[tok.Text] {
			if blk := p.block(cur.Next); blk.OK {
				cur = parse.Combine(cur, blk)
			}
		}
		// "open my $fh, ..." and "print our $x, ..." declare in place, and the
		// list continues after a comma.
		if d := p.expectText(cur.Next, "my", "our", "local", "state"); d.OK {
			item := p.declItem(d.Next)
			if !item.OK {
				return parse.Fail
			}
			cur = parse.Combine(cur, parse.Combine(d, item))
			if comma := p.keepIf(cur.Next, ","); comma.OK {
				if rest := p.argSeq(comma.Next); rest.OK {
					return parse.Combine(cur, parse.Combine(comma, rest))
				}
			}
			return cur
		}
		if args := p.parenArgs(cur.Next); args.OK {
			return parse.Combine(cur, args)
		}
		if args := p.argSeq(cur.Next); args.OK {
			cur = parse.Combine(cur, args)
		}
		return cur

	default:
		return parse.Fail
	}
}

// blockTakers are the list operators that may take a block before their list.
//
// Only these three do. Letting every list operator try a block made
// "bless {}, shift" parse as a block call, which failed, which made the whole
// "sub new { ... }" statement fail and the sub vanish from the output.
var blockTakers = map[string]bool{
	"map": true, "grep": true, "sort": true,
}

// listOps is the set of built-in list operators that may be called without
// parentheses, as Perl allows.
var listOps = map[string]bool{
	"print": true, "say": true, "printf": true, "sprintf": true,
	"die": true, "warn": true, "return": true, "push": true, "pop": true,
	"shift": true, "unshift": true, "splice": true, "split": true,
	"join": true, "keys": true, "values": true, "each": true,
	"sort": true, "reverse": true, "map": true, "grep": true,
	"defined": true, "undef": true, "scalar": true, "ref": true,
	"chomp": true, "chop": true, "lc": true, "uc": true, "lcfirst": true,
	"ucfirst": true, "length": true, "substr": true, "index": true,
	"rindex": true, "open": true, "close": true, "read": true,
	"write": true, "bless": true, "exists": true, "delete": true,
	"int": true, "abs": true, "sqrt": true, "rand": true, "srand": true,
	"time": true, "localtime": true, "gmtime": true, "sleep": true,
	"exit": true, "wantarray": true,
	"eval": true,
}

// parenExpr parses "( ... )", which in Perl is a list rather than a single
// expression: "( 1, 2, 3 )" and "( a => 1, b => 2 )" are both ordinary.
func (p *parser) parenExpr(pos int) parse.Result {
	if open := p.keepIf(pos, "("); open.OK {
		empty := parse.Fail
		if close := p.keepIf(open.Next, ")"); close.OK {
			empty = parse.Combine(open, close)
		}
		withList := parse.Fail
		if list := p.argSeq(open.Next); list.OK {
			if close := p.expectText(list.Next, ")"); close.OK {
				withList = parse.Combine(open, parse.Combine(list, close))
			}
		}
		return parse.Pick(empty, withList)
	}
	return parse.Fail
}

// parenArgs parses a parenthesised argument list written against the callee.
func (p *parser) parenArgs(pos int) parse.Result {
	i := p.peekStmt(pos)
	if i < 0 || p.Toks[i].Text != "(" {
		return parse.Fail
	}
	// Require adjacency, so that "name (something)" in prose is not read as a
	// call.
	if !p.adjacent(pos) {
		return parse.Fail
	}
	open := p.keepAt(i)
	empty := parse.Fail
	if close := p.keepIf(open.Next, ")"); close.OK {
		empty = parse.Combine(open, close)
	}
	withArgs := parse.Fail
	if args := p.argSeq(open.Next); args.OK {
		if close := p.expectText(args.Next, ")"); close.OK {
			withArgs = parse.Combine(open, parse.Combine(args, close))
		}
	}
	return parse.Pick(empty, withArgs)
}

// atCloser reports whether the next significant token closes a group.
func (p *parser) atCloser(pos int) bool {
	i := p.peek(pos)
	if i < 0 {
		return true
	}
	switch p.Toks[i].Text {
	case ")", "}", "]":
		return true
	}
	return false
}

// argSeq parses a comma or fat-comma separated list without surrounding
// brackets. A trailing separator before a closer is allowed, as in
// "( 1, 2, )".
func (p *parser) argSeq(pos int) parse.Result {
	return p.Memo(slotArgs, pos, false, func() parse.Result {
		i := p.peekStmt(pos)
		if i < 0 {
			return parse.Fail
		}
		cur := p.expression(i)
		if !cur.OK {
			return parse.Fail
		}
		for {
			comma := p.keepIf(cur.Next, ",", "=>")
			if !comma.OK {
				break
			}
			next := p.expression(comma.Next)
			if !next.OK {
				if p.atCloser(comma.Next) {
					cur = parse.Combine(cur, comma)
				}
				break
			}
			cur = parse.Combine(cur, parse.Combine(comma, next))
		}
		return cur
	})
}

func (p *parser) arrayRef(pos int) parse.Result {
	if open := p.keepIf(pos, "["); open.OK {
		empty := parse.Fail
		if close := p.keepIf(open.Next, "]"); close.OK {
			empty = parse.Combine(open, close)
		}
		withArgs := parse.Fail
		if args := p.argSeq(open.Next); args.OK {
			if close := p.expectText(args.Next, "]"); close.OK {
				withArgs = parse.Combine(open, parse.Combine(args, close))
			}
		}
		return parse.Pick(empty, withArgs)
	}
	return parse.Fail
}

func (p *parser) hashRef(pos int) parse.Result {
	if open := p.keepIf(pos, "{"); open.OK {
		empty := parse.Fail
		if close := p.keepIf(open.Next, "}"); close.OK {
			empty = parse.Combine(open, close)
		}
		withPairs := parse.Fail
		if pairs := p.argSeq(open.Next); pairs.OK {
			if close := p.expectText(pairs.Next, "}"); close.OK {
				withPairs = parse.Combine(open, parse.Combine(pairs, close))
			}
		}
		return parse.Pick(empty, withPairs)
	}
	return parse.Fail
}

func (p *parser) anonSub(pos int) parse.Result {
	kw := p.expectText(pos, "sub")
	if !kw.OK {
		return parse.Fail
	}
	cur := kw
	if open := p.peekStmt(cur.Next); open >= 0 && p.Toks[open].Text == "(" {
		proto := p.balanced(cur.Next, "(", ")")
		if !proto.OK {
			return parse.Fail
		}
		cur = parse.Combine(cur, proto)
	}
	blk := p.block(cur.Next)
	if !blk.OK {
		return parse.Fail
	}
	return parse.Combine(cur, blk)
}

func (p *parser) doBlock(pos int) parse.Result {
	kw := p.expectText(pos, "do", "eval")
	if !kw.OK {
		return parse.Fail
	}
	blk := p.block(kw.Next)
	if !blk.OK {
		return parse.Fail
	}
	return parse.Combine(kw, blk)
}
