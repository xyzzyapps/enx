package codeswitch

import (
	"strings"

	"enx/internal/host"
)

// NoiseCost is charged for treating a token that is neither a word nor trivia
// as English.
//
// Prose does contain punctuation, digits and dashes, so such tokens must be
// skippable; but keeping them as code is always free, so a real program never
// loses its operators to prose.
const NoiseCost = 5.0

// StrongCodeCost is charged for skipping a token that cannot be prose at all:
// a preprocessor directive, a regex literal, a heredoc, a sigil variable.
//
// It has to exceed a switch cost. Keeping an isolated token in the middle of
// prose ends the current English run and starts a new one afterwards, which
// costs one switch; if skipping were cheaper than that, an isolated
// "#include <stdio.h>" between two paragraphs would be thrown away. Marking
// these tokens as expensive to skip is what keeps them.
const StrongCodeCost = 14.0

// strongCodeKind reports whether a token of this kind can never be English.
func strongCodeKind(k host.Kind) bool {
	switch k {
	case host.Preproc, host.Regex, host.Heredoc, host.Variable, host.Sigil:
		return true
	}
	return false
}

// HostNamePenalty is added to the price of skipping a word that the host
// vocabulary recognises as a method or core name. It is what distinguishes
// "puts answer", a call, from "we start", two English words.
const HostNamePenalty = 8.0

// EscapeCost is charged for skipping a span the author marked as prose with a
// ~ ... ~ escape.
//
// It is almost free, because there is nothing to weigh: the author has said
// what the span is. The grammar can never keep it, so the decoder has no choice
// to make.
const EscapeCost = 1.0

// Costs implements host.Costs for one token stream, using the English lexicon,
// the host vocabulary, and the lexical shape of each token.
type Costs struct {
	toks  []host.Token
	lex   *Lexicon
	vocab host.Vocabulary
}

// NewCosts returns a cost model for toks. vocab may be nil.
func NewCosts(toks []host.Token, lex *Lexicon, vocab host.Vocabulary) *Costs {
	return &Costs{toks: toks, lex: lex, vocab: vocab}
}

// knowsName reports whether the token is a well known name in the host
// language.
func (c *Costs) knowsName(tok host.Token) bool {
	return c.vocab != nil && c.vocab.Knows(tok.Text)
}

// Keep implements host.Costs: the price of calling token i code.
//
// Only words have a price. Everything else is free, because nothing else can
// be English. A keyword is free too: the grammar only reaches for one when it
// needs it, and a keyword it does not need is skipped anyway.
//
// For a word, the English rank matters more than the price. A function word is
// almost never an identifier, so it is expensive to keep; an ordinary word
// costs CodeBias more to keep than to skip, which is what makes prose prose.
func (c *Costs) Keep(i int) float64 {
	tok := c.toks[i]
	if tok.IsTrivia() {
		return 0
	}
	if tok.Kind == host.Prose {
		// An author-marked prose span is never code, so the grammar cannot
		// take it and the decoder must skip it.
		return host.ImpossibleCost
	}
	if tok.Kind == host.Anaphor {
		// A claimed anaphor is code by construction: the author wrote a marker,
		// not a word, so there is nothing to weigh.
		return 0
	}
	if tok.Kind == host.Keyword {
		return 0
	}
	if !tok.Kind.IsWord() {
		return 0
	}
	if c.knowsName(tok) {
		// A host name in a code position is code, whatever the lexicon says.
		return 0
	}
	if c.callAttached(i) {
		// A word written hard against a "(" or "[" is a call or a subscript.
		// Prose never glues a word to a bracket, and this is what keeps
		// "sum(a, b)" a call when "sum" is also a common English word.
		return 0
	}
	if tok.Kind == host.Constant && !c.isLineInitial(i) {
		// A capitalised name away from the start of a line is a constant, not a
		// sentence-initial English word.
		return 0
	}
	if c.isMethodName(i) {
		// A word glued to a preceding dot is a method name, so "X.new" is code
		// even though "new" is one of the commonest words in English. The dot
		// must be adjacent, which is what stops the full stop that ends an
		// English sentence from turning the next word into a method call.
		return 0
	}
	if c.isReceiver(i) {
		// "other.cents" and "A::B" use a bare word as a receiver. Requiring
		// adjacency to both the separator and the following name keeps a full
		// stop at the end of a sentence from becoming a receiver.
		return 0
	}
	if LooksLikeCode(tok.Text) {
		return 0
	}
	if c.operandish(i) {
		// In operand position, even a function word is a variable name. It is
		// safe to price it as code because no statement form can end at a comma
		// or an operator, so a prose word in this position is still dropped.
		if cost, ok := c.lex.LogCost(tok.Text); ok {
			return CodeBias + cost
		}
		return CodeBias
	}
	if shortFunctionWords[strings.ToLower(tok.Text)] {
		return FunctionWordKeep
	}
	if cost, ok := c.lex.LogCost(tok.Text); ok {
		if cost < FunctionWordCutoff {
			return FunctionWordKeep
		}
		return CodeBias + cost
	}
	// A word the lexicon does not know still costs a little to keep, so that
	// an unknown prose word is not treated as free code.
	return CodeBias
}

