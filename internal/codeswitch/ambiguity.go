package codeswitch

import (
	"sort"

	"enx/internal/core"
	"enx/internal/host"
)

// DefaultMargin is the cost difference below which two readings of the same
// span are reported as ambiguous rather than decided silently.
//
// Costs are negative log probabilities, so a difference of 2 is a factor of
// about e^2 in likelihood. Anything under that is not a decision worth trusting.
const DefaultMargin = 2.0

// MaxAmbiguityProbes bounds how many regions are tested overall. Each probe is
// a full re-decode, so the analysis is deliberately capped; FindAmbiguities
// reports whether the cap was reached rather than implying a clean bill of
// health.
const MaxAmbiguityProbes = 128

// Ambiguity is a region whose opposite reading is also feasible at a similar
// cost.
//
// Both directions matter, and they fail differently. A region the decoder
// emitted as prose could have been code, which loses code loudly: the emitted
// program breaks. A region it emitted as code could have been prose, which is
// the dangerous one: the emitted program still runs but does the wrong thing.
type Ambiguity struct {
	// Span covers the region as it appears in the source.
	Span core.Span
	// Text is the region's source text.
	Text string
	// Chosen is the label the decoder gave the region.
	Chosen Label
	// ChosenCost and AlternativeCost are the costs of the labelling that was
	// emitted and of the labelling in which the region carries the other label.
	ChosenCost      float64
	AlternativeCost float64
}

// Margin is the difference between the two costs.
func (a Ambiguity) Margin() float64 { return a.AlternativeCost - a.ChosenCost }

// Alternative is the label the region could have carried instead.
func (a Ambiguity) Alternative() Label {
	if a.Chosen == Code {
		return English
	}
	return Code
}

// LabelsFromPath turns a decoded path into one label per token.
func LabelsFromPath(toks []host.Token, path *host.Path) []Label {
	labels := make([]Label, len(toks))
	for i, tok := range toks {
		switch {
		case tok.IsTrivia():
			labels[i] = Trivia
		case path != nil && i < len(path.Actions) && path.Actions[i] == host.KeepToken:
			labels[i] = Code
		default:
			labels[i] = English
		}
	}
	return labels
}

// pinned wraps a cost model and pins a set of tokens to one decision, so that
// an alternative reading can be tested without touching any grammar.
//
// A token pinned to code cannot be skipped; a token pinned to prose cannot be
// kept. If no parse exists under the pin, the original decision was forced and
// there is nothing to report.
type pinned struct {
	inner   host.Costs
	asCode  map[int]bool
	asProse map[int]bool
}

func (p *pinned) Keep(i int) float64 {
	if p.asProse[i] {
		return host.ImpossibleCost
	}
	return p.inner.Keep(i)
}

func (p *pinned) Skip(i int) float64 {
	if p.asCode[i] {
		return host.ImpossibleCost
	}
	return p.inner.Skip(i)
}

// PinForTest exposes the pinning wrapper so that its two directions can be
// tested directly. Nothing in the pipeline uses it.
func PinForTest(inner host.Costs, asCode, asProse map[int]bool) host.Costs {
	return &pinned{inner: inner, asCode: asCode, asProse: asProse}
}

// FindAmbiguities reports regions whose other reading is feasible at a cost
// within margin of the labelling that was chosen.
//
// Both directions are tested, but with different targeting, because they fail
// differently.
//
// Every English region is tested for being code: dropping code is the failure
// that breaks the emitted program, and prose regions are few.
//
// The reverse is tested only for kept tokens that are words the lexicon knows,
// most common English first. Testing whole code runs instead would be useless,
// because in every language deleting a statement is usually possible, so nearly
// every statement would look ambiguous; and testing them all would exceed any
// sane budget. A kept common English word is precisely the shape of the
// dangerous failure, where prose is read as code and the program still runs.
func FindAmbiguities(
	src *core.Source,
	machine host.Machine,
	lex *Lexicon,
	vocab host.Vocabulary,
	toks []host.Token,
	base *host.Path,
	margin float64,
) ([]Ambiguity, bool) {
	if machine == nil || base == nil || len(toks) == 0 {
		return nil, false
	}

	costs := NewCosts(toks, lex, vocab)
	labels := LabelsFromPath(toks, base)

	type probe struct {
		start, end int
		chosen     Label
		asCode     bool
	}
	var probes []probe

	for _, r := range englishRegions(toks, labels) {
		probes = append(probes, probe{start: r.start, end: r.end, chosen: English, asCode: true})
	}
	for _, i := range suspiciousWords(toks, labels, lex, MaxAmbiguityProbes) {
		probes = append(probes, probe{start: i, end: i, chosen: Code})
	}

	var out []Ambiguity
	truncated := false

	target := make(map[int]bool, 1)
	for n, pr := range probes {
		if n >= MaxAmbiguityProbes {
			truncated = true
			break
		}
		clear(target)
		for i := pr.start; i <= pr.end; i++ {
			if !toks[i].IsTrivia() {
				target[i] = true
			}
		}
		if len(target) == 0 {
			continue
		}

		pin := &pinned{inner: costs}
		if pr.asCode {
			pin.asCode = target
		} else {
			pin.asProse = target
		}

		alt, ok := machine.Decode(toks, pin)
		if !ok {
			// No parse exists under the pin, so the decision was forced.
			continue
		}
		if delta := alt.Cost - base.Cost; delta <= margin {
			out = append(out, Ambiguity{
				Span:            src.Span(toks[pr.start].Span.Start, toks[pr.end].Span.End),
				Text:            regionText(toks, pr.start, pr.end),
				Chosen:          pr.chosen,
				ChosenCost:      base.Cost,
				AlternativeCost: alt.Cost,
			})
		}
	}
	return out, truncated
}

// suspiciousWords returns the indices of kept tokens that are words a common
// English speaker would recognise, cheapest English first.
//
// These are the tokens where reading prose as code is plausible. A kept
// identifier that looks like code — "order_t", "MAX_LINES" — is never
// suspicious, and neither is a keyword, which the grammar reached for on
// purpose.
func suspiciousWords(toks []host.Token, labels []Label, lex *Lexicon, limit int) []int {
	type scored struct {
		index int
		cost  float64
	}
	var found []scored
	for i, tok := range toks {
		if labels[i] != Code || !tok.Kind.IsWord() {
			continue
		}
		if LooksLikeCode(tok.Text) {
			continue
		}
		cost, ok := lex.LogCost(tok.Text)
		if !ok {
			continue
		}
		found = append(found, scored{index: i, cost: cost})
	}
	sort.SliceStable(found, func(a, b int) bool { return found[a].cost < found[b].cost })
	if len(found) > limit {
		found = found[:limit]
	}
	out := make([]int, 0, len(found))
	for _, s := range found {
		out = append(out, s.index)
	}
	return out
}
