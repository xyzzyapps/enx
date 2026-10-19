// Package parse is the machinery shared by every host-language grammar in enx.
//
// It is a memoised best-cost recursive descent in which every production may
// skip tokens it does not need, paying the cost model's price. That single
// twist is what turns an ordinary grammar into a decision procedure: the parser
// returns the cheapest parse rather than the first one, and the tokens it
// refused are the English.
//
// A host grammar supplies three things and nothing else: its productions, the
// set of tokens a lookup must not cross, and its cost model.
package parse

import (
	"math"

	"enx/internal/host"
)

// Result is the outcome of parsing a production at a token position.
//
// Kept holds the token indices consumed as code and Ends the indices that
// terminate a statement, so the winning parse yields both the labelling and the
// boundary map the emitter needs.
type Result struct {
	OK   bool
	Next int
	Cost float64
	Kept []int
	Ends []int
}

// Fail is the result of a production that did not parse.
var Fail = Result{Cost: math.Inf(1)}

// Result algebra lives in leaf.go.

// Pick returns the better of two alternative parses of the same construct.
func Pick(a, b Result) Result {
	if Better(a, b) {
		return b
	}
	return a
}

// Func parses one production starting at a token index.
type Func func(pos int) Result

// Slot identifies a memoised production.
//
// Each production needs its own slot: a memo key is only (slot, position,
// flag), so sharing one would let the first production to run at a position
// decide the answer for the others.
type Slot uint8

// MatchText reports whether tok is non-trivia and its text is one of texts.
func MatchText(tok host.Token, texts []string) bool {
	if tok.IsTrivia() {
		return false
	}
	for _, t := range texts {
		if tok.Text == t {
			return true
		}
	}
	return false
}

type key struct {
	slot Slot
	pos  int
	flag bool
}

// Parser is the shared machinery for one file.
type Parser struct {
	// Toks is the token stream being parsed.
	Toks []host.Token
	// Costs is the model that prices keeping and skipping.
	Costs host.Costs
	// Stop reports whether a terminal lookup must not cross this token. Ruby
	// treats a newline or a semicolon as a statement boundary; C treats a
	// semicolon. A lookup that wants the boundary token itself still finds it.
	Stop func(tok host.Token) bool

	cache map[key]Result
	busy  map[key]bool
}

// New returns a parser for toks.
func New(toks []host.Token, costs host.Costs, stop func(host.Token) bool) *Parser {
	return &Parser{
		Toks:  toks,
		Costs: costs,
		Stop:  stop,
		cache: make(map[key]Result),
		busy:  make(map[key]bool),
	}
}

// Memo caches a production's result. A key re-entered while still being
// computed means left recursion, which these grammars do not permit, so the
// branch is refused rather than allowed to loop.
func (p *Parser) Memo(slot Slot, pos int, flag bool, compute func() Result) Result {
	k := key{slot, pos, flag}
	if r, ok := p.cache[k]; ok {
		return r
	}
	if p.busy[k] {
		return Fail
	}
	p.busy[k] = true
	r := compute()
	delete(p.busy, k)
	p.cache[k] = r
	return r
}

// Peek returns the index of the next non-trivia token at or after pos, or -1.
//
// Lookahead never skips English: a prefix operator or a primary form must be
// where the parser already is, otherwise "a + b" would read the "+" as a unary
// sign and drop "a" as prose.
func (p *Parser) Peek(pos int) int {
	for j := pos; j < len(p.Toks); j++ {
		if !p.Toks[j].IsTrivia() {
			return j
		}
	}
	return -1
}

// PeekWithin is Peek restricted to the current statement: it returns -1 when a
// token the grammar's Stop predicate rejects comes first.
func (p *Parser) PeekWithin(pos int) int {
	for j := pos; j < len(p.Toks); j++ {
		tok := p.Toks[j]
		if p.Stop != nil && p.Stop(tok) {
			return -1
		}
		if tok.IsTrivia() {
			continue
		}
		return j
	}
	return -1
}

// KeepAt returns a result consuming exactly the significant token at index i.
func (p *Parser) KeepAt(i int) Result {
	if i < 0 || i >= len(p.Toks) {
		return Fail
	}
	c := p.Costs.Keep(i)
	if c >= host.ImpossibleCost {
		return Fail
	}
	return Result{OK: true, Next: i + 1, Cost: c, Kept: []int{i}}
}

// KeepIf consumes the significant token at pos when its text is one of texts.
func (p *Parser) KeepIf(pos int, within bool, texts ...string) Result {
	i := p.peek(pos, within)
	if i < 0 || !MatchText(p.Toks[i], texts) {
		return Fail
	}
	return p.KeepAt(i)
}

