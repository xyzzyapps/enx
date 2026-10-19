package perl

import (
	"enx/internal/host"
	"enx/internal/host/parse"
)

// Machine is the Perl grammar, used as the hard constraint that a code
// subsequence must satisfy.
//
// The grammar models Perl with "use strict" in force, and that is the whole
// trick. Under strict subs a bareword is a compile error, so a function call
// needs parentheses, or a leading sigil, or a name on the built-in list-operator
// list. The grammar enforces exactly that, which means ordinary English —
// "we set the first amount" — cannot be parsed as code at all and is dropped,
// without any lexical evidence being needed.
type Machine struct{}

// NewMachine returns a new Perl grammar machine.
func NewMachine() *Machine { return &Machine{} }

// Language returns "perl".
func (m *Machine) Language() string { return "perl" }

// switchCost is charged once for each maximal run of English.
const switchCost = 6.0

func (p *parser) statement(pos int) parse.Result {
	return p.Memo(slotStatement, pos, false, func() parse.Result {
		best := p.altAt(pos, p.stmtForms...)
		if !best.OK {
			return parse.Fail
		}
		// A bare control keyword may still take an operand, so it is not a
		// statement end: a region on the same line must not gain a newline.
		// An explicit semicolon closes the statement and is recorded instead.
		if last := best.LastKept(); last >= 0 && !p.bareControlStatement(best) {
			ends := make([]int, 0, len(best.Ends)+1)
			ends = append(ends, best.Ends...)
			ends = append(ends, last)
			best.Ends = ends
		}
		return best
	})
}

// bareControlStatement reports whether the statement is a single control
// keyword that may still take an operand.
func (p *parser) bareControlStatement(r parse.Result) bool {
	if len(r.Kept) != 1 {
		return false
	}
	switch p.Toks[r.Kept[0]].Text {
	case "return", "last", "next", "redo", "goto", "die", "warn":
		return true
	}
	return false
}

// useStmt covers "use NAME;", "use NAME VERSION;", "use NAME (LIST);" and
// "no NAME;".
func (p *parser) useStmt(pos int) parse.Result {
	return p.Memo(slotUse, pos, false, func() parse.Result {
		kw := p.expectText(pos, "use", "no", "require")
		if !kw.OK {
			return parse.Fail
		}
		cur := kw
		name := p.expect(cur.Next, func(i int) bool {
			tok := p.Toks[i]
			return tok.Kind == host.Constant || tok.Kind == host.Identifier ||
				tok.Kind == host.String || tok.Kind == host.Variable
		})
		if !name.OK {
			return parse.Fail
		}
		cur = parse.Combine(cur, name)

		// Optional version, given as a number.
		if v := p.expectKind(cur.Next, host.Int, host.Float); v.OK {
			cur = parse.Combine(cur, v)
		}
		if open := p.peekStmt(cur.Next); open >= 0 && p.Toks[open].Text == "(" {
			args := p.balanced(cur.Next, "(", ")")
			if !args.OK {
				return parse.Fail
			}
			cur = parse.Combine(cur, args)
		}
		semi := p.optSemi(cur.Next)
		if !semi.OK {
			return parse.Fail
		}
		return parse.Combine(cur, semi)
	})
}

func (p *parser) packageStmt(pos int) parse.Result {
	return p.Memo(slotPackage, pos, false, func() parse.Result {
		kw := p.expectText(pos, "package")
		if !kw.OK {
			return parse.Fail
		}
		cur := kw
		for {
			part := p.expectKind(cur.Next, host.Constant, host.Identifier)
			if !part.OK {
				break
			}
			cur = parse.Combine(cur, part)
			if sep := p.keepIf(cur.Next, "::"); sep.OK {
				cur = parse.Combine(cur, sep)
				continue
			}
			break
		}
		if cur.Next == kw.Next {
			return parse.Fail
		}
		if v := p.expectKind(cur.Next, host.Int, host.Float); v.OK {
			cur = parse.Combine(cur, v)
		}
		// Block package syntax: "package Foo { ... }".
		if open := p.peekStmt(cur.Next); open >= 0 && p.Toks[open].Text == "{" {
			blk := p.block(cur.Next)
			if !blk.OK {
				return parse.Fail
			}
			return parse.Combine(cur, blk)
		}
		semi := p.optSemi(cur.Next)
		if !semi.OK {
			return parse.Fail
		}
		return parse.Combine(cur, semi)
	})
}

