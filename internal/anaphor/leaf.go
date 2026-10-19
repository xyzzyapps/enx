package anaphor

import "enx/internal/host"

// This file is the leaf layer: these functions read model fields and their
// arguments only. Resolve is the state machine that calls them.

// AssignmentOp reports whether text assigns to the preceding name.
func AssignmentOp(text string) bool {
	switch text {
	case "=", "+=", "-=", "*=", "/=", "%=", "**=", ".=", "||=", "&&=",
		"//=", "|=", "&=", "^=", "<<=", ">>=", "x=":
		return true
	}
	return false
}

// typeMatches reports whether a binding satisfies the type an anaphor asked for.
//
// An empty want matches anything. A trailing "*" requires a pointer, which is
// how "!-1:char*" distinguishes pointer-to-char from plain char now that "*" is
// no longer the marker. Without the star the base name matches either, because
// "char" is the type in both "char c" and "char *s".
func typeMatches(want string, b Binding) bool {
	if want == "" {
		return true
	}
	pointer := false
	if len(want) > 1 && want[len(want)-1] == '*' {
		pointer = true
		want = want[:len(want)-1]
	}
	if b.Type == "" {
		return false
	}
	if b.Type != want {
		return false
	}
	if pointer && !b.Pointer {
		return false
	}
	return true
}

// isNameToken reports whether tok can be the target of an assignment.
func isNameToken(tok host.Token) bool {
	switch tok.Kind {
	case host.Identifier, host.Constant, host.Variable:
		return true
	}
	return false
}
