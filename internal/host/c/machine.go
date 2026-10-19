package c

import (
	"enx/internal/host"
	"enx/internal/host/parse"
)

// Machine is the C grammar, used as the hard constraint that a code subsequence
// must satisfy.
//
// C is the hardest of the three hosts for this technique. Ruby has paren-less
// calls, which make English valid code but at least leaves a lexicon to argue
// with; Perl has sigils and strict subs, which refuse English outright. C has
// neither shelter, and a large share of its keywords are ordinary English
// words: if, else, for, while, do, switch, case, default, break, continue,
// return, goto, static, const, long, short, signed, unsigned, float, void,
// auto, register, extern, union, enum.
//
// What C does have is a hard structural rule: two adjacent identifiers are
// never valid, because there are no paren-less calls. Most English runs
// therefore fail on their own, and the cost model only has to handle the
// keyword collisions.
type Machine struct{}

// NewMachine returns a new C grammar machine.
func NewMachine() *Machine { return &Machine{} }

// Language returns "c".
func (m *Machine) Language() string { return "c" }

// switchCost is charged once for each maximal run of English.
const switchCost = 6.0

var storageClass = map[string]bool{
	"typedef": true, "extern": true, "static": true, "auto": true,
	"register": true, "_Thread_local": true,
}

var typeSpecKeyword = map[string]bool{
	"void": true, "char": true, "short": true, "int": true, "long": true,
	"float": true, "double": true, "signed": true, "unsigned": true,
	"_Bool": true, "_Complex": true,
}

var typeQualifier = map[string]bool{
	"const": true, "volatile": true, "restrict": true, "_Atomic": true,
	"_Noreturn": true, "inline": true, "_Alignas": true,
}

// declOrFunc tries a function definition and a declaration over the same
// prefix, and takes the better of the two. The prefix itself is memoised, so
// the shared work is done once.
func (p *parser) declOrFunc(pos int) parse.Result {
	return p.Memo(slotDeclaration, pos, true, func() parse.Result {
		return parse.Pick(p.functionDef(pos), p.declaration(pos))
	})
}

// declaration is "declaration-specifiers init-declarator-list? ;".
func (p *parser) declaration(pos int) parse.Result {
	return p.Memo(slotDeclaration, pos, false, func() parse.Result {
		specs := p.declSpecs(pos)
		if !specs.OK {
			return parse.Fail
		}
		// A bare "struct S { ... };" or "enum E { ... };" declares no name.
		if semi := p.keepIf(specs.Next, ";"); semi.OK {
			return parse.Combine(specs, semi)
		}

		cur := parse.Result{OK: true, Next: specs.Next}
		count := 0
		for {
			d := p.initDeclarator(cur.Next)
			if !d.OK {
				break
			}
			// A bit-field width belongs to the declarator.
			if colon := p.keepIf(d.Next, ":"); colon.OK {
				width := p.constExpr(colon.Next)
				if !width.OK {
					return parse.Fail
				}
				d = parse.Combine(d, parse.Combine(colon, width))
			}
			cur = parse.Combine(cur, d)
			count++
			if comma := p.keepIf(cur.Next, ","); comma.OK {
				cur = parse.Combine(cur, comma)
				continue
			}
			break
		}
		if count == 0 {
			return parse.Fail
		}
		semi := p.keepIf(cur.Next, ";")
		if !semi.OK {
			return parse.Fail
		}
		return parse.Combine(specs, parse.Combine(cur, semi))
	})
}

// functionDef is "declaration-specifiers declarator declaration-list?
// compound-statement". The declaration list is the old K&R form, where
// parameter types are declared after the parameter names.
func (p *parser) functionDef(pos int) parse.Result {
	return p.Memo(slotFunctionDef, pos, false, func() parse.Result {
		specs := p.declSpecs(pos)
		if !specs.OK {
			return parse.Fail
		}
		d := p.declarator(specs.Next)
		if !d.OK {
			return parse.Fail
		}
		cur := parse.Combine(specs, d)
		for {
			decl := p.declaration(cur.Next)
			if !decl.OK {
				break
			}
			cur = parse.Combine(cur, decl)
		}
		body := p.compound(cur.Next)
		if !body.OK {
			return parse.Fail
		}
		return parse.Combine(cur, body)
	})
}

