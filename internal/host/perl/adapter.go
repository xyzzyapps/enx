package perl

import (
	"enx/internal/host"
	"enx/internal/host/parse"
)

// The machinery that turns a grammar into a decision procedure — memoised
// best-cost recursive descent with skip transitions — lives in
// internal/host/parse and is shared with the other languages. This file is only
// the Perl-specific binding to it: Perl's productions, the slot identifiers
// they memoise under, and the boundary that stops a lookup crossing a
// statement.

// slot identifies a memoised production. Each production needs its own slot: a
// memo key is only (slot, position, flag), so sharing one would let the first
// production to run at a position decide the answer for the others. That bug
// made every "my" statement fail once, because a failing "use" statement had
// already claimed the slot.
type slot = parse.Slot

const (
	slotProgram slot = iota
	slotStatement
	slotExpression
	slotPrimary
	slotIf
	slotUnless
	slotWhile
	slotFor
	slotForeach
	slotSub
	slotGiven
	slotWhen
	slotBlockStmt
	slotBlockBody
	slotDecl
	slotUse
	slotPackage
	slotLabel
	slotControl
	slotModifier
	slotArgs
	slotSuffix
)

// parser adds Perl's productions to the shared machinery.
type parser struct {
	*parse.Parser

	stmtForms []parse.Func
	primForms []parse.Func
}

// perlStop is the boundary a terminal lookup must not cross.
//
// Only a semicolon ends a Perl statement; a newline does not. A semicolon is not
// trivia, so no lookup can cross one by accident, and a lookup that wants the
// semicolon itself still finds it.
func perlStop(tok host.Token) bool { return tok.Text == ";" }

// Decode implements host.Machine.
func (m *Machine) Decode(toks []host.Token, costs host.Costs) (*host.Path, bool) {
	return parse.Decode(toks, costs, perlStop, func(sp *parse.Parser) parse.Func {
		p := &parser{Parser: sp}
		p.initForms()
		return func(pos int) parse.Result { return p.program(pos, false) }
	})
}

func (p *parser) initForms() {
	p.stmtForms = []parse.Func{
		p.useStmt, p.subDecl, p.ifStmt, p.unlessStmt, p.whileStmt,
		p.forStmt, p.foreachStmt, p.givenStmt, p.whenStmt, p.declStmt, p.packageStmt,
		p.blockStmt, p.labelStmt, p.controlStmt, p.modifierStmt, p.exprStmt,
	}
	p.primForms = []parse.Func{
		p.parenExpr, p.arrayRef, p.hashRef, p.anonSub, p.doBlock,
		p.literalExpr, p.dispatchExpr,
	}
}

func (p *parser) peek(pos int) int     { return p.Parser.Peek(pos) }
func (p *parser) peekStmt(pos int) int { return p.Parser.Peek(pos) }

func (p *parser) keepAt(i int) parse.Result { return p.Parser.KeepAt(i) }

func (p *parser) keepIf(pos int, texts ...string) parse.Result {
	return p.Parser.KeepIf(pos, false, texts...)
}

func (p *parser) balanced(pos int, open, close string) parse.Result {
	return p.Parser.Balanced(pos, open, close, false)
}

func (p *parser) expect(pos int, pred func(i int) bool) parse.Result {
	return p.Parser.Expect(pos, false, pred)
}

func (p *parser) expectText(pos int, texts ...string) parse.Result {
	return p.Parser.ExpectText(pos, false, texts...)
}

func (p *parser) expectKind(pos int, kinds ...host.Kind) parse.Result {
	return p.Parser.ExpectKind(pos, false, kinds...)
}

func (p *parser) altAt(pos int, fs ...parse.Func) parse.Result { return p.Parser.Alt(pos, fs...) }

func (p *parser) atTerminator(pos int, terms map[string]bool) bool {
	return p.Parser.Terminator(pos, terms)
}

// optSemi consumes a statement terminator. Perl lets the last statement in a
// block omit it, so a closing brace or the end of input also ends a statement.
func (p *parser) optSemi(pos int) parse.Result {
	if s := p.keepIf(pos, ";"); s.OK {
		return s
	}
	i := p.peekStmt(pos)
	if i < 0 {
		return parse.Result{OK: true, Next: pos}
	}
	if p.Toks[i].Text == "}" {
		return parse.Result{OK: true, Next: pos}
	}
	return parse.Fail
}

func (p *parser) body(pos int, terms map[string]bool) parse.Result {
	return p.Parser.Body(pos, terms, switchCost, p.statement)
}

func (p *parser) program(pos int, prevKept bool) parse.Result {
	return p.Parser.Program(pos, prevKept, switchCost, p.statement, slotProgram)
}