// declStmt covers "my $x = 1;", "my ($a, $b) = @_;", "our @list;" and the
// local and state forms.
func (p *parser) declStmt(pos int) parse.Result {
	return p.Memo(slotDecl, pos, false, func() parse.Result {
		kw := p.expectText(pos, "my", "our", "local", "state")
		if !kw.OK {
			return parse.Fail
		}
		cur := kw

		if open := p.peekStmt(cur.Next); open >= 0 && p.Toks[open].Text == "(" {
			openTok := p.keepAt(open)
			list := parse.Result{OK: true, Next: openTok.Next}
			for {
				item := p.declItem(list.Next)
				if !item.OK {
					return parse.Fail
				}
				if list.Next == openTok.Next {
					list = item
				} else {
					list = parse.Combine(list, item)
				}
				comma := p.keepIf(list.Next, ",")
				if !comma.OK {
					break
				}
				list = parse.Combine(list, comma)
			}
			close := p.expectText(list.Next, ")")
			if !close.OK {
				return parse.Fail
			}
			cur = parse.Combine(cur, parse.Combine(openTok, parse.Combine(list, close)))
		} else {
			item := p.declItem(cur.Next)
			if !item.OK {
				return parse.Fail
			}
			cur = parse.Combine(cur, item)
			for {
				comma := p.keepIf(cur.Next, ",")
				if !comma.OK {
					break
				}
				next := p.declItem(comma.Next)
				if !next.OK {
					break
				}
				cur = parse.Combine(cur, parse.Combine(comma, next))
			}
		}

		if eq := p.keepIf(cur.Next, "="); eq.OK {
			rhs := p.expression(eq.Next)
			if !rhs.OK {
				return parse.Fail
			}
			cur = parse.Combine(cur, parse.Combine(eq, rhs))
		}
		semi := p.optSemi(cur.Next)
		if !semi.OK {
			return parse.Fail
		}
		return parse.Combine(cur, semi)
	})
}

// declItem is one declared variable with an optional initialiser.
func (p *parser) declItem(pos int) parse.Result {
	v := p.expect(pos, func(i int) bool {
		tok := p.Toks[i]
		return tok.Kind == host.Variable || tok.Kind == host.Identifier || tok.Kind == host.Constant
	})
	if !v.OK {
		return parse.Fail
	}
	cur := v
	if eq := p.keepIf(cur.Next, "="); eq.OK {
		rhs := p.expression(eq.Next)
		if !rhs.OK {
			return parse.Fail
		}
		cur = parse.Combine(cur, parse.Combine(eq, rhs))
	}
	return cur
}

func (p *parser) subDecl(pos int) parse.Result {
	return p.Memo(slotSub, pos, false, func() parse.Result {
		kw := p.expectText(pos, "sub")
		if !kw.OK {
			return parse.Fail
		}
		cur := kw
		name := p.expectKind(cur.Next, host.Identifier, host.Constant)
		if !name.OK {
			return parse.Fail
		}
		cur = parse.Combine(cur, name)

		// A signature or prototype, consumed whole: it is code and needs no
		// analysis.
		if open := p.peekStmt(cur.Next); open >= 0 && p.Toks[open].Text == "(" {
			proto := p.balanced(cur.Next, "(", ")")
			if !proto.OK {
				return parse.Fail
			}
			cur = parse.Combine(cur, proto)
		}

		// Subroutine attributes: "sub foo :lvalue { ... }".
		for {
			colon := p.keepIf(cur.Next, ":")
			if !colon.OK {
				break
			}
			attr := p.expectKind(colon.Next, host.Identifier, host.Constant)
			if !attr.OK {
				return parse.Fail
			}
			cur = parse.Combine(cur, parse.Combine(colon, attr))
		}

		// A forward declaration: "sub name;"
		if semi := p.keepIf(cur.Next, ";"); semi.OK {
			return parse.Combine(cur, semi)
		}

		blk := p.block(cur.Next)
		if !blk.OK {
			return parse.Fail
		}
		return parse.Combine(cur, blk)
	})
}

func (p *parser) ifStmt(pos int) parse.Result {
	return p.Memo(slotIf, pos, false, func() parse.Result { return p.ifLike(pos, "if") })
}

func (p *parser) unlessStmt(pos int) parse.Result {
	return p.Memo(slotUnless, pos, false, func() parse.Result { return p.ifLike(pos, "unless") })
}

