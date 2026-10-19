// Package c provides the C host: a tokenizer and the grammar machine that
// constrains code-switch labelling.
package c

import (
	"bytes"
	"unicode"
	"unicode/utf8"

	"enx/internal/core"
	"enx/internal/host"
)

// Lexer tokenizes C source.
//
// Lexing is total: every byte of the input belongs to exactly one token, so
// concatenating the token texts reproduces the input exactly. A preprocessor
// directive is a single token covering its whole logical line, because a macro
// body is a different language and the grammar has no business inside it.
type Lexer struct{}

// New returns a new C lexer.
func New() *Lexer { return &Lexer{} }

// Language returns "c".
func (l *Lexer) Language() string { return "c" }

// LexWithAnaphora tokenizes src with marker claimed as an anaphor marker.
//
// A zero marker means no marker is claimed, so Lex and LexWithAnaphora(0) are
// the same thing.
func (l *Lexer) LexWithAnaphora(src *core.Source, marker byte) ([]host.Token, []core.Diagnostic) {
	s := &scanner{src: src, b: src.Bytes(), atLineStart: true, anaphor: marker}
	s.run()
	return s.toks, s.diags
}

// Lex tokenizes src.
func (l *Lexer) Lex(src *core.Source) ([]host.Token, []core.Diagnostic) {
	s := &scanner{src: src, b: src.Bytes(), atLineStart: true}
	s.run()
	return s.toks, s.diags
}

// keywords is the C keyword set.
//
// Many of these are ordinary English words — if, else, for, while, do, switch,
// break, continue, return, case, default, static, const, long, short, signed,
// unsigned, float, void, auto, register, extern, union, enum — which is why C
// is the hardest host of the three for this technique. The decoder must not
// treat a keyword as code without evidence.
//
// bool, true, false and nullptr are deliberately absent: they are English words
// and are macros from <stdbool.h> in the code most people write.
var keywords = map[string]struct{}{
	"auto": {}, "break": {}, "case": {}, "char": {}, "const": {},
	"continue": {}, "default": {}, "do": {}, "double": {}, "else": {},
	"enum": {}, "extern": {}, "float": {}, "for": {}, "goto": {},
	"if": {}, "inline": {}, "int": {}, "long": {}, "register": {},
	"restrict": {}, "return": {}, "short": {}, "signed": {}, "sizeof": {},
	"static": {}, "struct": {}, "switch": {}, "typedef": {}, "union": {},
	"unsigned": {}, "void": {}, "volatile": {}, "while": {},
	"_Alignas": {}, "_Alignof": {}, "_Atomic": {}, "_Bool": {}, "_Complex": {},
	"_Generic": {}, "_Noreturn": {}, "_Static_assert": {}, "_Thread_local": {},
}

// operators is ordered longest first so a greedy match prefers "<<=" to "<<".
var operators = []string{
	"<<=", ">>=", "...", "->", "++", "--", "<<", ">>", "<=", ">=",
	"==", "!=", "&&", "||", "+=", "-=", "*=", "/=", "%=", "&=", "^=", "|=",
	"+", "-", "*", "/", "%", "<", ">", "=", "!", "&", "|", "^", "~",
	"?", ":", ".", ",", ";", "(", ")", "[", "]", "{", "}",
	"#",
}

// punctuation is the subset of operator-like tokens classified as Punct.
var punctuation = map[string]struct{}{
	",": {}, ";": {}, "(": {}, ")": {}, "[": {}, "]": {}, "{": {}, "}": {},
}

type scanner struct {
	src   *core.Source
	b     []byte
	i     int
	toks  []host.Token
	diags []core.Diagnostic

	lastSig  host.Kind
	lastText string

	// atLineStart marks a position where a preprocessor directive may begin.
	// A directive is only a directive when it is the first thing on its line.
	atLineStart bool

	// anaphor is the claimed anaphor marker, or 0 when anaphora is off.
	anaphor byte
}

func (s *scanner) run() {
	for s.i < len(s.b) {
		c := s.b[s.i]
		switch {
		case c == '\\' && s.lineSplice():
			// consumed as whitespace
		case s.anaphorAt():
			// An anaphor is claimed before anything else, because the marker
			// may also be a sigil or an operator in this language.
		case c == ' ' || c == '\t' || c == '\r' || c == '\v' || c == '\f':
			s.space()
		case c == '\n':
			s.newline()
		case c == '/' && s.i+1 < len(s.b) && (s.b[s.i+1] == '/' || s.b[s.i+1] == '*'):
			s.comment()
		case c == '#' && s.atLineStart:
			s.preproc()
		case c >= '0' && c <= '9':
			s.number()
		case c == '.' && s.i+1 < len(s.b) && s.b[s.i+1] >= '0' && s.b[s.i+1] <= '9':
			s.number()
		case c == '"':
			s.quoted(c)
		case c == '\'':
			s.charLiteral()
		case c == '~' && s.escape():
			// consumed
		case isIdentStart(c):
			s.word()
		case c >= utf8.RuneSelf:
			s.unicodeWord()
		default:
			s.operator()
		}
	}
}

