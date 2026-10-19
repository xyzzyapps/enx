package codeswitch

import (
	"strings"

	"enx/internal/core"
	"enx/internal/host"
)

// RunKind distinguishes verbatim code from a dropped English region.
type RunKind uint8

const (
	// RunCode is a stretch of code, including its spacing, copied verbatim.
	RunCode RunKind = iota
	// RunEnglish is a region of prose that is replaced.
	RunEnglish
)

// String renders the run kind in lower case.
func (k RunKind) String() string {
	if k == RunEnglish {
		return "english"
	}
	return "code"
}

// Run is either a verbatim stretch of code or an English region.
type Run struct {
	Kind RunKind
	// First and Last are inclusive token indices.
	First, Last int
	// Span covers the bytes of the run.
	Span core.Span
	// Replacement is the text substituted for an English run. It is empty for
	// code runs.
	Replacement string
}

// Separator decides what replaces a dropped English region.
type Separator interface {
	// Replacement returns the text substituted for the English region
	// regionText, which sat between the code tokens at indices prev and next.
	// Either index is -1 at the file boundaries.
	Replacement(regionText string, prev, next int) string
}

// LexicalSeparator replaces an English region using spelling alone: a region
// that spanned a line break disappears entirely, and anything else becomes a
// single space.
//
// It cannot distinguish a statement boundary from the inside of an expression,
// so it never inserts the newline that a multi-statement line needs, and it
// cannot see that a newline after "return" would change the meaning. The
// grammar decoder's separator supersedes it.
type LexicalSeparator struct{}

// Replacement implements Separator.
func (LexicalSeparator) Replacement(regionText string, prev, next int) string {
	if prev < 0 || next < 0 {
		return ""
	}
	if strings.Contains(regionText, "\n") {
		return ""
	}
	return " "
}

type region struct {
	start int
	end   int
	// lastEnglish is the index of the last English token before the trailing
	// layout was absorbed. It is kept for reporting.
	lastEnglish int
}

// englishRegions finds maximal English regions. Two English tokens belong to
// the same region when only layout separates them.
//
// The region absorbs the layout immediately before and after it, so that it can
// be replaced as a unit. Absorbing the trailing layout is what lets a separator
// remove a newline when one must not survive, and re-emit the following line's
// indentation when one must.
//
// Comments are deliberately not absorbed and do not join two regions. They are
// trivia, but they are never English: deleting a comment because it happened to
// stand next to a sentence would break the promise that trivia is always
// preserved.
func englishRegions(toks []host.Token, labels []Label) []region {
	var regions []region
	i := 0
	for i < len(toks) {
		if labels[i] != English {
			i++
			continue
		}
		start, end := i, i
		j := i + 1
		for j < len(toks) {
			if labels[j] == English {
				end = j
				j++
				continue
			}
			if isLayout(toks[j]) {
				k := j
				for k < len(toks) && isLayout(toks[k]) {
					k++
				}
				if k < len(toks) && labels[k] == English {
					j = k
					continue
				}
			}
			break
		}

		// Absorb preceding layout, but never cross into the previous region.
		limit := 0
		if len(regions) > 0 {
			limit = regions[len(regions)-1].end + 1
		}
		s := start
		for s > limit && isLayout(toks[s-1]) {
			s--
		}
		// Absorb trailing layout.
		e := end
		for e+1 < len(toks) && isLayout(toks[e+1]) {
			e++
		}

		regions = append(regions, region{start: s, end: e, lastEnglish: end})
		i = j
	}
	return regions
}

// isLayout is in leaf.go.

// Split groups tokens into code runs and English regions. The runs are
// contiguous and in token order, so concatenating their byte spans reproduces
// the input exactly.
func Split(toks []host.Token, labels []Label, sep Separator) []Run {
	regions := englishRegions(toks, labels)
	runs := make([]Run, 0, 2*len(regions)+1)

	pos := 0
	for _, r := range regions {
		if r.start > pos {
			runs = append(runs, Run{
				Kind:  RunCode,
				First: pos,
				Last:  r.start - 1,
				Span:  core.Span{Start: toks[pos].Span.Start, End: toks[r.start-1].Span.End},
			})
		}
		runs = append(runs, Run{
			Kind:  RunEnglish,
			First: r.start,
			Last:  r.end,
			Span:  core.Span{Start: toks[r.start].Span.Start, End: toks[r.end].Span.End},
			Replacement: sep.Replacement(
				regionText(toks, r.start, r.end),
				prevCodeIndex(toks, r.start),
				nextCodeIndex(toks, r.end),
			),
		})
		pos = r.end + 1
	}
	if pos < len(toks) {
		runs = append(runs, Run{
			Kind:  RunCode,
			First: pos,
			Last:  len(toks) - 1,
			Span:  core.Span{Start: toks[pos].Span.Start, End: toks[len(toks)-1].Span.End},
		})
	}
	return runs
}

// regionText is in leaf.go.

// prevCodeIndex returns the index of the last non-trivia token before i, or -1.
func prevCodeIndex(toks []host.Token, i int) int {
	for j := i - 1; j >= 0; j-- {
		if !toks[j].IsTrivia() {
			return j
		}
	}
	return -1
}

// nextCodeIndex returns the index of the first non-trivia token after i, or -1.
func nextCodeIndex(toks []host.Token, i int) int {
	for j := i + 1; j < len(toks); j++ {
		if !toks[j].IsTrivia() {
			return j
		}
	}
	return -1
}

