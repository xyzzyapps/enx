package host

// MatchAnaphor reports whether an anaphor marker begins at offset i, and returns
// the offset just past it.
//
// The marker is the caller's choice, and nothing here decides that one is in
// use: an empty marker means the feature is off. That matters because every
// claimed form collides with real syntax somewhere. With '!' as the marker,
// "!-1" is logical-not of minus one and is silently valid in all three hosts,
// and "!Point" is valid in Ruby. Only "!-2:Point" is a hard error everywhere.
// See SPEC section 18.2.
//
// Two forms are claimed:
//
//	marker "-" digits [ ":" type ]     an ordinal, optionally filtered by type
//	marker type                        the most recent of a type
//
// A type is an identifier, optionally namespace-qualified with "::", optionally
// followed by "*" for a pointer. So with '!' claimed:
//
//	!-1            the most recent assignment
//	!-2            the one before it
//	!Point         the most recent Point
//	!-2:Point      the second most recent Point
//	!-1:char*      the most recent pointer-to-char
//
// Unclaimed forms are left alone: "!1" (no minus) and "!-1x" fall through to the
// host as ordinary code.
func MatchAnaphor(b []byte, i int, marker byte) (int, bool) {
	if marker == 0 || i < 0 || i >= len(b) || b[i] != marker {
		return 0, false
	}
	j := i + 1
	if j >= len(b) {
		return 0, false
	}

	if b[j] == '-' {
		// Ordinal form.
		j++
		start := j
		for j < len(b) && b[j] >= '0' && b[j] <= '9' {
			j++
		}
		if j == start {
			return 0, false
		}
		// "!-1x" is not a marker.
		if j < len(b) && isIdentPartByte(b[j]) {
			return 0, false
		}
		if j < len(b) && b[j] == ':' {
			end, ok := scanType(b, j+1)
			if !ok {
				return 0, false
			}
			j = end
		}
		return j, true
	}

	// Type-only form.
	return scanType(b, j)
}

// scanType consumes a type name at i and returns the offset just past it.
//
// A type is one or more identifiers joined by "::", with an optional trailing
// "*". The pointer suffix is expressible precisely because "*" is no longer the
// anaphor marker: "!-1:char*" distinguishes pointer-to-char from plain char.
func scanType(b []byte, i int) (int, bool) {
	if i >= len(b) || !isIdentStartByte(b[i]) {
		return 0, false
	}
	j := i
	for {
		for j < len(b) && isIdentPartByte(b[j]) {
			j++
		}
		// "A::B" continues the name. The bounds check has to cover the
		// character after the separator, not the separator itself: "!A::" at
		// end of input must not read past the buffer. A dangling "::" is not a
		// type, so the whole marker is refused rather than split into a marker
		// and a stray operator.
		if j+1 < len(b) && b[j] == ':' && b[j+1] == ':' {
			if j+2 < len(b) && isIdentStartByte(b[j+2]) {
				j += 2
				continue
			}
			return 0, false
		}
		break
	}
	if j < len(b) && b[j] == '*' {
		j++
	}
	// A type running into more identifier characters is not a type, so the
	// marker is not claimed: "!Pointx" is already the name "Pointx".
	return j, true
}

// AnaphorRef is a parsed anaphor marker.
//
// It is named Ref rather than Anaphor because Anaphor is a token kind.
type AnaphorRef struct {
	// Position is how far back to look, where 1 is the most recent assignment.
	Position int
	// Type is the required type name, or "" for any. A trailing "*" means the
	// referent must be a pointer.
	Type string
}

// ParseAnaphor parses an anaphor token such as "!-2:Point" or "!Point".
func ParseAnaphor(text string) (AnaphorRef, bool) {
	if len(text) < 2 {
		return AnaphorRef{}, false
	}
	body := text[1:]

	a := AnaphorRef{Position: 1}
	if body[0] == '-' {
		i := 1
		n := 0
		for i < len(body) && body[i] >= '0' && body[i] <= '9' {
			// Saturate rather than overflow: a position beyond any possible
			// history simply will not resolve, which is the desired behaviour.
			if n < 1_000_000 {
				n = n*10 + int(body[i]-'0')
			}
			i++
		}
		if n == 0 {
			return AnaphorRef{}, false
		}
		a.Position = n
		body = body[i:]
	}
	if body == "" {
		return a, true
	}
	// A type follows either as ":Type" after an ordinal, or directly as "Type"
	// when the ordinal was omitted.
	typ := body
	if typ[0] == ':' {
		typ = typ[1:]
	}
	if typ == "" || !isIdentStartByte(typ[0]) {
		return AnaphorRef{}, false
	}
	a.Type = typ
	return a, true
}