// lineSplice consumes a backslash-newline pair, which joins two physical lines
// into one logical line everywhere in C, not only in macros.
func (s *scanner) lineSplice() bool {
	j := s.i + 1
	if j < len(s.b) && s.b[j] == '\r' {
		j++
	}
	if j >= len(s.b) || s.b[j] != '\n' {
		return false
	}
	start := s.i
	s.i = j + 1
	s.emit(host.Space, start, s.i)
	return true
}

func (s *scanner) emit(k host.Kind, start, end int) {
	if end <= start {
		return
	}
	tok := host.Token{Kind: k, Text: string(s.b[start:end]), Span: s.src.Span(start, end)}
	s.toks = append(s.toks, tok)

	switch {
	case k == host.Newline:
		s.lastSig = host.Newline
		s.lastText = "\n"
		s.atLineStart = true
	case k == host.Space:
		// Whitespace does not change atLineStart, so a directive may still
		// follow indentation.
	case k == host.Comment:
		// A comment does not change atLineStart either.
	default:
		s.lastSig = k
		s.lastText = tok.Text
		s.atLineStart = false
	}
}

func (s *scanner) space() {
	start := s.i
	for s.i < len(s.b) {
		switch s.b[s.i] {
		case ' ', '\t', '\r', '\v', '\f':
			s.i++
		default:
			s.emit(host.Space, start, s.i)
			return
		}
	}
	s.emit(host.Space, start, s.i)
}

func (s *scanner) newline() {
	start := s.i
	s.i++
	s.emit(host.Newline, start, s.i)
}

// comment consumes a line comment or a block comment.
func (s *scanner) comment() {
	start := s.i
	if s.b[s.i+1] == '/' {
		for s.i < len(s.b) && s.b[s.i] != '\n' {
			s.i++
		}
		s.emit(host.Comment, start, s.i)
		return
	}
	s.i += 2
	for s.i < len(s.b) {
		if s.b[s.i] == '*' && s.i+1 < len(s.b) && s.b[s.i+1] == '/' {
			s.i += 2
			break
		}
		s.i++
	}
	s.emit(host.Comment, start, s.i)
}

// preproc consumes a whole preprocessor directive as one token, including any
// line continuations. The text of a macro is not C, so nothing inside it is
// parsed.
func (s *scanner) preproc() {
	start := s.i
	for s.i < len(s.b) {
		c := s.b[s.i]
		if c == '\\' {
			j := s.i + 1
			if j < len(s.b) && s.b[j] == '\r' {
				j++
			}
			if j < len(s.b) && s.b[j] == '\n' {
				s.i = j + 1
				continue
			}
			s.i++
			continue
		}
		if c == '\n' {
			break
		}
		s.i++
	}
	s.emit(host.Preproc, start, s.i)
}

func (s *scanner) number() {
	start := s.i
	kind := host.Int

	if s.b[s.i] == '0' && s.i+1 < len(s.b) {
		switch s.b[s.i+1] {
		case 'x', 'X':
			s.i += 2
			s.digits(16)
			s.intSuffix()
			s.emit(kind, start, s.i)
			return
		case 'b', 'B':
			s.i += 2
			s.digits(2)
			s.intSuffix()
			s.emit(kind, start, s.i)
			return
		}
	}

	s.digits(10)
	if s.i+1 < len(s.b) && s.b[s.i] == '.' && s.b[s.i+1] >= '0' && s.b[s.i+1] <= '9' {
		kind = host.Float
		s.i++
		s.digits(10)
	} else if s.i < len(s.b) && s.b[s.i] == '.' && s.i+1 < len(s.b) &&
		(s.b[s.i+1] == 'e' || s.b[s.i+1] == 'E' || isAlpha(s.b[s.i+1])) {
		kind = host.Float
		s.i++
		s.digits(10)
	}
	if s.i < len(s.b) && (s.b[s.i] == 'e' || s.b[s.i] == 'E') {
		k := s.i + 1
		if k < len(s.b) && (s.b[k] == '+' || s.b[k] == '-') {
			k++
		}
		if k < len(s.b) && s.b[k] >= '0' && s.b[k] <= '9' {
			s.i = k
			s.digits(10)
			kind = host.Float
		}
	}
	// A float suffix is f, F, l or L; an integer suffix is u/U/l/L in any case,
	// possibly doubled.
	if kind == host.Float {
		if s.i < len(s.b) && (s.b[s.i] == 'f' || s.b[s.i] == 'F' || s.b[s.i] == 'l' || s.b[s.i] == 'L') {
			s.i++
		}
	} else {
		s.intSuffix()
	}
	s.emit(kind, start, s.i)
}

