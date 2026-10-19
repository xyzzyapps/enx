package host

// Spec is everything the pipeline needs to know about a host language. It is
// the unit that a language registers with the registry.
type Spec struct {
	// Language is the lower-case name used on the command line, for example
	// "ruby".
	Language string
	// Extension is the file extension of emitted files, including the dot.
	Extension string
	// CommentPrefix is the line comment introducer, such as "#" or "//". It is
	// used by --keep-prose, which turns dropped English back into comments.
	CommentPrefix string
	// Interpreter is the command used to execute an emitted file, for example
	// []string{"ruby"}. It is empty when the language is compiled.
	Interpreter []string
	// Compile is a command template for building a compiled language, with
	// {src} and {out} substituted. Example:
	// []string{"gcc", "-std=c11", "-o", "{out}", "{src}"}. When set, the
	// emitted file is compiled and the resulting executable is run.
	Compile []string
	// Vocabulary is the set of well known names in the host language. It may be
	// nil, but without it the decoder cannot tell a two word call such as
	// "puts answer" from two English words.
	Vocabulary Vocabulary
	// Scopes tells the anaphor resolver where blocks begin and end. A nil
	// Scopes means the whole file is one scope, which is a valid and simple
	// choice.
	Scopes ScopeTracker
	// Types reads the type written for an assignment. A nil Types means typed
	// anaphors never resolve, which is the safe default.
	Types TypeReader
	// Lexer tokenizes the input.
	Lexer Lexer
	// Machine is the grammar constraint. It is nil while a language is still
	// being brought up.
	Machine Machine
}

// Ready reports whether the language has a grammar machine and can therefore
// emit a program, as opposed to only being inspectable.
func (s Spec) Ready() bool { return s.Machine != nil }

// ScopeTracker tells the anaphor resolver where a block begins and ends.
//
// It lives here rather than in the anaphor package so that a host language can
// implement it without depending on the resolver, and the resolver can depend on
// the token stream without depending on any language.
type ScopeTracker interface {
	// Opens reports whether tok begins a new scope.
	Opens(tok Token) bool
	// Closes reports whether tok ends the innermost scope.
	Closes(tok Token) bool
}

// TypeReader reads the type that a host language writes for an assignment.
//
// It is a reader, not an inference engine, and that distinction is the whole
// design. C writes the type in the declaration; Ruby and Perl write it in the
// right hand side, as a literal's class or as the constant in a constructor
// call. Where the type is not written, ok is false and the binding simply has no
// type, so a typed anaphor cannot reach it. Guessing instead would silently pick
// the wrong referent, which is the failure this project refuses.
type TypeReader interface {
	// AssignedType reports the type written for the assignment whose target is
	// at nameIndex and whose operator is at eqIndex.
	//
	// pointer reports that the referent is a pointer, which only C distinguishes.
	// ok is false when the type is not written in the source.
	AssignedType(toks []Token, nameIndex, eqIndex int) (typeName string, pointer bool, ok bool)
}
