// Package core holds the types shared by every stage of the enx pipeline:
// byte spans, sources, diagnostics, and logging.
package core

import "fmt"

// Pos is a one-based line and column pair. Columns count runes, not bytes.
type Pos struct {
	Line int
	Col  int
}

// String renders the position as "line:col".
func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

// Span is a half-open byte range [Start, End) inside a Source, annotated with
// the positions of both bounds. It is the unit of location used by tokens,
// diagnostics, and switch-point reports.
type Span struct {
	Start, End       int
	StartPos, EndPos Pos
}

// Len returns the number of bytes covered by the span.
func (s Span) Len() int { return s.End - s.Start }

// Empty reports whether the span covers no bytes.
func (s Span) Empty() bool { return s.End <= s.Start }

// Contains reports whether the byte offset off lies inside the span.
func (s Span) Contains(off int) bool { return off >= s.Start && off < s.End }

// String renders the raw byte range.
func (s Span) String() string { return fmt.Sprintf("%d-%d", s.Start, s.End) }
