package ruby

import (
	"enx/internal/host"
	"enx/internal/host/parse"
)

// Machine is the Ruby grammar, used as the hard constraint that a code
// subsequence must satisfy.
//
// It is a memoised best-cost recursive-descent parser: every production may
// skip tokens it does not need, paying the model's price for calling them
// English, and the parser returns the cheapest parse rather than the first one.
// That is what makes the labelling a weighted decision rather than a guess.
type Machine struct{}

// NewMachine returns a new Ruby grammar machine.
func NewMachine() *Machine { return &Machine{} }

// Language returns "ruby".
func (m *Machine) Language() string { return "ruby" }

// switchCost is charged once for each maximal run of English. Prose comes in
// phrases, so an isolated English token wedged between two pieces of code is
// unlikely, and this term says so.
//
// It is what keeps "puts answer" as one call: keeping a bare word always costs
// CodeBias more than skipping it, so without a switch cost the cheaper reading
// would delete the argument.
const switchCost = 6.0

// statement parses one statement, recording where it ends so that the emitter
// knows where a separator may be inserted.
//
// A trailing semicolon is consumed as code rather than left to be skipped. Ruby
// allows ";" as a statement separator, and dropping it turned "def foo; 1; end"
// into "def foo 1", which is a different and invalid program.
func (p *parser) statement(pos int) parse.Result {
	return p.Memo(prodStatement, pos, false, func() parse.Result {
		best := p.altAt(pos, p.stmtForms...)
		if !best.OK {
			return parse.Fail
		}
		if semi := p.keepSemi(best.Next); semi.OK {
			best = parse.Combine(best, semi)
		}
		// A bare control keyword is a complete statement, but it is also an
		// open operand position: "return   the total   x + y" may have been
		// written as one return. Recording it as a statement end lets the
		// emitter insert a newline where the source had a space, turning the
		// return into a bare one. A semicolon closes it explicitly, so only
		// the no-semicolon case is excluded.
		if last := best.LastKept(); last >= 0 && !p.bareControlStatement(best) {
			ends := make([]int, 0, len(best.Ends)+1)
			ends = append(ends, best.Ends...)
			ends = append(ends, last)
			best.Ends = ends
		}
		return best
	})
}

// bareControlStatement reports whether the statement consists solely of a
// control keyword that may take an operand, such as a bare "return".
func (p *parser) bareControlStatement(r parse.Result) bool {
	if len(r.Kept) != 1 {
		return false
	}
	switch p.Toks[r.Kept[0]].Text {
	case "return", "break", "next", "redo", "retry":
		return true
	}
	return false
}

func (p *parser) exprStmt(pos int) parse.Result {
	r := p.expression(pos)
	if !r.OK {
		return parse.Fail
	}
	if p.endsBeforeComma(r.Next) {
		// A comma cannot follow a complete Ruby statement, so a reading that
		// stops at one is really the left hand side of a multiple assignment,
		// which has its own production.
		//
		// It is only wrong when a multiple assignment actually starts here. In
		// mixed text the comma is far more often prose punctuation, as in
		// "x = 5, and y as five too", and there the statement is perfectly good.
		//
		// This matters because statement forms are chosen on their own cost.
		// "a" alone costs less than the whole of "a, b = 1, 2", so without this
		// guard the cheap prefix wins and the rest of the line is skipped.
		if alt := p.multiAssign(pos); alt.OK {
			return parse.Fail
		}
	}
	return r
}

// endsBeforeComma reports whether the next significant token is a comma.
func (p *parser) endsBeforeComma(pos int) bool {
	i := p.peek(pos)
	return i >= 0 && p.Toks[i].Text == ","
}

