package ruby

import (
	"enx/internal/host"
	"enx/internal/host/parse"
)

// expression is the entry point for expressions.
func (p *parser) expression(pos int) parse.Result {
	return p.Memo(prodExpr, pos, false, func() parse.Result { return p.assignExpr(pos) })
}

var assignOps = []string{
	"=", "+=", "-=", "*=", "/=", "%=", "**=", "||=", "&&=", "|=", "&=",
	"^=", "<<=", ">>=",
}

// assignExpr handles assignment, which in Ruby is an expression and is
// right-associative.
func (p *parser) assignExpr(pos int) parse.Result {
	left := p.ternaryExpr(pos)
	if !left.OK {
		return parse.Fail
	}
	op := p.expectLineText(left.Next, assignOps...)
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
	q := p.expectLineText(cond.Next, "?")
	if !q.OK {
		return cond
	}
	thenArm := p.ternaryExpr(q.Next)
	if !thenArm.OK {
		return cond
	}
	colon := p.expectLineText(thenArm.Next, ":")
	if !colon.OK {
		return cond
	}
	elseArm := p.ternaryExpr(colon.Next)
	if !elseArm.OK {
		return cond
	}
	return parse.Combine(cond, parse.Combine(q, parse.Combine(thenArm, parse.Combine(colon, elseArm))))
}

// binLevels lists binary operators from loosest to tightest. Ruby's exact
// precedence is approximated: acceptance is what matters here, not evaluation
// order, because nothing is executed.
var binLevels = [][]string{
	{"||", "or"},
	{"&&", "and"},
	{"..", "..."},
	{"==", "!=", "===", "=~", "!~", "<=>"},
	{"<", ">", "<=", ">="},
	{"|", "^"},
	{"&"},
	{"<<", ">>"},
	{"+", "-"},
	{"*", "/", "%"},
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
		op := p.expectLineText(left.Next, binLevels[level]...)
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
	"!": true, "~": true, "-": true, "+": true, "not": true,
	"*": true, "**": true, "&": true,
}

func (p *parser) unaryExpr(pos int) parse.Result {
	if i := p.peekLine(pos); i >= 0 {
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
	op := p.expectLineText(left.Next, "**")
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
		r := p.Memo(prodSuffix, at, false, func() parse.Result { return p.suffix(at) })
		if !r.OK || r.Next <= cur.Next {
			break
		}
		cur = parse.Combine(cur, r)
	}
	return cur
}

func (p *parser) primaryExpr(pos int) parse.Result {
	return p.Memo(prodPrimary, pos, false, func() parse.Result {
		return p.altAt(pos, p.primForms...)
	})
}

// nonPrimaryKeywords are the keywords that can never begin a value, so a
// primary form must refuse them: they are followed by the operand they
// introduce, not by an expression.
var nonPrimaryKeywords = map[string]bool{
	"end": true, "else": true, "elsif": true, "then": true, "when": true,
	"rescue": true, "ensure": true, "do": true, "begin": true, "in": true,
	"if": true, "unless": true, "while": true, "until": true, "for": true,
	"case": true, "def": true, "class": true, "module": true, "return": true,
	"break": true, "next": true, "redo": true, "retry": true, "alias": true,
	"undef": true, "and": true, "or": true, "not": true,
}

func (p *parser) literalExpr(pos int) parse.Result {
	i := p.peek(pos)
	if i < 0 {
		return parse.Fail
	}
	switch p.Toks[i].Kind {
	case host.Int, host.Float, host.String, host.Symbol, host.Regex, host.Heredoc, host.Char, host.Anaphor:
		// An anaphor is a value, so it belongs wherever a literal does.
		return p.keepAt(i)
	}
	return parse.Fail
}

// dispatchExpr covers a name used as a value or as a call, including calls
// without parentheses such as "puts answer".
func (p *parser) dispatchExpr(pos int) parse.Result {
	i := p.peek(pos)
	if i < 0 {
		return parse.Fail
	}
	tok := p.Toks[i]
	switch tok.Kind {
	case host.Identifier, host.Constant, host.Variable, host.Sigil, host.Keyword:
	default:
		return parse.Fail
	}
	if tok.Kind == host.Keyword && nonPrimaryKeywords[tok.Text] {
		return parse.Fail
	}
	cur := p.keepAt(i)
	if args := p.callArgs(cur.Next); args.OK {
		cur = parse.Combine(cur, args)
	}
	if blk := p.block(cur.Next); blk.OK {
		cur = parse.Combine(cur, blk)
	}
	return cur
}