func (p *parser) initDeclarator(pos int) parse.Result {
	d := p.declarator(pos)
	if !d.OK {
		return parse.Fail
	}
	eq := p.keepIf(d.Next, "=")
	if !eq.OK {
		return d
	}
	init := p.initializer(eq.Next)
	if !init.OK {
		return parse.Fail
	}
	return parse.Combine(d, parse.Combine(eq, init))
}

func (p *parser) initializer(pos int) parse.Result {
	return p.Memo(slotInitializer, pos, false, func() parse.Result {
		if open := p.keepIf(pos, "{"); open.OK {
			cur := parse.Result{OK: true, Next: open.Next}
			for {
				item := p.initItem(cur.Next)
				if !item.OK {
					break
				}
				cur = parse.Combine(cur, item)
				if comma := p.keepIf(cur.Next, ","); comma.OK {
					cur = parse.Combine(cur, comma)
					continue
				}
				break
			}
			close := p.keepIf(cur.Next, "}")
			if !close.OK {
				return parse.Fail
			}
			return parse.Combine(open, parse.Combine(cur, close))
		}
		return p.assignExpr(pos)
	})
}

// initItem is one element of a braced initialiser, with an optional designator:
// ".name = value" or "[index] = value".
func (p *parser) initItem(pos int) parse.Result {
	prefix := parse.Fail
	if dot := p.keepIf(pos, "."); dot.OK {
		name := p.expectKind(dot.Next, host.Identifier)
		if !name.OK {
			return parse.Fail
		}
		eq := p.keepIf(name.Next, "=")
		if !eq.OK {
			return parse.Fail
		}
		prefix = parse.Combine(dot, parse.Combine(name, eq))
	} else if open := p.keepIf(pos, "["); open.OK {
		inner := p.expression(open.Next)
		close := p.keepIf(inner.Next, "]")
		if !inner.OK || !close.OK {
			return parse.Fail
		}
		eq := p.keepIf(close.Next, "=")
		if !eq.OK {
			return parse.Fail
		}
		prefix = parse.Combine(open, parse.Combine(inner, parse.Combine(close, eq)))
	}

	if prefix.OK {
		rest := p.initializer(prefix.Next)
		if !rest.OK {
			return parse.Fail
		}
		return parse.Combine(prefix, rest)
	}
	return p.initializer(pos)
}

// declSpecs is a run of storage classes, type specifiers and qualifiers,
// possibly including a struct, union or enum definition.
func (p *parser) declSpecs(pos int) parse.Result {
	return p.Memo(slotDeclSpecs, pos, false, func() parse.Result {
		cur := parse.Result{OK: true, Next: pos}
		count := 0
		sawType := false
		for {
			if sp := p.structSpec(cur.Next); sp.OK {
				cur = parse.Combine(cur, sp)
				count++
				sawType = true
				continue
			}
			if sp := p.enumSpec(cur.Next); sp.OK {
				cur = parse.Combine(cur, sp)
				count++
				sawType = true
				continue
			}
			i := p.peek(cur.Next)
			if i < 0 {
				break
			}
			tok := p.Toks[i]
			if tok.Kind == host.Keyword {
				if storageClass[tok.Text] || typeQualifier[tok.Text] {
					cur = parse.Combine(cur, p.keepAt(i))
					count++
					if tok.Text == "_Alignas" {
						if args := p.balanced(i+1, "(", ")"); args.OK {
							cur = parse.Combine(cur, args)
						}
					}
					continue
				}
				if typeSpecKeyword[tok.Text] {
					cur = parse.Combine(cur, p.keepAt(i))
					count++
					sawType = true
					continue
				}
				break
			}
			if tok.Kind == host.Identifier && (tok.Text == "__attribute__" || tok.Text == "__declspec") {
				cur = parse.Combine(cur, p.keepAt(i))
				count++
				if args := p.balanced(i+1, "(", ")"); args.OK {
					cur = parse.Combine(cur, args)
				}
				continue
			}
			// A typedef name may stand as a type specifier, but only when no
			// other type has been seen and a declarator can follow it. Without
			// both restrictions any identifier would look like a type, and
			// "something_else;" would read as a declaration.
			if tok.Kind == host.Identifier && !sawType && p.declaratorFollows(i+1) {
				cur = parse.Combine(cur, p.keepAt(i))
				count++
				sawType = true
				continue
			}
			break
		}
		if count == 0 {
			return parse.Fail
		}
		return cur
	})
}

