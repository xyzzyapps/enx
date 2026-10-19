package host

// This file is the leaf layer: token-kind classification and byte predicates.
// The lexers, matchers and parser are integrators.

// String returns the lower-case name of the kind.
func (k Kind) String() string {
	if int(k) < len(kindNames) && kindNames[k] != "" {
		return kindNames[k]
	}
	return "unknown"
}

// IsTrivia reports whether the token is spacing or a comment.
func (k Kind) IsTrivia() bool {
	return k == Space || k == Newline || k == Comment
}

// IsWord reports whether the token is a bare name that English prose could
// plausibly also be made of.
func (k Kind) IsWord() bool {
	return k == Identifier || k == Constant
}

// AlwaysCode reports whether the token can never be English.
func (k Kind) AlwaysCode() bool {
	switch k {
	case Operator, Punct, Int, Float, String, Symbol, Regex, Heredoc,
		Variable, Sigil, Preproc, Char, Label, Anaphor:
		return true
	default:
		return false
	}
}

func isIdentStartByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentPartByte(b byte) bool {
	return isIdentStartByte(b) || (b >= '0' && b <= '9')
}

func isSpaceByte(b byte) bool {
	switch b {
	case ' ', '\t', '\r', '\v', '\f', '\n':
		return true
	}
	return false
}