func (p *parser) parenExpr(pos int) parse.Result {
	i := p.peek(pos)
	if i < 0 || p.Toks[i].Text != "(" {
		return parse.Fail
	}
	open := p.keepAt(i)
	inner := p.expression(open.Next)
	if !inner.OK {
		if close := p.expectText(open.Next, ")"); close.OK {
			return parse.Combine(open, close)
		}
		return parse.Fail
	}
	close := p.expectText(inner.Next, ")")
	if !close.OK {
		return parse.Fail
	}
	return parse.Combine(open, parse.Combine(inner, close))
}

func (p *parser) arrayLit(pos int) parse.Result {
	i := p.peek(pos)
	if i < 0 || p.Toks[i].Text != "[" {
		return parse.Fail
	}
	open := p.keepAt(i)

	empty := parse.Fail
	if close := p.expectText(open.Next, "]"); close.OK {
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

func (p *parser) hashLit(pos int) parse.Result {
	i := p.peek(pos)
	if i < 0 || p.Toks[i].Text != "{" {
		return parse.Fail
	}
	open := p.keepAt(i)

	empty := parse.Fail
	if close := p.expectText(open.Next, "}"); close.OK {
		empty = parse.Combine(open, close)
	}
	withPairs := parse.Fail
	if pairs := p.hashPairs(open.Next); pairs.OK {
		if close := p.expectText(pairs.Next, "}"); close.OK {
			withPairs = parse.Combine(open, parse.Combine(pairs, close))
		}
	}
	return parse.Pick(empty, withPairs)
}

func (p *parser) hashPairs(pos int) parse.Result {
	cur := parse.Result{OK: true, Next: pos}
	empty := true
	for {
		one := p.hashPair(cur.Next)
		if !one.OK {
			break
		}
		if empty {
			cur = one
			empty = false
		} else {
			cur = parse.Combine(cur, one)
		}
		comma := p.expectLineText(cur.Next, ",")
		if !comma.OK {
			break
		}
		cur = parse.Combine(cur, comma)
	}
	if empty {
		return parse.Fail
	}
	return cur
}

func (p *parser) hashPair(pos int) parse.Result {
	i := p.peek(pos)
	if i < 0 {
		return parse.Fail
	}
	if p.Toks[i].Kind == host.Identifier || p.Toks[i].Kind == host.Constant ||
		p.Toks[i].Kind == host.String || p.Toks[i].Kind == host.Symbol {
		key := p.keepAt(i)
		if colon := p.expectLineText(key.Next, ":"); colon.OK {
			if val := p.expression(colon.Next); val.OK {
				return parse.Combine(key, parse.Combine(colon, val))
			}
		}
		if arrow := p.expectLineText(key.Next, "=>"); arrow.OK {
			if val := p.expression(arrow.Next); val.OK {
				return parse.Combine(key, parse.Combine(arrow, val))
			}
		}
		return parse.Fail
	}
	key := p.expression(pos)
	if !key.OK {
		return parse.Fail
	}
	arrow := p.expectLineText(key.Next, "=>")
	if !arrow.OK {
		return parse.Fail
	}
	val := p.expression(arrow.Next)
	if !val.OK {
		return parse.Fail
	}
	return parse.Combine(key, parse.Combine(arrow, val))
}

func (p *parser) lambdaLit(pos int) parse.Result {
	i := p.peek(pos)
	if i < 0 || p.Toks[i].Text != "->" {
		return parse.Fail
	}
	cur := p.keepAt(i)
	if open := p.expectLineText(cur.Next, "("); open.OK {
		params := p.paramList(open.Next)
		close := p.expectText(params.Next, ")")
		if !close.OK {
			return parse.Fail
		}
		cur = parse.Combine(cur, parse.Combine(open, parse.Combine(params, close)))
	} else {
		cur = parse.Combine(cur, p.blockParams(cur.Next))
	}
	blk := p.block(cur.Next)
	if !blk.OK {
		return parse.Fail
	}
	return parse.Combine(cur, blk)
}

func (p *parser) yieldExpr(pos int) parse.Result {
	kw := p.expectText(pos, "yield")
	if !kw.OK {
		return parse.Fail
	}
	if args := p.callArgs(kw.Next); args.OK {
		return parse.Combine(kw, args)
	}
	return kw
}

func (p *parser) superExpr(pos int) parse.Result {
	kw := p.expectText(pos, "super")
	if !kw.OK {
		return parse.Fail
	}
	if args := p.callArgs(kw.Next); args.OK {
		return parse.Combine(kw, args)
	}
	return kw
}

// suffix parses one postfix operation: a method call, an index, or a block.
func (p *parser) suffix(pos int) parse.Result {
	if op := p.expectLineText(pos, ".", "&.", "::"); op.OK {
		name := p.expectLineKind(op.Next, host.Identifier, host.Constant, host.Operator)
		if !name.OK {
			return parse.Fail
		}
		cur := parse.Combine(op, name)
		if args := p.callArgs(cur.Next); args.OK {
			cur = parse.Combine(cur, args)
		}
		if blk := p.block(cur.Next); blk.OK {
			cur = parse.Combine(cur, blk)
		}
		return cur
	}

	if open := p.keepIfLineAt(pos, "["); open.OK {
		empty := parse.Fail
		if close := p.expectText(open.Next, "]"); close.OK {
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

	return p.block(pos)
}

// block parses "{ ... }" or "do ... end" attached to a call.
func (p *parser) block(pos int) parse.Result {
	return p.Memo(prodBlock, pos, false, func() parse.Result {
		i := p.peek(pos)
		if i < 0 {
			return parse.Fail
		}
		switch p.Toks[i].Text {
		case "{":
			open := p.keepAt(i)
			cur := parse.Combine(open, p.blockParams(open.Next))
			cur = parse.Combine(cur, p.body(cur.Next, map[string]bool{"}": true}))
			close := p.expectText(cur.Next, "}")
			if !close.OK {
				return parse.Fail
			}
			return parse.Combine(cur, close)
		case "do":
			open := p.keepAt(i)
			cur := parse.Combine(open, p.blockParams(open.Next))
			cur = parse.Combine(cur, p.body(cur.Next, map[string]bool{"end": true}))
			close := p.expectText(cur.Next, "end")
			if !close.OK {
				return parse.Fail
			}
			return parse.Combine(cur, close)
		}
		return parse.Fail
	})
}

// blockParams parses "|a, b|". It always succeeds so that callers can combine
// it unconditionally; with no bar present it consumes nothing.
func (p *parser) blockParams(pos int) parse.Result {
	i := p.peek(pos)
	if i < 0 || p.Toks[i].Text != "|" {
		return parse.Result{OK: true, Next: pos}
	}
	open := p.keepAt(i)
	params := p.paramList(open.Next)
	close := p.expectText(params.Next, "|")
	if !close.OK {
		return parse.Result{OK: true, Next: pos}
	}
	return parse.Combine(open, parse.Combine(params, close))
}

// callArgs parses a parenthesised argument list, or a bare one.
func (p *parser) callArgs(pos int) parse.Result {
	if open := p.keepIfLineAt(pos, "("); open.OK {
		empty := parse.Fail
		if close := p.expectText(open.Next, ")"); close.OK {
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
	return p.argSeq(pos)
}

// atCloser reports whether the next significant token closes a group, or the
// input has ended.
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

// argSeq parses a comma-separated argument list without surrounding brackets.
//
// Arguments begin at the next significant token, so prose is never pulled into
// an argument list: whether to call with arguments at all is a cost decision
// made by the caller. A trailing comma before a closer is allowed.
func (p *parser) argSeq(pos int) parse.Result {
	return p.Memo(prodBareArgs, pos, false, func() parse.Result {
		i := p.peekLine(pos)
		if i < 0 {
			return parse.Fail
		}
		cur := p.expression(i)
		if !cur.OK {
			return parse.Fail
		}
		for {
			comma := p.expectLineText(cur.Next, ",")
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