// Plan is the emission plan for one file: verbatim code stretches interleaved
// with replacements for the English that was dropped.
type Plan struct {
	// Source is the file the plan was built from.
	Source *core.Source
	// Runs covers the whole file, in order.
	Runs []Run
	// Substitute, when set, supplies the text for a code token instead of its
	// own bytes. It is how a resolved anaphor is replaced by the name it stands
	// for. A nil Substitute means every code token is copied verbatim, which is
	// the behaviour when anaphora is off.
	Substitute func(tokenIndex int) string
	// Tokens is required when Substitute is set, so the plan can map byte
	// offsets back to token indices.
	Tokens []host.Token
}

// Text renders the emitted program: code verbatim, English replaced.
//
// When a Substitute is set, code runs are rendered token by token so that a
// resolved anaphor can be replaced by the name it stands for. The bytes between
// tokens, which are always trivia, are copied unchanged.
func (p *Plan) Text() string {
	var b strings.Builder
	for _, r := range p.Runs {
		switch r.Kind {
		case RunCode:
			p.writeCode(&b, r)
		case RunEnglish:
			b.WriteString(r.Replacement)
		}
	}
	return b.String()
}

// writeCode copies a code run, substituting token text where asked.
//
// Run records the token range it covers, so the walk is direct: no byte-offset
// search, and trivia inside the run is copied unchanged.
func (p *Plan) writeCode(b *strings.Builder, r Run) {
	if p.Substitute == nil || len(p.Tokens) == 0 {
		b.Write(p.Source.Bytes()[r.Span.Start:r.Span.End])
		return
	}
	first, last := r.First, r.Last
	if first < 0 {
		first = 0
	}
	if last >= len(p.Tokens) {
		last = len(p.Tokens) - 1
	}
	for i := first; i <= last; i++ {
		if i < 0 || i >= len(p.Tokens) {
			continue
		}
		if p.Tokens[i].IsTrivia() {
			b.WriteString(p.Tokens[i].Text)
		} else {
			b.WriteString(p.Substitute(i))
		}
	}
}

// CodeText returns only the verbatim code, with every replacement ignored.
//
// The emitter's core guarantee is that this is a subsequence of the input: the
// tool decides, but it never rewrites.
func (p *Plan) CodeText() string {
	var b strings.Builder
	for _, r := range p.Runs {
		if r.Kind == RunCode {
			b.Write(p.Source.Bytes()[r.Span.Start:r.Span.End])
		}
	}
	return b.String()
}

// TextWithProse renders the program with each dropped English region preserved
// as host comments, which is useful when checking a labelling by eye.
// commentPrefix is the host's line comment introducer, such as "#" or "//".
func (p *Plan) TextWithProse(commentPrefix string) string {
	var b strings.Builder
	for _, r := range p.Runs {
		switch r.Kind {
		case RunCode:
			// Use the same path as Text so that --keep-prose and --anaphor
			// compose: a resolved anaphor must still be replaced by its name.
			p.writeCode(&b, r)
		case RunEnglish:
			prose := p.Source.Slice(r.Span)
			for _, line := range strings.Split(prose, "\n") {
				trimmed := strings.TrimSpace(line)
				if trimmed == "" {
					continue
				}
				b.WriteString(commentPrefix)
				b.WriteString(" ")
				b.WriteString(trimmed)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

// MachineSeparator replaces an English region using the statement boundaries
// the grammar reported, so that a newline is inserted only where one is legal.
//
// This is what makes "return the total" safe: after "return", the grammar has
// not reached a statement boundary, so the region becomes a space and the
// operand stays on the same line. A grammar-free separator would delete the
// line break and turn the statement into a bare "return".
type MachineSeparator struct {
	sepAfter map[int]bool
	src      *core.Source
	toks     []host.Token
}

// NewMachineSeparator builds a separator from a decoded path. src and toks are
// optional; when present, a newline reuses the next code token's own line
// indentation rather than guessing from the text that was deleted.
func NewMachineSeparator(path *host.Path, src *core.Source, toks []host.Token) MachineSeparator {
	s := MachineSeparator{src: src, toks: toks}
	if path != nil {
		s.sepAfter = path.SeparatorAfter
	}
	return s
}

// Replacement implements Separator.
func (s MachineSeparator) Replacement(regionText string, prev, next int) string {
	if prev < 0 || next < 0 {
		return ""
	}
	// If the source already had a line break in the region, keep one. That is
	// what protects a bare "return" followed by prose on the next line, and it
	// is the least surprising choice for every other construct too.
	if strings.Contains(regionText, "\n") || s.sepAfter[prev] {
		if indent, ok := s.nextIndent(next); ok {
			return "\n" + indent
		}
		return "\n" + trailingIndent(regionText)
	}
	return " "
}

// nextIndent returns the indentation of the line that starts the next code
// token, when the token is preceded only by layout on that line.
func (s MachineSeparator) nextIndent(next int) (string, bool) {
	if s.src == nil || next < 0 || next >= len(s.toks) {
		return "", false
	}
	tok := s.toks[next]
	start := s.src.LineStart(tok.Span.StartPos.Line)
	if start < 0 || start > tok.Span.Start || tok.Span.Start > s.src.Len() {
		return "", false
	}
	prefix := s.src.Bytes()[start:tok.Span.Start]
	for _, b := range prefix {
		if b != ' ' && b != '\t' {
			return "", false
		}
	}
	return string(prefix), true
}

// trailingIndent is in leaf.go.