// declaratorFollows reports whether a declarator could begin here: another
// identifier, a pointer, or a parenthesised declarator.
func (p *parser) declaratorFollows(pos int) bool {
	i := p.peek(pos)
	if i < 0 {
		return false
	}
	switch p.Toks[i].Kind {
	case host.Identifier:
		return true
	}
	switch p.Toks[i].Text {
	case "*", "(":
		return true
	}
	return false
}

// structSpec is "struct|union [name]? [ { members } ]".
func (p *parser) structSpec(pos int) parse.Result {
	return p.Memo(slotStructSpec, pos, false, func() parse.Result {
		kw := p.expectText(pos, "struct", "union")
		if !kw.OK {
			return parse.Fail
		}
		cur := kw
		if name := p.expectKind(cur.Next, host.Identifier); name.OK {
			cur = parse.Combine(cur, name)
		}
		if open := p.keepIf(cur.Next, "{"); open.OK {
			inner := parse.Result{OK: true, Next: open.Next}
			for {
				member := p.declaration(inner.Next)
				if !member.OK {
					break
				}
				inner = parse.Combine(inner, member)
			}
			close := p.keepIf(inner.Next, "}")
			if !close.OK {
				return parse.Fail
			}
			cur = parse.Combine(cur, parse.Combine(open, parse.Combine(inner, close)))
		}
		if cur.Next == kw.Next {
			return parse.Fail
		}
		return cur
	})
}

// enumSpec is "enum [name]? [ { enumerators } ]".
func (p *parser) enumSpec(pos int) parse.Result {
	return p.Memo(slotEnumSpec, pos, false, func() parse.Result {
		kw := p.expectText(pos, "enum")
		if !kw.OK {
			return parse.Fail
		}
		cur := kw
		if name := p.expectKind(cur.Next, host.Identifier); name.OK {
			cur = parse.Combine(cur, name)
		}
		if open := p.keepIf(cur.Next, "{"); open.OK {
			inner := parse.Result{OK: true, Next: open.Next}
			for {
				item := p.enumerator(inner.Next)
				if !item.OK {
					break
				}
				inner = parse.Combine(inner, item)
				if comma := p.keepIf(inner.Next, ","); comma.OK {
					inner = parse.Combine(inner, comma)
					continue
				}
				break
			}
			close := p.keepIf(inner.Next, "}")
			if !close.OK {
				return parse.Fail
			}
			cur = parse.Combine(cur, parse.Combine(open, parse.Combine(inner, close)))
		}
		if cur.Next == kw.Next {
			return parse.Fail
		}
		return cur
	})
}

func (p *parser) enumerator(pos int) parse.Result {
	name := p.expectKind(pos, host.Identifier)
	if !name.OK {
		return parse.Fail
	}
	eq := p.keepIf(name.Next, "=")
	if !eq.OK {
		return name
	}
	val := p.constExpr(eq.Next)
	if !val.OK {
		return parse.Fail
	}
	return parse.Combine(name, parse.Combine(eq, val))
}

