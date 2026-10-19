package codeswitch

import "enx/internal/host"

// Decode runs a host grammar machine over toks and turns the path it returns
// into a labelling.
//
// ok is false when the grammar cannot find any parse at all in which the kept
// tokens form a valid program. A false result must be reported, not papered
// over: it means the input contains syntax the grammar does not cover, and
// guessing would be worse than refusing.
func Decode(machine host.Machine, lex *Lexicon, vocab host.Vocabulary, toks []host.Token) (*Labelling, *host.Path, bool) {
	if machine == nil {
		return nil, nil, false
	}
	if len(toks) == 0 {
		return &Labelling{Mode: "grammar"}, &host.Path{SeparatorAfter: map[int]bool{}}, true
	}

	costs := NewCosts(toks, lex, vocab)
	path, ok := machine.Decode(toks, costs)
	if !ok {
		return nil, nil, false
	}

	lab := &Labelling{Mode: "grammar", Decisions: make([]Decision, 0, len(toks))}
	for i, tok := range toks {
		d := Decision{
			Index:       i,
			Token:       tok,
			CodeCost:    costs.Keep(i),
			EnglishCost: costs.Skip(i),
		}
		switch {
		case tok.IsTrivia():
			d.Label = Trivia
			d.Reason = "trivia is always preserved"
		case path.Actions[i] == host.KeepToken:
			d.Label = Code
			d.Reason = "kept: the grammar needs it here"
			lab.CodeCost += d.CodeCost
		default:
			d.Label = English
			d.Reason = "skipped: the grammar has no use for it here"
			lab.EnglishCost += d.EnglishCost
		}
		lab.Decisions = append(lab.Decisions, d)
	}
	return lab, path, true
}