// Skip implements host.Costs: the price of calling token i English.
//
// Every token is skippable at some price. Nothing is forbidden, because a
// token that can neither be kept nor skipped leaves the grammar with no parse
// at all and fails the whole file.
func (c *Costs) Skip(i int) float64 {
	tok := c.toks[i]
	if tok.IsTrivia() {
		return 0
	}
	if tok.Kind == host.Prose {
		return EscapeCost
	}
	if tok.Kind == host.Anaphor {
		// Skipping an anaphor would delete a reference the author wrote.
		return StrongCodeCost
	}
	switch {
	case tok.Kind == host.Keyword:
		if cost, ok := c.lex.LogCost(tok.Text); ok {
			return cost + c.namePenalty(tok)
		}
		return NoiseCost
	case tok.Kind.IsWord():
		if LooksLikeCode(tok.Text) {
			return CodeShapedEnglishCost
		}
		if cost, ok := c.lex.LogCost(tok.Text); ok {
			return cost + c.namePenalty(tok)
		}
		return PlausibleEnglishCost
	default:
		if strongCodeKind(tok.Kind) {
			return StrongCodeCost
		}
		return NoiseCost
	}
}

// namePenalty makes skipping a known host name more expensive than skipping an
// ordinary word, which is what keeps "puts answer" a call.
func (c *Costs) namePenalty(tok host.Token) float64 {
	if c.knowsName(tok) {
		return HostNamePenalty
	}
	return 0
}

// operandAfter are the token texts that tell the token before them is an
// operand: "a," and "a = 1" and "f(a)" and "a[0]" and "x + a" all end or
// continue differently from the English article.
var operandAfter = map[string]bool{
	",": true, ")": true, "]": true, "}": true, ";": true, "[": true,
	"=": true, "==": true, "!=": true, "+=": true, "-=": true, "*=": true,
	"/=": true, "%=": true, "=>": true, "->": true, "|": true, "&": true,
	"*": true, "/": true, "%": true, "**": true, "+": true, "^": true,
	"..": true, "...": true,
	"<": true, ">": true, "<=": true, ">=": true, "<=>": true,
	"&&": true, "||": true, "<<": true, ">>": true,
}

// operandish reports whether token i is followed closely enough by something
// that can only follow an operand.
//
// It exists because a word can be at once common English and an ordinary
// variable name: "first", "total", "count", and of course "a". Spelling cannot
// tell them apart and neither can the neighbours alone, but the neighbours do
// separate the two uses. "the first of them" is prose; "first, *rest = list"
// and "x + first" are code.
//
// Pricing such a word as code is safe even when it is prose, because no
// statement form ends at a comma or an operator: the grammar refuses the
// reading, so the word is dropped anyway.
func (c *Costs) operandish(i int) bool {
	tok := c.toks[i]
	if !tok.Kind.IsWord() {
		return false
	}
	// A word directly after a definition keyword is the thing being defined, so
	// it is code. Without this, "sub new" and "def new" were dropped, because
	// "new" is one of the commonest words in English and the grammar could
	// equally well keep the body and skip the name.
	if prev := c.prevSignificant(i); prev >= 0 && definitionKeyword[c.toks[prev].Text] {
		return true
	}
	next := c.nextSignificant(i)
	if next < 0 {
		return false
	}
	return operandAfter[c.toks[next].Text]
}

// definitionKeyword are the keywords that introduce a name.
var definitionKeyword = map[string]bool{
	"sub": true, "def": true, "class": true, "module": true,
	"package": true, "struct": true, "union": true, "enum": true,
	"typedef": true,
}

// prevSignificant returns the index of the previous non-trivia token, or -1.
func (c *Costs) prevSignificant(i int) int {
	for j := i - 1; j >= 0; j-- {
		if !c.toks[j].IsTrivia() {
			return j
		}
	}
	return -1
}

// nextSignificant returns the index of the next non-trivia token after i, or -1.
func (c *Costs) nextSignificant(i int) int {
	for j := i + 1; j < len(c.toks); j++ {
		if !c.toks[j].IsTrivia() {
			return j
		}
	}
	return -1
}

// callAttached reports whether token i is written hard against a "(" or "[",
// which makes it a call or a subscript rather than a bare word.
//
// Adjacency is required, and that is the whole point: "sum(a, b)" is a call
// while "the (first)" is not.
func (c *Costs) callAttached(i int) bool {
	j := i + 1
	if j >= len(c.toks) {
		return false
	}
	next := c.toks[j]
	if next.IsTrivia() || c.toks[i].Span.End != next.Span.Start {
		return false
	}
	return next.Text == "(" || next.Text == "["
}

// isLineInitial reports whether token i is the first significant token of its
// line. It distinguishes a sentence-initial capital, which may be prose, from a
// capitalised constant in the middle of a line, which is code.
func (c *Costs) isLineInitial(i int) bool {
	for j := i - 1; j >= 0; j-- {
		tok := c.toks[j]
		if tok.Kind == host.Newline {
			return true
		}
		if !tok.IsTrivia() {
			return false
		}
	}
	return true
}

// isMethodName and isReceiver are in leaf.go.