// declarator is "pointer? direct-declarator".
func (p *parser) declarator(pos int) parse.Result {
	return p.Memo(slotDeclarator, pos, false, func() parse.Result {
		cur := parse.Result{OK: true, Next: pos}
		for {
			star := p.keepIf(cur.Next, "*")
			if !star.OK {
				break
			}
			cur = parse.Combine(cur, star)
			for {
				i := p.peek(cur.Next)
				if i < 0 {
					break
				}
				tok := p.Toks[i]
				if tok.Kind == host.Keyword && (typeQualifier[tok.Text] || tok.Text == "const") {
					cur = parse.Combine(cur, p.keepAt(i))
					continue
				}
				break
			}
		}

		i := p.peek(cur.Next)
		if i < 0 {
			return parse.Fail
		}
		var direct parse.Result
		switch {
		case p.Toks[i].Kind == host.Identifier:
			direct = p.keepAt(i)
		case p.Toks[i].Text == "(":
			open := p.keepAt(i)
			inner := p.declarator(open.Next)
			close := p.keepIf(inner.Next, ")")
			if !inner.OK || !close.OK {
				return parse.Fail
			}
			direct = parse.Combine(open, parse.Combine(inner, close))
		default:
			return parse.Fail
		}

		for {
			if br := p.balanced(direct.Next, "[", "]"); br.OK {
				direct = parse.Combine(direct, br)
				continue
			}
			if pr := p.paramGroup(direct.Next); pr.OK {
				direct = parse.Combine(direct, pr)
				continue
			}
			break
		}
		return parse.Combine(cur, direct)
	})
}

// paramGroup is "( parameters )", including the empty and (void) forms and the
// old-style identifier list.
func (p *parser) paramGroup(pos int) parse.Result {
	open := p.keepIf(pos, "(")
	if !open.OK {
		return parse.Fail
	}
	if close := p.keepIf(open.Next, ")"); close.OK {
		return parse.Combine(open, close)
	}
	list := parse.Result{OK: true, Next: open.Next}
	for {
		prm := p.param(list.Next)
		if !prm.OK {
			break
		}
		list = parse.Combine(list, prm)
		if comma := p.keepIf(list.Next, ","); comma.OK {
			list = parse.Combine(list, comma)
			continue
		}
		break
	}
	close := p.keepIf(list.Next, ")")
	if !close.OK {
		return parse.Fail
	}
	return parse.Combine(open, parse.Combine(list, close))
}

func (p *parser) param(pos int) parse.Result {
	if dots := p.keepIf(pos, "..."); dots.OK {
		return dots
	}
	if specs := p.declSpecs(pos); specs.OK {
		if d := p.declarator(specs.Next); d.OK {
			return parse.Combine(specs, d)
		}
		return specs
	}
	// An old-style identifier list, as in "int f(a, b)".
	if id := p.expectKind(pos, host.Identifier); id.OK {
		return id
	}
	return parse.Fail
}

// compound is "{ block-items }".
func (p *parser) compound(pos int) parse.Result {
	return p.Memo(slotCompound, pos, false, func() parse.Result {
		open := p.keepIf(pos, "{")
		if !open.OK {
			return parse.Fail
		}
		inner := p.Parser.Body(open.Next, map[string]bool{"}": true}, switchCost, p.blockItem)
		close := p.keepIf(inner.Next, "}")
		if !close.OK {
			return parse.Fail
		}
		return parse.Combine(open, parse.Combine(inner, close))
	})
}

func (p *parser) preprocItem(pos int) parse.Result {
	return p.Memo(slotPreproc, pos, false, func() parse.Result {
		return p.expectKind(pos, host.Preproc)
	})
}

// statement is one C statement.
func (p *parser) statement(pos int) parse.Result {
	return p.altAt(pos,
		p.compound,
		p.ifStmt,
		p.whileStmt,
		p.doWhileStmt,
		p.forStmt,
		p.switchStmt,
		p.caseLabel,
		p.returnStmt,
		p.breakStmt,
		p.gotoStmt,
		p.userLabel,
		p.preprocItem,
		p.emptyStmt,
		p.exprStmt,
	)
}

