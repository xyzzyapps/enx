// Package host defines the contract that every host language (Ruby, Perl, C)
// implements so that the code-switching pipeline can be language-agnostic.
//
// The pipeline never rewrites code. It only labels tokens as code, English, or
// trivia, and deletes the English. The host language therefore contributes
// exactly two things: a tokenizer, and a grammar machine that acts as a hard
// constraint on which token sequences can be code.
package host

import "enx/internal/core"

// Kind classifies a token lexically. It says nothing about whether the token
// is code or English; that decision belongs to the decoder.
type Kind uint8

const (
	// Unknown is a token the lexer could not classify.
	Unknown Kind = iota
	// Space is a run of spaces or tabs.
	Space
	// Newline is a line terminator.
	Newline
	// Comment is a line or block comment.
	Comment
	// Identifier is a lowercase-initial name, for example foo_bar.
	Identifier
	// Constant is an uppercase-initial name, for example FooBar.
	Constant
	// Keyword is a language reserved word.
	Keyword
	// Operator is an operator symbol such as + or &&.
	Operator
	// Punct is a separating or bracketing symbol such as , or (.
	Punct
	// Int is an integer literal.
	Int
	// Float is a floating point literal.
	Float
	// String is a string literal, including its quotes.
	String
	// Symbol is a symbol or interned name literal.
	Symbol
	// Regex is a regular expression literal.
	Regex
	// Heredoc is a heredoc literal, including its body.
	Heredoc
	// Variable is a sigil-prefixed variable, as in Perl or Ruby.
	Variable
	// Sigil is a bare sigil without a name.
	Sigil
	// Preproc is a preprocessor directive, as in C.
	Preproc
	// Char is a character literal.
	Char
	// Label is a label or jump target.
	Label
	// Prose is a span the author marked as prose with a ~ ... ~ escape. It is
	// never code and is deleted with the rest of the English.
	Prose
	// Anaphor is a claimed anaphor marker followed by an ordinal or a type,
	// such as !-1 or !-1:Point. It is always code, and is resolved to the name
	// of an earlier assignment when anaphora is enabled. See SPEC section 18.
	Anaphor
)

var kindNames = [...]string{
	Unknown:    "unknown",
	Space:      "space",
	Newline:    "newline",
	Comment:    "comment",
	Identifier: "identifier",
	Constant:   "constant",
	Keyword:    "keyword",
	Operator:   "operator",
	Punct:      "punct",
	Int:        "int",
	Float:      "float",
	String:     "string",
	Symbol:     "symbol",
	Regex:      "regex",
	Heredoc:    "heredoc",
	Variable:   "variable",
	Sigil:      "sigil",
	Preproc:    "preproc",
	Char:       "char",
	Label:      "label",
	Prose:      "prose",
	Anaphor:    "anaphor",
}

// Kind classification is in leaf.go.

// Token is one lexical unit together with its exact byte span.
type Token struct {
	Kind Kind
	Text string
	Span core.Span
}

// IsTrivia reports whether the token is spacing or a comment.
func (t Token) IsTrivia() bool { return t.Kind.IsTrivia() }

// String renders the token for logs and test failures.
func (t Token) String() string {
	return t.Kind.String() + "(" + t.Text + ")"
}

// Lexer turns source bytes into tokens.
//
// A Lexer must be total: every byte of the input belongs to exactly one token,
// so that concatenating token texts reproduces the input exactly. This is what
// lets the emitter preserve untouched regions byte for byte.
type Lexer interface {
	// Language is the lower-case language name, for example "ruby".
	Language() string
	// Lex tokenizes src. Diagnostics describe lexical problems only; a Lexer
	// must still return a best-effort token stream.
	Lex(src *core.Source) ([]Token, []core.Diagnostic)
}

// AnaphorLexer is a Lexer that can also claim an anaphor marker.
//
// It is a separate interface so that claiming the marker stays opt-in: a caller
// that does not ask for anaphora uses Lex and sees the marker as the ordinary
// operator it already is. See SPEC section 18.2 for why the marker must be
// claimed rather than assumed.
type AnaphorLexer interface {
	Lexer
	// LexWithAnaphora tokenizes src, treating marker followed by digits as an
	// Anaphor token. A zero marker means no marker is claimed.
	LexWithAnaphora(src *core.Source, marker byte) ([]Token, []core.Diagnostic)
}