// KeepRange marks every token in [from, to) as code. It is how a construct
// whose contents need no analysis, such as a prototype or a signature, is
// consumed whole.
func (p *Parser) KeepRange(from, to int) Result {
	kept := make([]int, 0, to-from)
	cost := 0.0
	for i := from; i < to; i++ {
		if p.Toks[i].IsTrivia() {
			continue
		}
		c := p.Costs.Keep(i)
		if c >= host.ImpossibleCost {
			return Fail
		}
		cost += c
		kept = append(kept, i)
	}
	return Result{OK: true, Next: to, Cost: cost, Kept: kept}
}

// Balanced consumes a balanced bracket group whole.
func (p *Parser) Balanced(pos int, open, close string, within bool) Result {
	i := p.peek(pos, within)
	if i < 0 || p.Toks[i].Text != open {
		return Fail
	}
	depth := 0
	for j := i; j < len(p.Toks); j++ {
		tok := p.Toks[j]
		if tok.IsTrivia() {
			continue
		}
		if tok.Text == open {
			depth++
		}
		if tok.Text == close {
			depth--
			if depth == 0 {
				return p.KeepRange(i, j+1)
			}
		}
	}
	return Fail
}

func (p *Parser) peek(pos int, within bool) int {
	if within {
		return p.PeekWithin(pos)
	}
	return p.Peek(pos)
}

// Expect returns the next significant token when pred accepts it.
//
// Lookup is adjacency only, never a search. An early version let a terminal
// search forward while skipping English, which let the "and" in
// "x = 5, and y ..." become the operator joining "x" to "and y": a parse that
// was valid and completely wrong. Prose is crossed by Program and Body, which
// skip a token at a time between statements, so no terminal ever needs to look
// past the next significant token.
func (p *Parser) Expect(pos int, within bool, pred func(i int) bool) Result {
	i := p.peek(pos, within)
	if i < 0 || !pred(i) {
		return Fail
	}
	return p.KeepAt(i)
}

// ExpectText is Expect for a set of token texts.
func (p *Parser) ExpectText(pos int, within bool, texts ...string) Result {
	return p.Expect(pos, within, func(i int) bool { return MatchText(p.Toks[i], texts) })
}

// ExpectKind is Expect for a set of token kinds.
func (p *Parser) ExpectKind(pos int, within bool, kinds ...host.Kind) Result {
	return p.Expect(pos, within, func(i int) bool {
		tok := p.Toks[i]
		if tok.IsTrivia() {
			return false
		}
		for _, k := range kinds {
			if tok.Kind == k {
				return true
			}
		}
		return false
	})
}

// Alt returns the best of several alternative productions.
func (p *Parser) Alt(pos int, fs ...Func) Result {
	best := Fail
	for _, f := range fs {
		if r := f(pos); Better(best, r) {
			best = r
		}
	}
	return best
}

// Terminator reports whether the next significant token is one of terms.
func (p *Parser) Terminator(pos int, terms map[string]bool) bool {
	for j := pos; j < len(p.Toks); j++ {
		tok := p.Toks[j]
		if tok.IsTrivia() {
			continue
		}
		return terms[tok.Text]
	}
	return false
}

// Body parses statements until a terminator appears, crossing prose between
// statements one token at a time. switchCost is charged once per maximal run of
// English, because prose comes in phrases.
//
// Each position compares "parse a statement here" with "skip this token and
// continue". The second branch is what lets prose live inside a class, method or
// function body: a bare expression is a valid statement in Ruby, so without it
// the greedy parser keeps the prose as a run of tiny statements.
//
// The decision is memoised per (position, prevKept) for this body only. The
// terminator set is fixed for the call; the statement production has its own
// parser-wide memo.
func (p *Parser) Body(pos int, terms map[string]bool, switchCost float64, statement Func) Result {
	type key struct {
		pos      int
		prevKept bool
	}
	memo := make(map[key]Result)

	var walk func(int, bool) Result
	walk = func(pos int, prevKept bool) Result {
		if pos >= len(p.Toks) {
			return Result{OK: true, Next: pos}
		}
		k := key{pos: pos, prevKept: prevKept}
		if r, ok := memo[k]; ok {
			return r
		}

		// The terminator is looked up through trivia, exactly as the previous
		// iterative body did. Trivia before it is left for the caller.
		if p.Terminator(pos, terms) {
			r := Result{OK: true, Next: pos}
			memo[k] = r
			return r
		}

		tok := p.Toks[pos]
		if tok.IsTrivia() {
			r := walk(pos+1, prevKept)
			memo[k] = r
			return r
		}

		best := Fail

		// Candidate 1: a statement starts here.
		st := statement(pos)
		statementOK := st.OK && st.Next > pos
		if statementOK {
			if rest := walk(st.Next, true); rest.OK {
				cand := Combine(st, rest)
				if !best.OK || cand.Cost < best.Cost {
					best = cand
				}
			}
		}

		// Candidate 2: this token is English. Skipping the first token of a
		// run pays the switch cost; later tokens in the run do not.
		//
		// Do not let an arbitrary skip wander into a statement that actually
		// parsed: in C, skipping "money_t total = { 0 };" would walk into the
		// initialiser's "}" and mistake it for the function's terminator. A
		// word may be skipped over a successful statement only when the cost
		// model says the English reading is cheaper. Punctuation is skipped
		// only when no statement starts there, which is the old body fallback.
		allowSkip := !statementOK
		if statementOK && tok.Kind.IsWord() {
			allowSkip = p.Costs.Skip(pos) < p.Costs.Keep(pos)
		}
		if allowSkip {
			if sk := p.Costs.Skip(pos); sk < host.ImpossibleCost {
				if rest := walk(pos+1, false); rest.OK {
					cost := sk + rest.Cost
					if prevKept {
						cost += switchCost
					}
					cand := Result{
						OK:   true,
						Next: rest.Next,
						Cost: cost,
						Kept: rest.Kept,
						Ends: rest.Ends,
					}
					if !best.OK || cand.Cost < best.Cost {
						best = cand
					}
				}
			}
		}

		if !best.OK {
			// Preserve the old body behaviour: stop at a token that can be
			// neither kept nor skipped and let the caller decide.
			best = Result{OK: true, Next: pos}
		}
		memo[k] = best
		return best
	}

	return walk(pos, true)
}