// multiAssign handles "a, b = 1, 2" and its splatted forms.
//
// Without it the left hand side parses as one statement and the rest dribbles
// out as further statements, which is valid Ruby and wrong: the two sides are
// one assignment.
func (p *parser) multiAssign(pos int) parse.Result {
	return p.Memo(prodMultiAssign, pos, false, func() parse.Result {
		first := p.mlhs(pos)
		if !first.OK {
			return parse.Fail
		}
		cur := first
		count := 1
		for {
			comma := p.expectLineText(cur.Next, ",")
			if !comma.OK {
				break
			}
			next := p.mlhs(comma.Next)
			if !next.OK {
				break
			}
			cur = parse.Combine(cur, parse.Combine(comma, next))
			count++
		}
		if count < 2 {
			return parse.Fail
		}
		eq := p.expectLineText(cur.Next, "=")
		if !eq.OK {
			return parse.Fail
		}
		rhs := p.exprList(eq.Next)
		if !rhs.OK {
			return parse.Fail
		}
		return parse.Combine(cur, parse.Combine(eq, rhs))
	})
}

// mlhs is one target on the left of a multiple assignment, optionally splatted.
//
// A bare lower-case name is accepted here even when the cost model would rather
// call it English, because a comma list followed by an assignment is the one
// place where a single-letter name is unambiguously a variable: "a, b = 1, 2".
func (p *parser) mlhs(pos int) parse.Result {
	cur := parse.Result{OK: true, Next: pos}
	if star := p.keepIfLineAt(cur.Next, "*"); star.OK {
		cur = parse.Combine(cur, star)
	}
	target := p.postfixExpr(cur.Next)
	if !target.OK {
		return parse.Fail
	}
	return parse.Combine(cur, target)
}

