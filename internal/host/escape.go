package host

// MatchEscape reports whether a forced-prose escape begins at offset i, and
// returns the offset just past the closing tilde.
//
// The escape exists for the rare span that is genuinely ambiguous to both the
// grammar and the lexicon, where a human knows the answer and wants to say so
// rather than hope. It is off unless written, and it never appears in the
// output: the delimiters are dropped with the prose they mark.
//
// The rule is deliberately narrow, because "~" is a real operator in all three
// languages. A tilde only opens an escape when it stands alone between
// whitespace, and an escape only closes at another tilde that stands alone, on
// the same line. So:
//
//	~ the total ~       an escape
//	x = ~y;             ordinary code
//	x = ~y + ~z;        ordinary code, two tildes but neither stands alone
//	a ~ b               no closing tilde, so not an escape
//
// The same-line requirement keeps an unterminated tilde from swallowing the
// rest of the file.
func MatchEscape(b []byte, i int) (int, bool) {
	if i < 0 || i >= len(b) || b[i] != '~' {
		return 0, false
	}
	// The opening tilde must stand alone: whitespace or the start of the file
	// before it, whitespace after it.
	if i > 0 && !isSpaceByte(b[i-1]) {
		return 0, false
	}
	if i+1 >= len(b) || !isSpaceByte(b[i+1]) {
		return 0, false
	}
	for j := i + 1; j < len(b); j++ {
		switch b[j] {
		case '\n':
			return 0, false
		case '~':
			if !isSpaceByte(b[j-1]) {
				continue
			}
			if j+1 < len(b) && !isSpaceByte(b[j+1]) {
				continue
			}
			return j + 1, true
		}
	}
	return 0, false
}
