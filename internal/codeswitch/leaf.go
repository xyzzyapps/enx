package codeswitch

import (
	"strings"

	"enx/internal/host"
)

// This file is the leaf layer: these functions use the standard library,
// model fields and their arguments only. Costs, decoding and planning are
// integrators.

// LooksLikeCode reports whether the shape of a bare word marks it as an
// identifier rather than prose: underscores, digits, CamelCase, SCREAMING
// case, or unusual length.
func LooksLikeCode(text string) bool {
	if text == "" {
		return false
	}
	if len(text) > maxIdentifierLength {
		return true
	}
	if strings.ContainsAny(text, "_0123456789") {
		return true
	}

	seenLower := false
	allUpper := len(text) >= 2
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c >= 'a' && c <= 'z':
			seenLower = true
			allUpper = false
		case c >= 'A' && c <= 'Z':
			if seenLower {
				return true
			}
		default:
			allUpper = false
		}
	}
	return allUpper
}

// isLayout reports whether tok is spacing or a line break. Comments are trivia
// too, but they are content and must not be swallowed into an English region.
func isLayout(tok host.Token) bool {
	return tok.Kind == host.Space || tok.Kind == host.Newline
}

// isMethodName reports whether token i is glued to a preceding method
// separator, as in "order.count" or "Product.new".
func (c *Costs) isMethodName(i int) bool {
	if i == 0 {
		return false
	}
	prev := c.toks[i-1]
	switch prev.Text {
	case ".", "&.", "::":
	default:
		return false
	}
	return prev.Span.End == c.toks[i].Span.Start
}

// isReceiver reports whether token i is the receiver of an adjacent member
// access, as in "other.cents" or "A::B". Both joins must be adjacent: that is
// what distinguishes code from a word followed by an English full stop.
func (c *Costs) isReceiver(i int) bool {
	if i+2 >= len(c.toks) {
		return false
	}
	sep := c.toks[i+1]
	switch sep.Text {
	case ".", "&.", "::":
	default:
		return false
	}
	name := c.toks[i+2]
	if name.Kind != host.Identifier && name.Kind != host.Constant {
		return false
	}
	return c.toks[i].Span.End == sep.Span.Start && sep.Span.End == name.Span.Start
}

// regionText returns the source bytes covered by the token range.
func regionText(toks []host.Token, start, end int) string {
	var b strings.Builder
	for i := start; i <= end; i++ {
		b.WriteString(toks[i].Text)
	}
	return b.String()
}

// trailingIndent returns the spaces and tabs after the last newline in text.
// Because a region absorbs the layout that follows it, this is the indentation
// of the line that comes next, so the separator can reproduce it.
func trailingIndent(text string) string {
	i := strings.LastIndexByte(text, '\n')
	if i < 0 {
		return ""
	}
	rest := text[i+1:]
	for j := 0; j < len(rest); j++ {
		if rest[j] != ' ' && rest[j] != '\t' {
			return rest[:j]
		}
	}
	return rest
}
