package core

import (
	"sort"
	"unicode/utf8"
)

// Source is an immutable input file together with a line index. All spans and
// offsets in the pipeline refer to a Source, so a diagnostic can always be
// rendered back to the original text.
type Source struct {
	// Name is the path or logical name shown in diagnostics.
	Name string

	text   []byte
	starts []int // byte offset of the first byte of each line
}

// NewSource wraps text and builds its line index.
func NewSource(name string, text []byte) *Source {
	s := &Source{Name: name, text: text}

	// A trailing newline does not open a new line.
	starts := make([]int, 1, 1+len(text)/32)
	starts[0] = 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' && i+1 < len(text) {
			starts = append(starts, i+1)
		}
	}
	s.starts = starts
	return s
}

// Bytes returns the raw input. Callers must not modify it.
func (s *Source) Bytes() []byte { return s.text }

// Len returns the input length in bytes.
func (s *Source) Len() int { return len(s.text) }

// LineCount returns the number of lines, which is at least one.
func (s *Source) LineCount() int { return len(s.starts) }

// LineStart returns the byte offset at which the one-based line begins.
func (s *Source) LineStart(line int) int {
	if line < 1 {
		return 0
	}
	if line > len(s.starts) {
		return len(s.text)
	}
	return s.starts[line-1]
}

// LineEnd returns the byte offset just past the last byte of the one-based
// line, excluding its newline terminator.
func (s *Source) LineEnd(line int) int {
	if line < 1 || line > len(s.starts) {
		return len(s.text)
	}
	end := len(s.text)
	if line < len(s.starts) {
		end = s.starts[line] - 1 // drop '\n'
	} else if end > 0 && s.text[end-1] == '\n' {
		// The last line may still be terminated, in which case the trailing
		// newline is not part of its content.
		end--
	}
	if end > s.LineStart(line) && s.text[end-1] == '\r' {
		end--
	}
	return end
}

// LineText returns the one-based line without its terminator.
func (s *Source) LineText(line int) string {
	return string(s.text[s.LineStart(line):s.LineEnd(line)])
}

// LineOf returns the one-based line containing the byte offset.
func (s *Source) LineOf(off int) int {
	return s.lineIndex(off) + 1
}

func (s *Source) lineIndex(off int) int {
	if off < 0 {
		off = 0
	}
	if off > len(s.text) {
		off = len(s.text)
	}
	i := sort.Search(len(s.starts), func(i int) bool { return s.starts[i] > off }) - 1
	if i < 0 {
		return 0
	}
	return i
}

// Pos returns the position of the byte offset.
func (s *Source) Pos(off int) Pos {
	li := s.lineIndex(off)
	start := s.starts[li]
	col := utf8.RuneCount(s.text[start:off]) + 1
	return Pos{Line: li + 1, Col: col}
}

// Span builds a Span from byte offsets, clamping them to the input.
func (s *Source) Span(start, end int) Span {
	if start < 0 {
		start = 0
	}
	if end > len(s.text) {
		end = len(s.text)
	}
	if end < start {
		end = start
	}
	return Span{
		Start:    start,
		End:      end,
		StartPos: s.Pos(start),
		EndPos:   s.Pos(end),
	}
}

// Slice returns the text covered by the span.
func (s *Source) Slice(sp Span) string { return string(s.text[sp.Start:sp.End]) }

// String returns the source name.
func (s *Source) String() string { return s.Name }
