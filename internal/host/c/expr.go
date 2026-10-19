package c

import (
	"enx/internal/host"
	"enx/internal/host/parse"
)

// expression is the entry point for expressions.
func (p *parser) expression(pos int) parse.Result {
	return p.Memo(slotExpression, pos, false, func() parse.Result { return p.commaExpr(pos) })
}

// constExpr is the restricted form C allows in a case label, an array bound or
// an enumerator value: no assignment and no comma.
func (p *parser) constExpr(pos int) parse.Result {
	return p.conditionalExpr(pos)
}

func (p *parser) commaExpr(pos int) parse.Result {
	cur := p.assignExpr(pos)
	if !cur.OK {
		return parse.Fail
	}
	for {
		comma := p.keepIf(cur.Next, ",")
		if !comma.OK {
			break
		}
		right := p.assignExpr(comma.Next)
		if !right.OK {
			break
		}
		cur = parse.Combine(cur, parse.Combine(comma, right))
	}
	return cur
}

var assignOps = []string{
	"=", "+=", "-=", "*=", "/=", "%=", "<<=", ">>=", "&=", "^=", "|=",
}

func (p *parser) assignExpr(pos int) parse.Result {
	left := p.conditionalExpr(pos)
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

func (p *parser) conditionalExpr(pos int) parse.Result {
	cond := p.binaryChain(pos, 0)
	if !cond.OK {
		return parse.Fail
	}
	q := p.keepIf(cond.Next, "?")
	if !q.OK {
		return cond
	}
	thenArm := p.expression(q.Next)
	if !thenArm.OK {
		return cond
	}
	colon := p.keepIf(thenArm.Next, ":")
	if !colon.OK {
		return cond
	}
	elseArm := p.conditionalExpr(colon.Next)
	if !elseArm.OK {
		return cond
	}
	return parse.Combine(cond, parse.Combine(q, parse.Combine(thenArm, parse.Combine(colon, elseArm))))
}

// binLevels lists binary operators from loosest to tightest, which is C's
// precedence almost exactly. Nothing is executed, so only acceptance matters.
var binLevels = [][]string{
	{"||"},
	{"&&"},
	{"|"},
	{"^"},
	{"&"},
	{"==", "!="},
	{"<", ">", "<=", ">="},
	{"<<", ">>"},
	{"+", "-"},
	{"*", "/", "%"},
}

func (p *parser) binaryChain(pos, level int) parse.Result {
	if level >= len(binLevels) {
		return p.castExpr(pos)
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

// castExpr is the classic C ambiguity: "( name ) x" is a cast when name is a
// type and a parenthesised expression otherwise. Both readings are built and
// the better one wins; since both keep the same bytes, the choice only affects
// how far the parse reaches.
func (p *parser) castExpr(pos int) parse.Result {
	return p.Memo(slotCast, pos, false, func() parse.Result {
		asCast := parse.Fail
		if open := p.keepIf(pos, "("); open.OK {
			if tn := p.typeName(open.Next); tn.OK {
				if close := p.keepIf(tn.Next, ")"); close.OK {
					operand := p.castExpr(close.Next)
					if operand.OK {
						asCast = parse.Combine(open, parse.Combine(tn, parse.Combine(close, operand)))
					}
				}
			}
		}
		return parse.Pick(asCast, p.unaryExpr(pos))
	})
}

// typeName is "declaration-specifiers declarator?", used by casts and sizeof.
func (p *parser) typeName(pos int) parse.Result {
	specs := p.declSpecs(pos)
	if !specs.OK {
		return parse.Fail
	}
	if d := p.declarator(specs.Next); d.OK {
		return parse.Combine(specs, d)
	}
	return specs
}

var unaryOperators = map[string]bool{
	"++": true, "--": true, "+": true, "-": true, "!": true,
	"~": true, "*": true, "&": true,
}

func (p *parser) unaryExpr(pos int) parse.Result {
	i := p.peek(pos)
	if i >= 0 {
		tok := p.Toks[i]
		// sizeof is special: it may take a parenthesised type name.
		if tok.Kind == host.Keyword && tok.Text == "sizeof" {
			head := p.keepAt(i)
			if open := p.keepIf(head.Next, "("); open.OK {
				if tn := p.typeName(open.Next); tn.OK {
					if close := p.keepIf(tn.Next, ")"); close.OK {
						return parse.Combine(head, parse.Combine(open, parse.Combine(tn, close)))
					}
				}
			}
			if operand := p.unaryExpr(head.Next); operand.OK {
				return parse.Combine(head, operand)
			}
			return head
		}
		if tok.Kind == host.Operator && unaryOperators[tok.Text] {
			pre := p.keepAt(i)
			if operand := p.castExpr(pre.Next); operand.OK {
				return parse.Combine(pre, operand)
			}
		}
	}
	return p.postfixExpr(pos)
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

// suffix is one postfix operation: a subscript, a call, a member access, or an
// increment.
func (p *parser) suffix(pos int) parse.Result {
	if open := p.keepIf(pos, "["); open.OK {
		inner := p.expression(open.Next)
		close := p.keepIf(inner.Next, "]")
		if !inner.OK || !close.OK {
			return parse.Fail
		}
		return parse.Combine(open, parse.Combine(inner, close))
	}
	if open := p.keepIf(pos, "("); open.OK {
		empty := parse.Fail
		if close := p.keepIf(open.Next, ")"); close.OK {
			empty = parse.Combine(open, close)
		}
		withArgs := parse.Fail
		if args := p.argList(open.Next); args.OK {
			if close := p.keepIf(args.Next, ")"); close.OK {
				withArgs = parse.Combine(open, parse.Combine(args, close))
			}
		}
		return parse.Pick(empty, withArgs)
	}
	if op := p.keepIf(pos, ".", "->"); op.OK {
		name := p.expectKind(op.Next, host.Identifier)
		if !name.OK {
			return parse.Fail
		}
		return parse.Combine(op, name)
	}
	if inc := p.keepIf(pos, "++", "--"); inc.OK {
		return inc
	}
	return parse.Fail
}

func (p *parser) argList(pos int) parse.Result {
	cur := p.assignExpr(pos)
	if !cur.OK {
		return parse.Fail
	}
	for {
		comma := p.keepIf(cur.Next, ",")
		if !comma.OK {
			break
		}
		next := p.assignExpr(comma.Next)
		if !next.OK {
			break
		}
		cur = parse.Combine(cur, parse.Combine(comma, next))
	}
	return cur
}

func (p *parser) primaryExpr(pos int) parse.Result {
	return p.Memo(slotPrimary, pos, false, func() parse.Result {
		return p.altAt(pos, p.primForms...)
	})
}

// parenHead is "( expression )", the parenthesised head of if, while, for and
// switch, and also a parenthesised expression in primary position.
func (p *parser) parenHead(pos int) parse.Result {
	open := p.keepIf(pos, "(")
	if !open.OK {
		return parse.Fail
	}
	inner := p.expression(open.Next)
	if !inner.OK {
		return parse.Fail
	}
	close := p.keepIf(inner.Next, ")")
	if !close.OK {
		return parse.Fail
	}
	return parse.Combine(open, parse.Combine(inner, close))
}

// genericExpr consumes a C11 _Generic selection. The association list is kept
// whole; no type checking is attempted.
func (p *parser) genericExpr(pos int) parse.Result {
	kw := p.expectText(pos, "_Generic")
	if !kw.OK {
		return parse.Fail
	}
	args := p.balanced(kw.Next, "(", ")")
	if !args.OK {
		return parse.Fail
	}
	return parse.Combine(kw, args)
}

// compoundLiteral covers "(type){ initializer }".
func (p *parser) compoundLiteral(pos int) parse.Result {
	open := p.keepIf(pos, "(")
	if !open.OK {
		return parse.Fail
	}
	tn := p.typeName(open.Next)
	if !tn.OK {
		return parse.Fail
	}
	close := p.keepIf(tn.Next, ")")
	if !close.OK {
		return parse.Fail
	}
	init := p.initializer(close.Next)
	if !init.OK {
		return parse.Fail
	}
	return parse.Combine(open, parse.Combine(tn, parse.Combine(close, init)))
}

func (p *parser) identExpr(pos int) parse.Result {
	i := p.peek(pos)
	if i < 0 {
		return parse.Fail
	}
	switch p.Toks[i].Kind {
	case host.Identifier, host.Constant:
		return p.keepAt(i)
	}
	return parse.Fail
}

func (p *parser) literalExpr(pos int) parse.Result {
	i := p.peek(pos)
	if i < 0 {
		return parse.Fail
	}
	switch p.Toks[i].Kind {
	case host.Int, host.Float, host.Char, host.Anaphor:
		// An anaphor is a value, so it belongs wherever a literal does.
		return p.keepAt(i)
	}
	return parse.Fail
}

// stringExpr consumes one or more adjacent string literals, which C
// concatenates at compile time.
func (p *parser) stringExpr(pos int) parse.Result {
	i := p.peek(pos)
	if i < 0 || p.Toks[i].Kind != host.String {
		return parse.Fail
	}
	cur := p.keepAt(i)
	for {
		j := p.peek(cur.Next)
		if j < 0 || p.Toks[j].Kind != host.String {
			break
		}
		cur = parse.Combine(cur, p.keepAt(j))
	}
	return cur
}