func (s *scanner) intSuffix() {
	for s.i < len(s.b) {
		switch s.b[s.i] {
		case 'u', 'U', 'l', 'L':
			s.i++
		default:
			return
		}
	}
}

func (s *scanner) digits(base int) {
	for s.i < len(s.b) {
		c := s.b[s.i]
		var ok bool
		switch base {
		case 2:
			ok = c == '0' || c == '1'
		case 8:
			ok = c >= '0' && c <= '7'
		case 16:
			ok = (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		default:
			ok = c >= '0' && c <= '9'
		}
		if !ok {
			return
		}
		s.i++
	}
}

// quoted consumes a string literal, including any escaped quote.
func (s *scanner) quoted(q byte) {
	start := s.i
	s.i++
	for s.i < len(s.b) {
		c := s.b[s.i]
		if c == '\\' {
			if s.i+1 < len(s.b) {
				s.i += 2
			} else {
				s.i++
			}
			continue
		}
		if c == q {
			s.i++
			break
		}
		if c == '\n' {
			break
		}
		s.i++
	}
	s.emit(host.String, start, s.i)
}

// charLiteral consumes a character constant, which is quote-delimited like a
// string but only ever short.
func (s *scanner) charLiteral() {
	start := s.i
	s.i++
	for s.i < len(s.b) {
		c := s.b[s.i]
		if c == '\\' {
			if s.i+1 < len(s.b) {
				s.i += 2
			} else {
				s.i++
			}
			continue
		}
		if c == '\'' {
			s.i++
			break
		}
		if c == '\n' {
			break
		}
		s.i++
	}
	s.emit(host.Char, start, s.i)
}

func (s *scanner) word() {
	start := s.i
	for s.i < len(s.b) && isIdentPart(s.b[s.i]) {
		s.i++
	}
	text := string(s.b[start:s.i])
	kind := host.Identifier
	if _, ok := keywords[text]; ok {
		kind = host.Keyword
	}
	// Capitalisation is not syntax in C: RED is an identifier, not a constant.
	// The Ruby lexer distinguishes them because Ruby's grammar does; C's does
	// not, and treating RED as a different kind broke every enum.
	s.emit(kind, start, s.i)
}

func (s *scanner) unicodeWord() {
	start := s.i
	first := true
	for s.i < len(s.b) {
		r, size := utf8.DecodeRune(s.b[s.i:])
		if r == utf8.RuneError && size == 1 {
			break
		}
		if !(isLetterRune(r) || (r >= '0' && r <= '9') || r == '_') {
			break
		}
		if first && !isLetterRune(r) {
			break
		}
		first = false
		s.i += size
	}
	if s.i == start {
		_, size := utf8.DecodeRune(s.b[s.i:])
		if size <= 0 {
			size = 1
		}
		s.i += size
		s.emit(host.Punct, start, s.i)
		return
	}
	s.emit(host.Identifier, start, s.i)
}

func (s *scanner) operator() {
	start := s.i
	for _, op := range operators {
		if bytes.HasPrefix(s.b[s.i:], []byte(op)) {
			s.i += len(op)
			kind := host.Operator
			if _, ok := punctuation[op]; ok {
				kind = host.Punct
			}
			s.emit(kind, start, s.i)
			return
		}
	}
	s.i++
	s.emit(host.Punct, start, s.i)
}

func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentPart(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

func isUpper(b byte) bool { return b >= 'A' && b <= 'Z' }

func isAlpha(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

func isLetterRune(r rune) bool {
	return unicode.IsLetter(r)
}

// escape consumes a ~ ... ~ span that the author marked as prose.
//
// The rule for recognising one is shared by all three lexers; see
// host.MatchEscape.
func (s *scanner) escape() bool {
	end, ok := host.MatchEscape(s.b, s.i)
	if !ok {
		return false
	}
	s.emit(host.Prose, s.i, end)
	s.i = end
	return true
}

// anaphorAt consumes a claimed anaphor marker followed by digits, if one begins
// here. The rule is shared by all three lexers; see host.MatchAnaphor.
func (s *scanner) anaphorAt() bool {
	end, ok := host.MatchAnaphor(s.b, s.i, s.anaphor)
	if !ok {
		return false
	}
	s.emit(host.Anaphor, s.i, end)
	s.i = end
	return true
}