// exprList is a comma-separated run of values, as on the right of a multiple
// assignment. The first may be splatted.
func (p *parser) exprList(pos int) parse.Result {
	cur := parse.Result{OK: true, Next: pos}
	if star := p.keepIfLineAt(cur.Next, "*"); star.OK {
		cur = parse.Combine(cur, star)
	}
	first := p.assignExpr(cur.Next)
	if !first.OK {
		return parse.Fail
	}
	cur = parse.Combine(cur, first)
	for {
		comma := p.expectLineText(cur.Next, ",")
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

// modifierStmt handles "expr if cond" and its siblings, including a control
// keyword head such as "next if x".
func (p *parser) modifierStmt(pos int) parse.Result {
	return p.Memo(prodModifier, pos, false, func() parse.Result {
		head := p.altAt(pos, p.expression, p.controlStmt)
		if !head.OK {
			return parse.Fail
		}
		if p.endsBeforeComma(head.Next) {
			return parse.Fail
		}
		kw := p.expectLineText(head.Next, "if", "unless", "while", "until")
		if !kw.OK {
			return parse.Fail
		}
		cond := p.expression(kw.Next)
		if !cond.OK {
			return parse.Fail
		}
		return parse.Combine(head, parse.Combine(kw, cond))
	})
}

func (p *parser) controlStmt(pos int) parse.Result {
	return p.Memo(prodControl, pos, false, func() parse.Result {
		kw := p.expectText(pos, "return", "break", "next", "redo", "retry")
		if !kw.OK {
			return parse.Fail
		}
		if kw.Next >= len(p.Toks) {
			return kw
		}
		// A control keyword may take a list of values, as in "return a, b".
		if arg := p.exprList(kw.Next); arg.OK {
			return parse.Combine(kw, arg)
		}
		return kw
	})
}

func (p *parser) ifStmt(pos int) parse.Result {
	return p.Memo(prodIf, pos, true, func() parse.Result { return p.ifLike(pos, "if") })
}

func (p *parser) unlessStmt(pos int) parse.Result {
	return p.Memo(prodIf, pos, false, func() parse.Result { return p.ifLike(pos, "unless") })
}

// ifExpr parses an if construct in expression position, such as the right hand
// side of "x = if cond then 1 else 2 end".
//
// It gets its own slot. Sharing prodIf with unlessStmt would let whichever was
// tried first at a position decide the other's answer, which is the exact bug
// class the slot design exists to prevent.
func (p *parser) ifExpr(pos int) parse.Result {
	return p.Memo(prodIfExpr, pos, false, func() parse.Result { return p.ifLike(pos, "if") })
}

func (p *parser) ifLike(pos int, kwText string) parse.Result {
	kw := p.expectText(pos, kwText)
	if !kw.OK {
		return parse.Fail
	}
	cond := p.expression(kw.Next)
	if !cond.OK {
		return parse.Fail
	}
	cur := parse.Combine(kw, cond)
	if then := p.expectText(cur.Next, "then"); then.OK {
		cur = parse.Combine(cur, then)
	}

	terms := map[string]bool{"elsif": true, "else": true, "end": true}
	cur = parse.Combine(cur, p.body(cur.Next, terms))

	for {
		elsif := p.expectText(cur.Next, "elsif")
		if !elsif.OK {
			break
		}
		c2 := p.expression(elsif.Next)
		if !c2.OK {
			break
		}
		cur = parse.Combine(cur, parse.Combine(elsif, c2))
		if then := p.expectText(cur.Next, "then"); then.OK {
			cur = parse.Combine(cur, then)
		}
		cur = parse.Combine(cur, p.body(cur.Next, terms))
	}
	if els := p.expectText(cur.Next, "else"); els.OK {
		cur = parse.Combine(cur, els)
		cur = parse.Combine(cur, p.body(cur.Next, map[string]bool{"end": true}))
	}
	end := p.expectText(cur.Next, "end")
	if !end.OK {
		return parse.Fail
	}
	return parse.Combine(cur, end)
}

func (p *parser) whileStmt(pos int) parse.Result {
	return p.Memo(prodWhile, pos, false, func() parse.Result {
		kw := p.expectText(pos, "while", "until")
		if !kw.OK {
			return parse.Fail
		}
		cond := p.expression(kw.Next)
		if !cond.OK {
			return parse.Fail
		}
		cur := parse.Combine(kw, cond)
		if do := p.expectText(cur.Next, "do"); do.OK {
			cur = parse.Combine(cur, do)
		}
		cur = parse.Combine(cur, p.body(cur.Next, map[string]bool{"end": true}))
		end := p.expectText(cur.Next, "end")
		if !end.OK {
			return parse.Fail
		}
		return parse.Combine(cur, end)
	})
}

func (p *parser) untilStmt(pos int) parse.Result { return p.whileStmt(pos) }

func (p *parser) forStmt(pos int) parse.Result {
	return p.Memo(prodFor, pos, false, func() parse.Result {
		kw := p.expectText(pos, "for")
		if !kw.OK {
			return parse.Fail
		}
		cur := kw
		for {
			v := p.expectKind(cur.Next, host.Identifier, host.Variable)
			if !v.OK {
				return parse.Fail
			}
			cur = parse.Combine(cur, v)
			comma := p.expectLineText(cur.Next, ",")
			if !comma.OK {
				break
			}
			cur = parse.Combine(cur, comma)
		}
		in := p.expectText(cur.Next, "in")
		if !in.OK {
			return parse.Fail
		}
		iter := p.expression(in.Next)
		if !iter.OK {
			return parse.Fail
		}
		cur = parse.Combine(cur, parse.Combine(in, iter))
		if do := p.expectText(cur.Next, "do"); do.OK {
			cur = parse.Combine(cur, do)
		}
		cur = parse.Combine(cur, p.body(cur.Next, map[string]bool{"end": true}))
		end := p.expectText(cur.Next, "end")
		if !end.OK {
			return parse.Fail
		}
		return parse.Combine(cur, end)
	})
}

func (p *parser) caseStmt(pos int) parse.Result { return p.caseExpr(pos) }

func (p *parser) caseExpr(pos int) parse.Result {
	return p.Memo(prodCase, pos, false, func() parse.Result {
		kw := p.expectText(pos, "case")
		if !kw.OK {
			return parse.Fail
		}
		terms := map[string]bool{"when": true, "in": true, "else": true, "end": true}
		cur := kw
		if !p.atTerminator(cur.Next, terms) {
			if subj := p.expression(cur.Next); subj.OK {
				cur = parse.Combine(cur, subj)
			}
		}
		cur = parse.Combine(cur, p.body(cur.Next, terms))

		for {
			when := p.expectText(cur.Next, "when")
			if !when.OK {
				break
			}
			c := when
			for {
				alt := p.expression(c.Next)
				if !alt.OK {
					return parse.Fail
				}
				c = parse.Combine(c, alt)
				comma := p.expectLineText(c.Next, ",")
				if !comma.OK {
					break
				}
				c = parse.Combine(c, comma)
			}
			if then := p.expectText(c.Next, "then"); then.OK {
				c = parse.Combine(c, then)
			}
			cur = parse.Combine(cur, c)
			cur = parse.Combine(cur, p.body(cur.Next, terms))
		}
		// Ruby 3 pattern arms. The pattern itself is accepted as an expression;
		// guards and alternative patterns are deliberately left for a later
		// pass, because guessing at their grammar would be worse than refusing.
		for {
			in := p.expectText(cur.Next, "in")
			if !in.OK {
				break
			}
			pat := p.expression(in.Next)
			if !pat.OK {
				return parse.Fail
			}
			c := parse.Combine(in, pat)
			if then := p.expectText(c.Next, "then"); then.OK {
				c = parse.Combine(c, then)
			}
			cur = parse.Combine(cur, c)
			cur = parse.Combine(cur, p.body(cur.Next, terms))
		}
		if els := p.expectText(cur.Next, "else"); els.OK {
			cur = parse.Combine(cur, els)
			cur = parse.Combine(cur, p.body(cur.Next, map[string]bool{"end": true}))
		}
		end := p.expectText(cur.Next, "end")
		if !end.OK {
			return parse.Fail
		}
		return parse.Combine(cur, end)
	})
}

func (p *parser) beginStmt(pos int) parse.Result { return p.beginExpr(pos) }

func (p *parser) beginExpr(pos int) parse.Result {
	return p.Memo(prodBegin, pos, false, func() parse.Result {
		kw := p.expectText(pos, "begin")
		if !kw.OK {
			return parse.Fail
		}
		terms := map[string]bool{"rescue": true, "else": true, "ensure": true, "end": true}
		cur := parse.Combine(kw, p.body(kw.Next, terms))

		for {
			rescue := p.expectText(cur.Next, "rescue")
			if !rescue.OK {
				break
			}
			c := rescue
			stop := map[string]bool{"then": true, "rescue": true, "else": true, "ensure": true, "end": true}
			for {
				if p.atTerminator(c.Next, stop) {
					break
				}
				alt := p.expression(c.Next)
				if !alt.OK {
					break
				}
				c = parse.Combine(c, alt)
				comma := p.expectLineText(c.Next, ",")
				if !comma.OK {
					break
				}
				c = parse.Combine(c, comma)
			}
			if arrow := p.expectText(c.Next, "=>"); arrow.OK {
				if name := p.expectKind(arrow.Next, host.Identifier, host.Variable); name.OK {
					c = parse.Combine(c, parse.Combine(arrow, name))
				}
			}
			cur = parse.Combine(cur, c)
			cur = parse.Combine(cur, p.body(cur.Next, terms))
		}
		if els := p.expectText(cur.Next, "else"); els.OK {
			cur = parse.Combine(cur, els)
			cur = parse.Combine(cur, p.body(cur.Next, map[string]bool{"ensure": true, "end": true}))
		}
		if ens := p.expectText(cur.Next, "ensure"); ens.OK {
			cur = parse.Combine(cur, ens)
			cur = parse.Combine(cur, p.body(cur.Next, map[string]bool{"end": true}))
		}
		end := p.expectText(cur.Next, "end")
		if !end.OK {
			return parse.Fail
		}
		return parse.Combine(cur, end)
	})
}

func (p *parser) defStmt(pos int) parse.Result {
	return p.Memo(prodDef, pos, false, func() parse.Result {
		kw := p.expectText(pos, "def")
		if !kw.OK {
			return parse.Fail
		}
		cur := kw

		if self := p.expectLineText(cur.Next, "self"); self.OK {
			if dot := p.expectLineText(self.Next, "."); dot.OK {
				cur = parse.Combine(cur, parse.Combine(self, dot))
			}
		} else {
			// A singleton method on an expression: "def obj.foo".
			i := p.peekLine(cur.Next)
			if i >= 0 {
				if kind := p.Toks[i].Kind; kind == host.Identifier || kind == host.Constant || kind == host.Variable {
					if j := p.peekLine(i + 1); j >= 0 && p.Toks[j].Text == "." {
						cur = parse.Combine(cur, parse.Combine(p.keepAt(i), p.keepAt(j)))
					}
				}
			}
		}

		name := p.expectLine(cur.Next, func(i int) bool {
			tok := p.Toks[i]
			return tok.Kind == host.Identifier || tok.Kind == host.Constant ||
				(tok.Kind == host.Operator && isMethodOperator(tok.Text))
		})
		if !name.OK {
			return parse.Fail
		}
		cur = parse.Combine(cur, name)

		// A setter is "foo=", with the equals adjacent to the name. The endless
		// form "def foo = expr" has a space and must not take this path.
		if eq := p.keepAdjacent(cur.Next, "="); eq.OK {
			cur = parse.Combine(cur, eq)
		}

		if open := p.expectLineText(cur.Next, "("); open.OK {
			params := p.paramList(open.Next)
			rparen := p.expectText(params.Next, ")")
			if !rparen.OK {
				return parse.Fail
			}
			cur = parse.Combine(cur, parse.Combine(open, parse.Combine(params, rparen)))
		} else if bare := p.paramList(cur.Next); bare.OK && len(bare.Kept) > 0 {
			cur = parse.Combine(cur, bare)
		}

		// "def foo; body; end" needs the semicolon kept, or the body runs into
		// the parameter list and the method is parsed as taking arguments.
		if semi := p.keepSemi(cur.Next); semi.OK {
			cur = parse.Combine(cur, semi)
		}

		// Endless method definition: "def name = expr".
		if eq := p.expectLineText(cur.Next, "="); eq.OK {
			body := p.expression(eq.Next)
			if !body.OK {
				return parse.Fail
			}
			return parse.Combine(cur, parse.Combine(eq, body))
		}

		terms := map[string]bool{"rescue": true, "else": true, "ensure": true, "end": true}
		cur = parse.Combine(cur, p.body(cur.Next, terms))
		if rescue := p.expectText(cur.Next, "rescue"); rescue.OK {
			cur = parse.Combine(cur, rescue)
			if arrow := p.expectText(cur.Next, "=>"); arrow.OK {
				if name := p.expectKind(arrow.Next, host.Identifier); name.OK {
					cur = parse.Combine(cur, parse.Combine(arrow, name))
				}
			}
			cur = parse.Combine(cur, p.body(cur.Next, terms))
		}
		if ens := p.expectText(cur.Next, "ensure"); ens.OK {
			cur = parse.Combine(cur, ens)
			cur = parse.Combine(cur, p.body(cur.Next, map[string]bool{"end": true}))
		}
		end := p.expectText(cur.Next, "end")
		if !end.OK {
			return parse.Fail
		}
		return parse.Combine(cur, end)
	})
}

func isMethodOperator(text string) bool {
	switch text {
	case "+", "-", "*", "/", "%", "**", "==", "!=", "<", ">", "<=", ">=",
		"<=>", "===", "=~", "!~", "<<", ">>", "[]", "~", "!":
		return true
	}
	return false
}

// paramList parses method or block parameters. It is deliberately permissive:
// parameters carry no decision the emitter needs.
func (p *parser) paramList(pos int) parse.Result {
	cur := parse.Result{OK: true, Next: pos}
	empty := true
	for {
		one := p.param(cur.Next)
		if !one.OK {
			break
		}
		if empty {
			cur = one
			empty = false
		} else {
			cur = parse.Combine(cur, one)
		}
		if comma := p.expectLineText(cur.Next, ","); comma.OK {
			cur = parse.Combine(cur, comma)
			continue
		}
		break
	}
	if empty {
		return parse.Result{OK: true, Next: pos}
	}
	return cur
}

func (p *parser) param(pos int) parse.Result {
	cur := parse.Result{OK: true, Next: pos}
	if pre := p.expectLineText(cur.Next, "*", "**", "&"); pre.OK {
		cur = pre
	}
	if name := p.expectLineKind(cur.Next, host.Identifier, host.Variable, host.Constant); name.OK {
		cur = parse.Combine(cur, name)
	} else if cur.Next == pos {
		return parse.Fail
	}
	if colon := p.expectLineText(cur.Next, ":"); colon.OK {
		cur = parse.Combine(cur, colon)
		if def := p.expression(cur.Next); def.OK {
			cur = parse.Combine(cur, def)
		}
		return cur
	}
	if eq := p.expectLineText(cur.Next, "="); eq.OK {
		def := p.expression(eq.Next)
		if !def.OK {
			return parse.Fail
		}
		return parse.Combine(cur, parse.Combine(eq, def))
	}
	return cur
}

func (p *parser) classStmt(pos int) parse.Result {
	return p.Memo(prodClass, pos, false, func() parse.Result {
		kw := p.expectText(pos, "class")
		if !kw.OK {
			return parse.Fail
		}
		cur := kw
		if shift := p.expectLineText(cur.Next, "<<"); shift.OK {
			recv := p.expectLineKind(shift.Next, host.Keyword, host.Identifier, host.Variable, host.Constant)
			if !recv.OK {
				return parse.Fail
			}
			cur = parse.Combine(cur, parse.Combine(shift, recv))
		} else {
			name := p.expectLineKind(cur.Next, host.Constant)
			if !name.OK {
				return parse.Fail
			}
			cur = parse.Combine(cur, name)
			if path := p.constPath(cur.Next); path.OK {
				cur = parse.Combine(cur, path)
			}
			if lt := p.expectLineText(cur.Next, "<"); lt.OK {
				sup := p.expression(lt.Next)
				if !sup.OK {
					return parse.Fail
				}
				cur = parse.Combine(cur, parse.Combine(lt, sup))
			}
		}
		cur = parse.Combine(cur, p.body(cur.Next, map[string]bool{"end": true}))
		end := p.expectText(cur.Next, "end")
		if !end.OK {
			return parse.Fail
		}
		return parse.Combine(cur, end)
	})
}

func (p *parser) constPath(pos int) parse.Result {
	cur := parse.Result{OK: true, Next: pos}
	for {
		sep := p.expectLineText(cur.Next, "::")
		if !sep.OK {
			break
		}
		name := p.expectLineKind(sep.Next, host.Constant, host.Identifier)
		if !name.OK {
			break
		}
		cur = parse.Combine(cur, parse.Combine(sep, name))
	}
	if cur.Next == pos {
		return parse.Fail
	}
	return cur
}

func (p *parser) moduleStmt(pos int) parse.Result {
	return p.Memo(prodModule, pos, false, func() parse.Result {
		kw := p.expectText(pos, "module")
		if !kw.OK {
			return parse.Fail
		}
		name := p.expectLineKind(kw.Next, host.Constant)
		if !name.OK {
			return parse.Fail
		}
		cur := parse.Combine(kw, name)
		if path := p.constPath(cur.Next); path.OK {
			cur = parse.Combine(cur, path)
		}
		cur = parse.Combine(cur, p.body(cur.Next, map[string]bool{"end": true}))
		end := p.expectText(cur.Next, "end")
		if !end.OK {
			return parse.Fail
		}
		return parse.Combine(cur, end)
	})
}