// Program parses statements to the end of the input.
func (p *Parser) Program(pos int, prevKept bool, switchCost float64, statement Func, slot Slot) Result {
	return p.Memo(slot, pos, prevKept, func() Result {
		if pos >= len(p.Toks) {
			return Result{OK: true, Next: pos}
		}
		tok := p.Toks[pos]
		if tok.IsTrivia() {
			rest := p.Program(pos+1, prevKept, switchCost, statement, slot)
			if !rest.OK {
				return Fail
			}
			return Result{OK: true, Next: rest.Next, Cost: rest.Cost, Kept: rest.Kept, Ends: rest.Ends}
		}

		best := Fail
		// A successful production must consume at least one token. Without the
		// guard, a production that returned OK without advancing would recurse
		// forever on the same position.
		if st := statement(pos); st.OK && st.Next > pos {
			if rest := p.Program(st.Next, true, switchCost, statement, slot); rest.OK {
				if cand := Combine(st, rest); cand.Cost < best.Cost {
					best = cand
				}
			}
		}
		if sk := p.Costs.Skip(pos); sk < host.ImpossibleCost {
			if rest := p.Program(pos+1, false, switchCost, statement, slot); rest.OK {
				cost := sk + rest.Cost
				if prevKept {
					cost += switchCost
				}
				cand := Result{OK: true, Next: rest.Next, Cost: cost, Kept: rest.Kept, Ends: rest.Ends}
				if cand.Cost < best.Cost {
					best = cand
				}
			}
		}
		return best
	})
}

// Decode runs entry and turns the resulting labelling into a host.Path.
//
// A false result means the grammar found no parse at all in which the kept
// tokens form a valid program. That must be reported rather than papered over:
// it means the input uses syntax the grammar does not cover, and guessing would
// be worse than refusing.
func Decode(toks []host.Token, costs host.Costs, stop func(host.Token) bool, entry func(p *Parser) Func) (*host.Path, bool) {
	if len(toks) == 0 {
		return &host.Path{SeparatorAfter: map[int]bool{}}, true
	}
	p := New(toks, costs, stop)
	r := entry(p)(0)
	if !r.OK || r.Next != len(toks) {
		return nil, false
	}

	kept := make(map[int]struct{}, len(r.Kept))
	for _, i := range r.Kept {
		kept[i] = struct{}{}
	}
	path := &host.Path{
		Actions:        make([]host.Action, len(toks)),
		SeparatorAfter: make(map[int]bool, len(r.Ends)),
		Cost:           r.Cost,
	}
	for i := range toks {
		if _, ok := kept[i]; ok {
			path.Actions[i] = host.KeepToken
		} else {
			path.Actions[i] = host.SkipToken
		}
	}
	for _, e := range r.Ends {
		if _, ok := kept[e]; ok {
			path.SeparatorAfter[e] = true
		}
	}
	return path, true
}

// EndStatement records that a result ends a statement, so that a separator may
// be inserted after it.
func (r Result) EndStatement() Result {
	last := r.LastKept()
	if last < 0 {
		return r
	}
	ends := make([]int, 0, len(r.Ends)+1)
	ends = append(ends, r.Ends...)
	ends = append(ends, last)
	r.Ends = ends
	return r
}
