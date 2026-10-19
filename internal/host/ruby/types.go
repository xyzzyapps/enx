package ruby

import (
	"strings"

	"enx/internal/host"
)

// Types reads the class that a Ruby assignment writes for its value.
//
// Ruby has no declared types, so this is a reader and not an inference engine.
// It recognises exactly the two places where the class is spelled out in the
// source: a literal, whose class is its form, and a constructor call, whose
// class is the constant before ".new". In both cases the expression must be the
// whole right hand side; "Point.new.to_s" is a String, not a Point. Everything
// else — a method call, an arithmetic expression, an attribute read — reports
// ok=false, so the binding has no type and a typed anaphor cannot reach it.
//
// Reading the target of a call on the left hand side instead would be inference,
// and a wrong guess here does not raise: it silently selects the wrong referent.
type Types struct{}

// NewTypes returns the Ruby type reader.
func NewTypes() *Types { return &Types{} }

// classReturningConstructors do not return an instance of themselves.
//
// "Struct.new(:a)" returns a class, not a Struct, and the same goes for Class,
// Module and Data. Excluding them is what keeps "X = Struct.new(...)" from making
// X's type "Struct", which would be wrong in the one case where a constructor is
// most often used.
var classReturningConstructors = map[string]bool{
	"Struct": true, "Class": true, "Module": true, "Data": true,
}

// AssignedType implements host.TypeReader.
func (t *Types) AssignedType(toks []host.Token, nameIndex, eqIndex int) (string, bool, bool) {
	i := nextSignificant(toks, eqIndex+1)
	if i < 0 {
		return "", false, false
	}
	// A comma at the top level means the right hand side is a list of values,
	// so a target receives an array, not the first value. "a, b = 1, \"x\"" and
	// "x = 1, 2" must not type a or b from the first element.
	if hasTopLevelComma(toks, i) {
		return "", false, false
	}
	tok := toks[i]

	// A literal's class is its form, but only when the literal is the whole
	// right hand side. "x = 1 + 2" and "x = [1, 2].first" do not have a written
	// type for x, even though the first token has one.
	switch tok.Kind {
	case host.Int:
		if endsStatement(toks, i+1) {
			return "Integer", false, true
		}
		return "", false, false
	case host.Float:
		if endsStatement(toks, i+1) {
			return "Float", false, true
		}
		return "", false, false
	case host.String, host.Heredoc:
		if endsStatement(toks, i+1) {
			if tok.Kind == host.String && isPercentArray(tok.Text) {
				return "Array", false, true
			}
			return "String", false, true
		}
		return "", false, false
	case host.Symbol:
		if endsStatement(toks, i+1) {
			return "Symbol", false, true
		}
		return "", false, false
	case host.Regex:
		if endsStatement(toks, i+1) {
			return "Regexp", false, true
		}
		return "", false, false
	}
	switch tok.Text {
	case "[":
		if end, ok := balancedEnd(toks, i, "[", "]"); ok && endsStatement(toks, end) {
			return "Array", false, true
		}
		return "", false, false
	case "{":
		if end, ok := balancedEnd(toks, i, "{", "}"); ok && endsStatement(toks, end) {
			return "Hash", false, true
		}
		return "", false, false
	case "nil", "true", "false":
		if !endsStatement(toks, i+1) {
			return "", false, false
		}
		switch tok.Text {
		case "nil":
			return "NilClass", false, true
		case "true":
			return "TrueClass", false, true
		default:
			return "FalseClass", false, true
		}
	}

	// A constructor call: Const.new, or A::B.new, and it must be the whole
	// right hand side. "Point.new.to_s" is a String, not a Point.
	if tok.Kind == host.Constant {
		name, after := constantChain(toks, i)
		if name != "" {
			if end, ok := newCallEnd(toks, after); ok && endsStatement(toks, end) {
				short := lastSegment(name)
				if classReturningConstructors[short] {
					return "", false, false
				}
				return short, false, true
			}
		}
	}
	return "", false, false
}

// isPercentArray reports whether a percent literal produces an array. The
// lexer classifies every percent literal as String, Regex or Symbol; %w and %i
// are the array-valued forms.
func isPercentArray(text string) bool {
	return strings.HasPrefix(text, "%w") || strings.HasPrefix(text, "%W") ||
		strings.HasPrefix(text, "%i") || strings.HasPrefix(text, "%I")
}

// endsStatement reports whether the next token after from is a statement end,
// a newline, or the end of input. Comments are trivia and are skipped.
func endsStatement(toks []host.Token, from int) bool {
	for i := from; i < len(toks); i++ {
		tok := toks[i]
		if tok.Kind == host.Newline || tok.Text == ";" {
			return true
		}
		if tok.IsTrivia() {
			continue
		}
		return false
	}
	return true
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

// newCallEnd recognises ".new" followed by optional parenthesised arguments and
// returns the index just past the call.
func newCallEnd(toks []host.Token, i int) (int, bool) {
	dot := nextSignificant(toks, i)
	if dot < 0 || toks[dot].Text != "." {
		return 0, false
	}
	name := nextSignificant(toks, dot+1)
	if name < 0 || toks[name].Text != "new" {
		return 0, false
	}
	next := nextSignificant(toks, name+1)
	if next >= 0 && toks[next].Text == "(" {
		return balancedEnd(toks, next, "(", ")")
	}
	return name + 1, true
}

// hasTopLevelComma reports whether a comma outside brackets appears on the
// right hand side before the statement ends.
func hasTopLevelComma(toks []host.Token, start int) bool {
	depth := 0
	for j := start; j < len(toks); j++ {
		tok := toks[j]
		if tok.Kind == host.Newline && depth == 0 {
			return false
		}
		if tok.IsTrivia() {
			continue
		}
		switch tok.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth > 0 {
				depth--
			}
		case ",":
			if depth == 0 {
				return true
			}
		case ";":
			if depth == 0 {
				return false
			}
		}
	}
	return false
}

// constantChain reads "A::B::C" from i and returns the full name and the index
// just past it.
func constantChain(toks []host.Token, i int) (string, int) {
	name := toks[i].Text
	j := i + 1
	for {
		sep := nextSignificant(toks, j)
		if sep < 0 || toks[sep].Text != "::" {
			return name, j
		}
		part := nextSignificant(toks, sep+1)
		if part < 0 || (toks[part].Kind != host.Constant && toks[part].Kind != host.Identifier) {
			return name, j
		}
		name += "::" + toks[part].Text
		j = part + 1
	}
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