// staticAssert covers "_Static_assert(condition, message);".
func (p *parser) staticAssert(pos int) parse.Result {
	kw := p.expectText(pos, "_Static_assert")
	if !kw.OK {
		return parse.Fail
	}
	args := p.balanced(kw.Next, "(", ")")
	if !args.OK {
		return parse.Fail
	}
	semi := p.keepIf(args.Next, ";")
	if !semi.OK {
		return parse.Fail
	}
	return parse.Combine(kw, parse.Combine(args, semi))
}

func (p *parser) emptyStmt(pos int) parse.Result { return p.keepIf(pos, ";") }

func (p *parser) exprStmt(pos int) parse.Result {
	expr := p.expression(pos)
	if !expr.OK {
		return parse.Fail
	}
	semi := p.keepIf(expr.Next, ";")
	if !semi.OK {
		return parse.Fail
	}
	return parse.Combine(expr, semi)
}

func (p *parser) ifStmt(pos int) parse.Result {
	return p.Memo(slotIf, pos, false, func() parse.Result {
		kw := p.expectText(pos, "if")
		if !kw.OK {
			return parse.Fail
		}
		cond := p.parenHead(kw.Next)
		if !cond.OK {
			return parse.Fail
		}
		cur := parse.Combine(kw, cond)
		thenArm := p.statement(cur.Next)
		if !thenArm.OK {
			return parse.Fail
		}
		cur = parse.Combine(cur, thenArm)
		if els := p.expectText(cur.Next, "else"); els.OK {
			elseArm := p.statement(els.Next)
			if !elseArm.OK {
				return parse.Fail
			}
			cur = parse.Combine(cur, parse.Combine(els, elseArm))
		}
		return cur
	})
}

func (p *parser) whileStmt(pos int) parse.Result {
	return p.Memo(slotWhile, pos, false, func() parse.Result {
		kw := p.expectText(pos, "while")
		if !kw.OK {
			return parse.Fail
		}
		cond := p.parenHead(kw.Next)
		if !cond.OK {
			return parse.Fail
		}
		body := p.statement(cond.Next)
		if !body.OK {
			return parse.Fail
		}
		return parse.Combine(kw, parse.Combine(cond, body))
	})
}

func (p *parser) doWhileStmt(pos int) parse.Result {
	return p.Memo(slotDoWhile, pos, false, func() parse.Result {
		kw := p.expectText(pos, "do")
		if !kw.OK {
			return parse.Fail
		}
		body := p.statement(kw.Next)
		if !body.OK {
			return parse.Fail
		}
		cur := parse.Combine(kw, body)
		w := p.expectText(cur.Next, "while")
		if !w.OK {
			return parse.Fail
		}
		cond := p.parenHead(w.Next)
		if !cond.OK {
			return parse.Fail
		}
		semi := p.keepIf(cond.Next, ";")
		if !semi.OK {
			return parse.Fail
		}
		return parse.Combine(cur, parse.Combine(w, parse.Combine(cond, semi)))
	})
}

func (p *parser) forStmt(pos int) parse.Result {
	return p.Memo(slotFor, pos, false, func() parse.Result {
		kw := p.expectText(pos, "for")
		if !kw.OK {
			return parse.Fail
		}
		open := p.keepIf(kw.Next, "(")
		if !open.OK {
			return parse.Fail
		}
		cur := parse.Combine(kw, open)

		// init: a declaration (C99) or an expression, or nothing.
		if decl := p.declaration(cur.Next); decl.OK {
			cur = parse.Combine(cur, decl)
		} else if semi := p.keepIf(cur.Next, ";"); semi.OK {
			cur = parse.Combine(cur, semi)
		} else if expr := p.expression(cur.Next); expr.OK {
			semi := p.keepIf(expr.Next, ";")
			if !semi.OK {
				return parse.Fail
			}
			cur = parse.Combine(cur, parse.Combine(expr, semi))
		} else {
			return parse.Fail
		}

		// condition
		if expr := p.expression(cur.Next); expr.OK {
			cur = parse.Combine(cur, expr)
		}
		semi := p.keepIf(cur.Next, ";")
		if !semi.OK {
			return parse.Fail
		}
		cur = parse.Combine(cur, semi)

		// increment
		if expr := p.expression(cur.Next); expr.OK {
			cur = parse.Combine(cur, expr)
		}
		close := p.keepIf(cur.Next, ")")
		if !close.OK {
			return parse.Fail
		}
		cur = parse.Combine(cur, close)

		body := p.statement(cur.Next)
		if !body.OK {
			return parse.Fail
		}
		return parse.Combine(cur, body)
	})
}

