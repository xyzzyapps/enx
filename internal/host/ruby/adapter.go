package ruby

import (
	"enx/internal/host"
	"enx/internal/host/parse"
)

// The machinery that turns a grammar into a decision procedure — memoised
// best-cost recursive descent with skip transitions — lives in
// internal/host/parse and is shared with the other languages. This file is only
// the Ruby-specific binding to it: Ruby's productions, the slot identifiers
// they memoise under, and the boundary that stops a lookup crossing a
// statement.

// slot identifies a memoised production. Each production needs its own slot: a
// memo key is only (slot, position, flag), so sharing one would let the first
// production to run at a position decide the answer for the others.
type slot = parse.Slot

const (
	prodProgram slot = iota
	prodStatement
	prodExpr
	prodPrimary
	prodIf
	prodWhile
	prodFor
	prodCase
	prodBegin
	prodDef
	prodClass
	prodModule
	prodControl
	prodModifier
	prodBareArgs
	prodSuffix
	prodBlock
	prodLambda
	prodMultiAssign
	prodIfExpr
)

// parser adds Ruby's productions to the shared machinery.
type parser struct {
	*parse.Parser

	stmtForms []parse.Func
	primForms []parse.Func
}

// rubyStop is the boundary a terminal lookup must not cross.
//
// A newline ends a statement in Ruby, so it is a boundary as well as a
// semicolon. Perl needs only the semicolon. Getting this wrong joined a line
// ending in "y = 5" to the "and" that opened the next line.
func rubyStop(tok host.Token) bool {
	return tok.Kind == host.Newline || tok.Text == ";"
}

// Decode implements host.Machine.
func (m *Machine) Decode(toks []host.Token, costs host.Costs) (*host.Path, bool) {
	return parse.Decode(toks, costs, rubyStop, func(sp *parse.Parser) parse.Func {
		p := &parser{Parser: sp}
		p.initForms()
		return func(pos int) parse.Result { return p.program(pos, false) }
	})
}

func (p *parser) initForms() {
	p.stmtForms = []parse.Func{
		p.ifStmt, p.unlessStmt, p.whileStmt, p.untilStmt, p.forStmt,
		p.caseStmt, p.beginStmt, p.defStmt, p.classStmt, p.moduleStmt,
		p.controlStmt, p.modifierStmt, p.multiAssign, p.exprStmt,
	}
	p.primForms = []parse.Func{
		p.parenExpr, p.arrayLit, p.hashLit, p.lambdaLit,
		p.ifExpr, p.caseExpr, p.beginExpr, p.yieldExpr, p.superExpr,
		p.literalExpr, p.dispatchExpr,
	}
}

func (p *parser) peek(pos int) int     { return p.Parser.Peek(pos) }
func (p *parser) peekLine(pos int) int { return p.Parser.PeekWithin(pos) }

func (p *parser) keepAt(i int) parse.Result { return p.Parser.KeepAt(i) }

func (p *parser) keepIfLineAt(pos int, texts ...string) parse.Result {
	return p.Parser.KeepIf(pos, true, texts...)
}

// keepAdjacent consumes a significant token only when it touches the token
// before it. It distinguishes "def foo=(v)" from the endless form "def foo = v".
func (p *parser) keepAdjacent(pos int, texts ...string) parse.Result {
	i := p.peekLine(pos)
	if i <= 0 {
		return parse.Fail
	}
	j := i - 1
	for j >= 0 && p.Toks[j].IsTrivia() {
		j--
	}
	if j < 0 || p.Toks[j].Span.End != p.Toks[i].Span.Start {
		return parse.Fail
	}
	return p.Parser.KeepIf(pos, true, texts...)
}

func (p *parser) expectLine(pos int, pred func(i int) bool) parse.Result {
	return p.Parser.Expect(pos, true, pred)
}

func (p *parser) expectText(pos int, texts ...string) parse.Result {
	return p.Parser.ExpectText(pos, false, texts...)
}

func (p *parser) expectLineText(pos int, texts ...string) parse.Result {
	return p.Parser.ExpectText(pos, true, texts...)
}

func (p *parser) expectKind(pos int, kinds ...host.Kind) parse.Result {
	return p.Parser.ExpectKind(pos, false, kinds...)
}

func (p *parser) expectLineKind(pos int, kinds ...host.Kind) parse.Result {
	return p.Parser.ExpectKind(pos, true, kinds...)
}

func (p *parser) altAt(pos int, fs ...parse.Func) parse.Result { return p.Parser.Alt(pos, fs...) }

func (p *parser) atTerminator(pos int, terms map[string]bool) bool {
	return p.Parser.Terminator(pos, terms)
}

func (p *parser) body(pos int, terms map[string]bool) parse.Result {
	return p.Parser.Body(pos, terms, switchCost, p.statement)
}

func (p *parser) program(pos int, prevKept bool) parse.Result {
	return p.Parser.Program(pos, prevKept, switchCost, p.statement, prodProgram)
}

// keepSemi consumes a semicolon.
//
// It cannot use the line-restricted lookup, because Ruby's stop predicate
// includes ";" and PeekWithin refuses to return the very token it stopped at.
// The unrestricted lookup returns the next significant token, which is the
// semicolon when there is one, and never crosses it.
func (p *parser) keepSemi(pos int) parse.Result {
	return p.Parser.KeepIf(pos, false, ";")
}
