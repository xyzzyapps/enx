package host

import "math"

// ImpossibleCost marks a decision the model forbids outright.
var ImpossibleCost = math.Inf(1)

// Costs supplies the grammar decoder with the price of each decision. The
// grammar is the hard constraint; the costs only break genuine ties.
//
// Both methods take a token index so that a model can look at the token and at
// its neighbours.
type Costs interface {
	// Keep is the cost of treating token i as code. Zero is cheap.
	Keep(i int) float64
	// Skip is the cost of treating token i as English. ImpossibleCost means
	// the token cannot be English at all.
	Skip(i int) float64
}

// Action is the decision for one token.
type Action uint8

const (
	// KeepToken means the token is code and is emitted.
	KeepToken Action = iota
	// SkipToken means the token is English and is deleted.
	SkipToken
)

// String renders the action in lower case.
func (a Action) String() string {
	if a == SkipToken {
		return "skip"
	}
	return "keep"
}

// Path is a complete labelling of a token stream produced by a Machine.
type Path struct {
	// Actions has one entry per token, in token order.
	Actions []Action
	// SeparatorAfter[i] is true when a statement separator may be inserted
	// after the code token at index i without changing meaning. It is false for
	// every token that does not end a statement, which is what stops a newline
	// being inserted after a bare "return".
	SeparatorAfter map[int]bool
	// Cost is the total cost of the path.
	Cost float64
}

// Counts returns the number of tokens at each action.
func (p *Path) Counts() (kept, skipped int) {
	for _, a := range p.Actions {
		if a == SkipToken {
			skipped++
		} else {
			kept++
		}
	}
	return kept, skipped
}

// Machine is a host grammar used as a hard constraint on which token
// subsequences may be code.
//
// A Machine does not tokenize; it is handed a token stream. It owns the
// weighted search because the cost model and the grammar are inseparable: only
// the grammar knows where a cheaper-looking decision would be illegal. The
// grammar implementations keep only the grammar deal: costs arrive as a
// Costs value.
type Machine interface {
	// Language is the lower-case language name, for example "ruby".
	Language() string
	// Decode returns the lowest-cost labelling of toks in which every token is
	// either kept or skipped and the kept tokens form a valid program. ok is
	// false when no such labelling exists.
	Decode(toks []Token, costs Costs) (*Path, bool)
}