func (p *parser) switchStmt(pos int) parse.Result {
	return p.Memo(slotSwitch, pos, false, func() parse.Result {
		kw := p.expectText(pos, "switch")
		if !kw.OK {
			return parse.Fail
		}
		cond := p.parenHead(kw.Next)
		if !cond.OK {
			return parse.Fail
		}
		body := p.statement(cond.Next)
		if !body.OK {
			return parse.Fail
		}
		return parse.Combine(kw, parse.Combine(cond, body))
	})
}

// caseLabel keeps "case EXPR :" or "default :" as a label statement. The
// labelled statement that follows is parsed as an ordinary statement, so
// everything is kept without the grammar needing to model switch bodies.
func (p *parser) caseLabel(pos int) parse.Result {
	if dflt := p.expectText(pos, "default"); dflt.OK {
		colon := p.keepIf(dflt.Next, ":")
		if !colon.OK {
			return parse.Fail
		}
		return parse.Combine(dflt, colon)
	}
	kw := p.expectText(pos, "case")
	if !kw.OK {
		return parse.Fail
	}
	val := p.constExpr(kw.Next)
	if !val.OK {
		return parse.Fail
	}
	colon := p.keepIf(val.Next, ":")
	if !colon.OK {
		return parse.Fail
	}
	return parse.Combine(kw, parse.Combine(val, colon))
}

// userLabel keeps "name :" as a label statement, for the same reason.
func (p *parser) userLabel(pos int) parse.Result {
	return p.Memo(slotLabel, pos, false, func() parse.Result {
		name := p.expectKind(pos, host.Identifier)
		if !name.OK {
			return parse.Fail
		}
		colon := p.keepIf(name.Next, ":")
		if !colon.OK {
			return parse.Fail
		}
		return parse.Combine(name, colon)
	})
}

func (p *parser) returnStmt(pos int) parse.Result {
	return p.Memo(slotControl, pos, false, func() parse.Result {
		kw := p.expectText(pos, "return")
		if !kw.OK {
			return parse.Fail
		}
		cur := kw
		if expr := p.expression(cur.Next); expr.OK {
			cur = parse.Combine(cur, expr)
		}
		semi := p.keepIf(cur.Next, ";")
		if !semi.OK {
			return parse.Fail
		}
		return parse.Combine(cur, semi)
	})
}

func (p *parser) breakStmt(pos int) parse.Result {
	return p.Memo(slotBreak, pos, false, func() parse.Result {
		kw := p.expectText(pos, "break", "continue")
		if !kw.OK {
			return parse.Fail
		}
		semi := p.keepIf(kw.Next, ";")
		if !semi.OK {
			return parse.Fail
		}
		return parse.Combine(kw, semi)
	})
}

func (p *parser) gotoStmt(pos int) parse.Result {
	return p.Memo(slotGoto, pos, false, func() parse.Result {
		kw := p.expectText(pos, "goto")
		if !kw.OK {
			return parse.Fail
		}
		name := p.expectKind(kw.Next, host.Identifier)
		if !name.OK {
			return parse.Fail
		}
		semi := p.keepIf(name.Next, ";")
		if !semi.OK {
			return parse.Fail
		}
		return parse.Combine(kw, parse.Combine(name, semi))
	})
}
