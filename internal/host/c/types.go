package c

import "enx/internal/host"

// Types reads the type that a C declaration writes for its declarators.
//
// C is the only host where typed anaphora is exact rather than conventional: the
// type is spelled out in the source, so reading it is parsing, not guessing.
//
// The reader is deliberately conservative. A declaration whose type it cannot
// determine — an unknown typedef reached through a pointer chain, say — reports
// ok=false, so the binding has no type and a typed anaphor cannot reach it.
type Types struct{}

// NewTypes returns the C type reader.
func NewTypes() *Types { return &Types{} }

var typeSpecifiers = map[string]bool{
	"void": true, "char": true, "short": true, "int": true, "long": true,
	"float": true, "double": true, "signed": true, "unsigned": true,
	"_Bool": true, "_Complex": true,
}

var declarationPrefixes = map[string]bool{
	"typedef": true, "extern": true, "static": true, "auto": true,
	"register": true, "_Thread_local": true, "const": true, "volatile": true,
	"restrict": true, "_Atomic": true, "inline": true, "_Noreturn": true,
}

// AssignedType implements host.TypeReader.
//
// It reads the declaration's specifiers from the start of the statement, then
// decides whether this particular declarator is a pointer. The split matters for
// "char *a, b;": both declarators share the declared type, but only the first is
// a pointer.
func (t *Types) AssignedType(toks []host.Token, nameIndex, eqIndex int) (string, bool, bool) {
	start := statementStart(toks, nameIndex)

	base := ""
	typedefName := ""

	for j := start; j < nameIndex; j++ {
		tok := toks[j]
		if tok.IsTrivia() {
			continue
		}
		switch {
		case tok.Text == "struct" || tok.Text == "union" || tok.Text == "enum":
			// The tag is the next identifier and is the type name.
			if k := nextIdentifier(toks, j+1); k >= 0 && k < nameIndex {
				base = toks[k].Text
			}
			continue
		case tok.Text == "*":
			// A '*' here belongs to the declarator, not to the declaration as
			// a whole. Pointer-ness is decided below, per declarator, so that
			// "char *a, b;" does not make b a pointer.
			continue
		case tok.Kind == host.Keyword && typeSpecifiers[tok.Text]:
			base = tok.Text
			continue
		case tok.Kind == host.Keyword && declarationPrefixes[tok.Text]:
			continue
		case tok.Kind == host.Identifier:
			// A typedef name stands as the type when no builtin specifier has
			// been seen. Anything after that is the declarator, which the
			// pointer decision below handles separately.
			if base == "" {
				typedefName = tok.Text
			}
			continue
		}
	}

	if typedefName != "" && base == "" {
		base = typedefName
	}
	if base == "" {
		return "", false, false
	}

	// Pointer-ness belongs to the declarator, not the declaration: "char *a, b;"
	// declares a pointer and a plain char. Count '*' between the last separator
	// and this name, and only there.
	pointer := false
	for j := nameIndex - 1; j >= start; j-- {
		tok := toks[j]
		if tok.IsTrivia() {
			continue
		}
		if tok.Text == "," {
			break
		}
		if tok.Text == "*" {
			pointer = true
		}
	}
	return base, pointer, true
}

// statementStart returns the index just after the previous statement boundary.
func statementStart(toks []host.Token, from int) int {
	for j := from - 1; j >= 0; j-- {
		tok := toks[j]
		if tok.IsTrivia() {
			continue
		}
		if tok.Text == ";" || tok.Text == "{" || tok.Text == "}" || tok.Kind == host.Preproc {
			return j + 1
		}
	}
	return 0
}

// nextIdentifier returns the index of the next Identifier at or after i, or -1.
func nextIdentifier(toks []host.Token, i int) int {
	for j := i; j < len(toks); j++ {
		if toks[j].IsTrivia() {
			continue
		}
		if toks[j].Kind == host.Identifier {
			return j
		}
		return -1
	}
	return -1
}