func (p *parser) ifLike(pos int, kwText string) parse.Result {
	kw := p.expectText(pos, kwText)
	if !kw.OK {
		return parse.Fail
	}
	cond := p.parenExpr(kw.Next)
	if !cond.OK {
		return parse.Fail
	}
	cur := parse.Combine(kw, cond)

	blk := p.block(cur.Next)
	if !blk.OK {
		return parse.Fail
	}
	cur = parse.Combine(cur, blk)

	for {
		elsif := p.expectText(cur.Next, "elsif")
		if !elsif.OK {
			break
		}
		c2 := p.parenExpr(elsif.Next)
		if !c2.OK {
			break
		}
		b2 := p.block(c2.Next)
		if !b2.OK {
			break
		}
		cur = parse.Combine(cur, parse.Combine(elsif, parse.Combine(c2, b2)))
	}
	if els := p.expectText(cur.Next, "else"); els.OK {
		blk2 := p.block(els.Next)
		if !blk2.OK {
			return parse.Fail
		}
		cur = parse.Combine(cur, parse.Combine(els, blk2))
	}
	// An optional terminator may follow a block statement.
	semi := p.optSemi(cur.Next)
	if semi.OK {
		cur = parse.Combine(cur, semi)
	}
	return cur
}

// givenStmt covers Perl's given/when switch form. The semantics are not
// evaluated here; the grammar only needs to keep the construct and its blocks
// together.
func (p *parser) givenStmt(pos int) parse.Result {
	return p.Memo(slotGiven, pos, false, func() parse.Result {
		kw := p.expectText(pos, "given")
		if !kw.OK {
			return parse.Fail
		}
		cond := p.parenExpr(kw.Next)
		if !cond.OK {
			return parse.Fail
		}
		blk := p.block(cond.Next)
		if !blk.OK {
			return parse.Fail
		}
		return parse.Combine(kw, parse.Combine(cond, blk))
	})
}

// whenStmt is the arm form used inside given.
func (p *parser) whenStmt(pos int) parse.Result {
	return p.Memo(slotWhen, pos, false, func() parse.Result {
		kw := p.expectText(pos, "when")
		if !kw.OK {
			return parse.Fail
		}
		cond := p.parenExpr(kw.Next)
		if !cond.OK {
			return parse.Fail
		}
		blk := p.block(cond.Next)
		if !blk.OK {
			return parse.Fail
		}
		return parse.Combine(kw, parse.Combine(cond, blk))
	})
}

func (p *parser) whileStmt(pos int) parse.Result {
	return p.Memo(slotWhile, pos, false, func() parse.Result {
		kw := p.expectText(pos, "while", "until")
		if !kw.OK {
			return parse.Fail
		}
		cond := p.parenExpr(kw.Next)
		if !cond.OK {
			return parse.Fail
		}
		cur := parse.Combine(kw, cond)
		blk := p.block(cur.Next)
		if !blk.OK {
			return parse.Fail
		}
		cur = parse.Combine(cur, blk)
		if cont := p.expectText(cur.Next, "continue"); cont.OK {
			blk2 := p.block(cont.Next)
			if !blk2.OK {
				return parse.Fail
			}
			cur = parse.Combine(cur, parse.Combine(cont, blk2))
		}
		semi := p.optSemi(cur.Next)
		if semi.OK {
			cur = parse.Combine(cur, semi)
		}
		return cur
	})
}

// forStmt covers both the C-style loop and the list form, since Perl accepts
// either after "for" or "foreach".
func (p *parser) forStmt(pos int) parse.Result {
	return p.Memo(slotFor, pos, false, func() parse.Result { return p.forLike(pos, "for") })
}

func (p *parser) foreachStmt(pos int) parse.Result {
	return p.Memo(slotForeach, pos, false, func() parse.Result { return p.forLike(pos, "foreach") })
}

