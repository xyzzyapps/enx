package perl

import "enx/internal/host"

// Types reads the type that a Perl assignment writes for its value.
//
// Perl is the one host where the container type is in the *name*: a sigil says
// whether a variable holds a scalar, an array or a hash. That is a genuine
// advantage, and this reader uses it. On top of that it recognises a class
// constructor, "Class->new(...)", which is the standard Perl idiom and spells the
// class out in the source. The constructor must be the whole right hand side:
// "Point->new->name" is a string, not a Point.
//
// Everything else — a method call result, an arithmetic expression — reports
// ok=false, so the binding has no type and a typed anaphor cannot reach it.
type Types struct{}

// NewTypes returns the Perl type reader.
func NewTypes() *Types { return &Types{} }

// AssignedType implements host.TypeReader.
func (t *Types) AssignedType(toks []host.Token, nameIndex, eqIndex int) (string, bool, bool) {
	// A class constructor wins over the sigil, because it is the more specific
	// answer: "$p = Point->new" is a Point, not merely a scalar. It must be the
	// whole right hand side: "Point->new->name" is a string, not a Point.
	if i := nextSignificant(toks, eqIndex+1); i >= 0 {
		if toks[i].Kind == host.Constant || toks[i].Kind == host.Identifier {
			name, after := nameChain(toks, i)
			if name != "" {
				if end, ok := newCallEnd(toks, after); ok && endsStatement(toks, end) {
					return lastSegment(name), false, true
				}
			}
		}
	}

	// Otherwise the sigil decides the container type.
	if nameIndex >= 0 && nameIndex < len(toks) {
		text := toks[nameIndex].Text
		if len(text) > 0 && toks[nameIndex].Kind == host.Variable {
			switch text[0] {
			case '@':
				return "array", false, true
			case '%':
				return "hash", false, true
			case '$':
				return "scalar", false, true
			}
		}
	}
	return "", false, false
}

// nameChain reads "Point" or "Point::Sub" from i, returning the name and the
// index just past it.
func nameChain(toks []host.Token, i int) (string, int) {
	name := toks[i].Text
	j := i + 1
	for {
		sep := nextSignificant(toks, j)
		if sep < 0 || toks[sep].Text != "::" {
			return name, j
		}
		part := nextSignificant(toks, sep+1)
		if part < 0 {
			return name, j
		}
		name += "::" + toks[part].Text
		j = part + 1
	}
}

// endsStatement reports whether the next significant token is the end of a
// statement: a semicolon, a closing brace, or end of input. Newlines are
// whitespace in Perl, so they are skipped.
func endsStatement(toks []host.Token, from int) bool {
	i := nextSignificant(toks, from)
	return i < 0 || toks[i].Text == ";" || toks[i].Text == "}"
}

// balancedEnd returns the token index just past the matching close token.
func balancedEnd(toks []host.Token, start int, open, close string) (int, bool) {
	depth := 0
	for j := start; j < len(toks); j++ {
		if toks[j].IsTrivia() {
			continue
		}
		switch toks[j].Text {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return j + 1, true
			}
		}
	}
	return 0, false
}

// newCallEnd recognises "->new" followed by optional parenthesised arguments
// and returns the index just past the call.
func newCallEnd(toks []host.Token, i int) (int, bool) {
	arrow := nextSignificant(toks, i)
	if arrow < 0 || toks[arrow].Text != "->" {
		return 0, false
	}
	name := nextSignificant(toks, arrow+1)
	if name < 0 || toks[name].Text != "new" {
		return 0, false
	}
	next := nextSignificant(toks, name+1)
	if next >= 0 && toks[next].Text == "(" {
		return balancedEnd(toks, next, "(", ")")
	}
	return name + 1, true
}

// lastSegment returns the final component of a "A::B" name.
func lastSegment(name string) string {
	for i := len(name) - 2; i >= 0; i-- {
		if name[i] == ':' && name[i+1] == ':' {
			return name[i+2:]
		}
	}
	return name
}

func nextSignificant(toks []host.Token, i int) int {
	for j := i; j < len(toks); j++ {
		if !toks[j].IsTrivia() {
			return j
		}
	}
	return -1
}
