package c

import (
	"enx/internal/host"
	"enx/internal/host/parse"
)

// The machinery that turns a grammar into a decision procedure — memoised
// best-cost recursive descent with skip transitions — lives in
// internal/host/parse and is shared with the other languages. This file is only
// the C-specific binding to it.

// slot identifies a memoised production. Each production needs its own slot.
type slot = parse.Slot

const (
	slotProgram slot = iota
	slotItem
	slotBlockItem
	slotDeclaration
	slotFunctionDef
	slotDeclSpecs
	slotDeclarator
	slotStructSpec
	slotEnumSpec
	slotInitializer
	slotCompound
	slotIf
	slotWhile
	slotDoWhile
	slotFor
	slotSwitch
	slotControl
	slotBreak
	slotGoto
	slotLabel
	slotExpression
	slotPrimary
	slotSuffix
	slotCast
	slotPreproc
)

// parser adds C's productions to the shared machinery.
type parser struct {
	*parse.Parser

	itemForms  []parse.Func
	blockForms []parse.Func
	primForms  []parse.Func
}

// cStop is the boundary a terminal lookup must not cross.
//
// Only a semicolon ends a C statement; unlike Ruby a newline does not, and
// unlike Perl there is no sigil to anchor a declaration. A semicolon is not
// trivia, so no lookup crosses one by accident.
func cStop(tok host.Token) bool { return tok.Text == ";" }

// Decode implements host.Machine.
func (m *Machine) Decode(toks []host.Token, costs host.Costs) (*host.Path, bool) {
	return parse.Decode(toks, costs, cStop, func(sp *parse.Parser) parse.Func {
		p := &parser{Parser: sp}
		p.initForms()
		return func(pos int) parse.Result { return p.program(pos, false) }
	})
}

func (p *parser) initForms() {
	p.itemForms = []parse.Func{p.preprocItem, p.staticAssert, p.declOrFunc, p.statement}
	p.blockForms = []parse.Func{p.staticAssert, p.declaration, p.statement}
	p.primForms = []parse.Func{
		p.parenExpr, p.compoundLiteral, p.genericExpr,
		p.identExpr, p.literalExpr, p.stringExpr,
	}
}

// item is one top-level construct: a preprocessor line, a declaration, a
// function definition, or (permissively) a statement. Every item records the
// token it ends on, so the emitter knows where a separator may be inserted.
func (p *parser) item(pos int) parse.Result {
	return p.Memo(slotItem, pos, false, func() parse.Result {
		return p.altAt(pos, p.itemForms...).EndStatement()
	})
}

// blockItem is one construct inside a compound statement.
func (p *parser) blockItem(pos int) parse.Result {
	return p.Memo(slotBlockItem, pos, false, func() parse.Result {
		return p.altAt(pos, p.blockForms...).EndStatement()
	})
}

func (p *parser) program(pos int, prevKept bool) parse.Result {
	return p.Parser.Program(pos, prevKept, switchCost, p.item, slotProgram)
}

func (p *parser) peek(pos int) int          { return p.Parser.Peek(pos) }
func (p *parser) keepAt(i int) parse.Result { return p.Parser.KeepAt(i) }
func (p *parser) keepIf(pos int, texts ...string) parse.Result {
	return p.Parser.KeepIf(pos, false, texts...)
}

// parenExpr is the parenthesised-expression primary. It is the same shape as
// the parenthesised head of a control statement.
func (p *parser) parenExpr(pos int) parse.Result { return p.parenHead(pos) }

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

// attached reports whether the next significant token touches the token before
// pos and its text is one of texts.
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