func (p *parser) forLike(pos int, kwText string) parse.Result {
	kw := p.expectText(pos, kwText)
	if !kw.OK {
		return parse.Fail
	}
	cur := kw

	// Optional loop variable: "foreach my $x (@list)" or "foreach $x (@list)".
	declared := false
	if d := p.expectText(cur.Next, "my", "our", "state"); d.OK {
		cur = parse.Combine(cur, d)
		declared = true
	}
	if v := p.peekStmt(cur.Next); v >= 0 && p.Toks[v].Kind == host.Variable {
		cur = parse.Combine(cur, p.keepAt(v))
	} else if declared {
		return parse.Fail
	}

	if open := p.peekStmt(cur.Next); open >= 0 && p.Toks[open].Text == "(" {
		group := p.balanced(cur.Next, "(", ")")
		if !group.OK {
			return parse.Fail
		}
		cur = parse.Combine(cur, group)
	} else {
		list := p.expression(cur.Next)
		if !list.OK {
			return parse.Fail
		}
		cur = parse.Combine(cur, list)
	}

	blk := p.block(cur.Next)
	if !blk.OK {
		return parse.Fail
	}
	cur = parse.Combine(cur, blk)
	semi := p.optSemi(cur.Next)
	if semi.OK {
		cur = parse.Combine(cur, semi)
	}
	return cur
}

func (p *parser) blockStmt(pos int) parse.Result {
	return p.Memo(slotBlockStmt, pos, false, func() parse.Result { return p.block(pos) })
}

// labelStmt covers "LOOP: while (...) { ... }".
func (p *parser) labelStmt(pos int) parse.Result {
	return p.Memo(slotLabel, pos, false, func() parse.Result {
		i := p.peekStmt(pos)
		if i < 0 {
			return parse.Fail
		}
		tok := p.Toks[i]
		if tok.Kind != host.Constant && tok.Kind != host.Identifier {
			return parse.Fail
		}
		name := p.keepAt(i)
		colon := p.expectText(name.Next, ":")
		if !colon.OK {
			return parse.Fail
		}
		st := p.statement(colon.Next)
		if !st.OK {
			return parse.Fail
		}
		return parse.Combine(name, parse.Combine(colon, st))
	})
}

// controlHead parses a control keyword with its optional operand but without
// its terminator, so that a modifier may follow it.
func (p *parser) controlHead(pos int) parse.Result {
	kw := p.expectText(pos, "return", "last", "next", "redo", "goto", "die", "warn")
	if !kw.OK {
		return parse.Fail
	}
	if arg := p.expression(kw.Next); arg.OK {
		return parse.Combine(kw, arg)
	}
	// "next OUTER" and friends take a label, which is a bare name.
	if label := p.expectKind(kw.Next, host.Constant, host.Identifier); label.OK {
		return parse.Combine(kw, label)
	}
	return kw
}

func (p *parser) controlStmt(pos int) parse.Result {
	return p.Memo(slotControl, pos, false, func() parse.Result {
		head := p.controlHead(pos)
		if !head.OK {
			return parse.Fail
		}
		semi := p.optSemi(head.Next)
		if !semi.OK {
			return parse.Fail
		}
		return parse.Combine(head, semi)
	})
}

// modifierStmt handles "STATEMENT if EXPR;", which Perl allows after any
// statement, including after a control keyword.
func (p *parser) modifierStmt(pos int) parse.Result {
	return p.Memo(slotModifier, pos, false, func() parse.Result {
		head := p.altAt(pos, p.expression, p.controlHead)
		if !head.OK {
			return parse.Fail
		}
		kw := p.expectText(head.Next, "if", "unless", "while", "until", "for", "foreach")
		if !kw.OK {
			return parse.Fail
		}
		cond := p.expression(kw.Next)
		if !cond.OK {
			return parse.Fail
		}
		cur := parse.Combine(head, parse.Combine(kw, cond))
		semi := p.optSemi(cur.Next)
		if !semi.OK {
			return parse.Fail
		}
		return parse.Combine(cur, semi)
	})
}

func (p *parser) exprStmt(pos int) parse.Result {
	expr := p.expression(pos)
	if !expr.OK {
		return parse.Fail
	}
	semi := p.optSemi(expr.Next)
	if !semi.OK {
		return parse.Fail
	}
	return parse.Combine(expr, semi)
}

// block parses "{ statements }".
func (p *parser) block(pos int) parse.Result {
	return p.Memo(slotBlockBody, pos, false, func() parse.Result {
		i := p.peekStmt(pos)
		if i < 0 || p.Toks[i].Text != "{" {
			return parse.Fail
		}
		open := p.keepAt(i)
		inner := p.body(open.Next, map[string]bool{"}": true})
		close := p.expectText(inner.Next, "}")
		if !close.OK {
			return parse.Fail
		}
		return parse.Combine(open, parse.Combine(inner, close))
	})
}
