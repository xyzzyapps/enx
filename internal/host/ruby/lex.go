// Package ruby provides the Ruby host: a tokenizer and, later, the grammar
// machine that constrains code-switch labelling.
package ruby

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"

	"enx/internal/core"
	"enx/internal/host"
)

// Lexer tokenizes Ruby source.
//
// Lexing is total: every byte of the input belongs to exactly one token, so
// concatenating the token texts reproduces the input exactly. The emitter
// relies on that to leave untouched regions byte-identical.
type Lexer struct{}

// New returns a new Ruby lexer.
func New() *Lexer { return &Lexer{} }

// Language returns "ruby".
func (l *Lexer) Language() string { return "ruby" }

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

// keywords is the Ruby reserved-word set. Words such as "if", "for", "while",
// "do" and "return" are also ordinary English, which is precisely why the
// decoder must not treat a keyword as code without evidence.
var keywords = map[string]struct{}{
	"alias": {}, "and": {}, "begin": {}, "BEGIN": {}, "break": {},
	"case": {}, "class": {}, "def": {}, "defined?": {}, "do": {},
	"else": {}, "elsif": {}, "end": {}, "END": {}, "ensure": {},
	"false": {}, "for": {}, "if": {}, "in": {}, "module": {},
	"next": {}, "nil": {}, "not": {}, "or": {}, "redo": {},
	"rescue": {}, "retry": {}, "return": {}, "self": {}, "super": {},
	"then": {}, "true": {}, "undef": {}, "unless": {}, "until": {},
	"when": {}, "while": {}, "yield": {},
}

// operators is ordered longest first so that a greedy match finds "<<=" before
// "<<" and "<<" before "<".
var operators = []string{
	"<=>", "===", "...", "**=", "<<=", ">>=", "&&=", "||=", "&.",
	"=~", "!~", "**", "==", "!=", "<=", ">=", "<<", ">>", "&&", "||",
	"..", "::", "->", "=>", "+=", "-=", "*=", "/=", "%=", "|=", "&=", "^=",
	"+", "-", "*", "/", "%", "<", ">", "=", "!", "&", "|", "^", "~",
	"?", ".", ",", ";", "(", ")", "[", "]", "{", "}",
}

// punctuation is the subset of operator-like tokens classified as Punct.
var punctuation = map[string]struct{}{
	",": {}, ";": {}, "(": {}, ")": {}, "[": {}, "]": {}, "{": {}, "}": {},
}

type heredoc struct {
	term     string
	indented bool
}

type scanner struct {
	src   *core.Source
	b     []byte
	i     int
	toks  []host.Token
	diags []core.Diagnostic

	// pending holds heredocs opened on the current line, whose bodies begin
	// after the next newline.
	pending []heredoc

	// lastSig and lastText describe the last non-trivia token, used to decide
	// whether a slash starts a regex or is division.
	lastSig  host.Kind
	lastText string

	atLineStart bool

	// anaphor is the claimed anaphor marker, or 0 when anaphora is off.
	anaphor byte
}

func (s *scanner) run() {
	for s.i < len(s.b) {
		c := s.b[s.i]
		switch {
		case s.anaphorAt():
			// An anaphor is claimed before anything else, because the marker
			// may also be a sigil or an operator in this language.
		case c == ' ' || c == '\t' || c == '\r' || c == '\v' || c == '\f':
			s.space()
		case c == '\n':
			s.newline()
		case c == '#':
			s.lineComment()
		case c == '=' && s.atLineStart && bytes.HasPrefix(s.b[s.i:], []byte("=begin")):
			s.blockComment()
		case c >= '0' && c <= '9':
			s.number()
		case c == '"' || c == '\'' || c == '`':
			s.quoted(c)
		case c == '%':
			if !s.percentLiteral() {
				s.operator()
			}
		case c == ':':
			s.colon()
		case c == '/':
			if s.regexAllowed() {
				s.regex()
			} else {
				s.operator()
			}
		case c == '@' || c == '$':
			s.sigilVariable()
		case c == '~' && s.escape():
			// consumed
		case isIdentStart(c):
			s.word()
		case c == '<' && s.heredocMarker():
			// consumed by heredocMarker
		case c >= utf8.RuneSelf:
			s.unicodeWord()
		default:
			s.operator()
		}
	}
	// A heredoc opened on the final line without a trailing newline still
	// needs its body consumed.
	s.consumeHeredocBodies()
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
	case k == host.Space || k == host.Comment:
		// Trivia does not change the previous significant token.
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
	s.consumeHeredocBodies()
}

func (s *scanner) lineComment() {
	start := s.i
	for s.i < len(s.b) && s.b[s.i] != '\n' {
		s.i++
	}
	s.emit(host.Comment, start, s.i)
}

func (s *scanner) blockComment() {
	start := s.i
	for s.i < len(s.b) {
		lineStart := s.i
		k := s.i
		for k < len(s.b) && s.b[k] != '\n' {
			k++
		}
		line := string(s.b[lineStart:k])
		if k < len(s.b) {
			k++
		}
		s.i = k
		if strings.HasPrefix(line, "=end") {
			break
		}
	}
	s.emit(host.Comment, start, s.i)
}

func (s *scanner) number() {
	start := s.i
	kind := host.Int

	if s.b[s.i] == '0' && s.i+1 < len(s.b) {
		switch s.b[s.i+1] {
		case 'x', 'X':
			s.i += 2
			s.digits(16)
			s.emit(kind, start, s.i)
			return
		case 'b', 'B':
			s.i += 2
			s.digits(2)
			s.emit(kind, start, s.i)
			return
		case 'o', 'O':
			s.i += 2
			s.digits(8)
			s.emit(kind, start, s.i)
			return
		}
	}

	s.digits(10)
	if s.i+1 < len(s.b) && s.b[s.i] == '.' && s.b[s.i+1] >= '0' && s.b[s.i+1] <= '9' {
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
	if s.i < len(s.b) && (s.b[s.i] == 'r' || s.b[s.i] == 'i') {
		s.i++
	}
	s.emit(kind, start, s.i)
}

func (s *scanner) digits(base int) {
	for s.i < len(s.b) {
		c := s.b[s.i]
		if c == '_' {
			s.i++
			continue
		}
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
		s.i++
	}
	s.emit(host.String, start, s.i)
}

func (s *scanner) percentLiteral() bool {
	start := s.i
	j := s.i + 1
	if j >= len(s.b) {
		return false
	}

	var typ byte
	if isAlpha(s.b[j]) {
		switch s.b[j] {
		case 'w', 'W', 'i', 'I', 'q', 'Q', 'r', 's':
			typ = s.b[j]
			j++
		default:
			return false
		}
	}
	if j >= len(s.b) {
		return false
	}

	open := s.b[j]
	var close byte
	switch open {
	case '(':
		close = ')'
	case '[':
		close = ']'
	case '{':
		close = '}'
	case '<':
		close = '>'
	case '|', '/', '!', '#', '~', '^', '`', '%', '@', '$', ',', '.',
		':', ';', '?', '*', '+', '-', '_', '=', '\\', '\'', '"':
		close = open
	default:
		return false
	}

	paired := open != close
	depth := 1
	k := j + 1
	for k < len(s.b) {
		c := s.b[k]
		if c == '\\' {
			if k+1 < len(s.b) {
				k += 2
			} else {
				k++
			}
			continue
		}
		if paired && c == open {
			depth++
			k++
			continue
		}
		if c == close {
			depth--
			k++
			if depth == 0 {
				break
			}
			continue
		}
		k++
	}
	s.i = k

	kind := host.String
	switch typ {
	case 'r':
		kind = host.Regex
	case 's':
		kind = host.Symbol
	}
	s.emit(kind, start, s.i)
	return true
}

func (s *scanner) colon() {
	start := s.i
	if s.i+1 < len(s.b) && s.b[s.i+1] == ':' {
		s.i += 2
		s.emit(host.Operator, start, s.i)
		return
	}

	j := s.i + 1
	if j < len(s.b) {
		if q := s.b[j]; q == '"' || q == '\'' {
			k := j + 1
			for k < len(s.b) {
				if s.b[k] == '\\' {
					k += 2
					continue
				}
				if s.b[k] == q {
					k++
					break
				}
				k++
			}
			if k > len(s.b) {
				k = len(s.b)
			}
			s.i = k
			s.emit(host.Symbol, start, s.i)
			return
		}
		if isIdentStart(s.b[j]) {
			k := j
			for k < len(s.b) && isIdentPart(s.b[k]) {
				k++
			}
			if k < len(s.b) && (s.b[k] == '?' || s.b[k] == '!' || s.b[k] == '=') {
				k++
			}
			s.i = k
			s.emit(host.Symbol, start, s.i)
			return
		}
		if strings.IndexByte("+-*/%<>=!~^&|[]", s.b[j]) >= 0 {
			s.i = j + 1
			s.emit(host.Symbol, start, s.i)
			return
		}
	}

	s.i++
	s.emit(host.Punct, start, s.i)
}

func (s *scanner) regexAllowed() bool {
	switch s.lastSig {
	case host.Unknown, host.Newline, host.Operator, host.Punct:
		return true
	case host.Keyword:
		switch s.lastText {
		case "if", "unless", "while", "until", "and", "or", "not",
			"when", "then", "else", "elsif", "return", "yield", "in", "do":
			return true
		}
		return false
	default:
		return false
	}
}

func (s *scanner) regex() {
	start := s.i
	s.i++
	inClass := false
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
		if c == '[' {
			inClass = true
			s.i++
			continue
		}
		if c == ']' {
			inClass = false
			s.i++
			continue
		}
		if c == '/' && !inClass {
			s.i++
			break
		}
		if c == '\n' {
			break
		}
		s.i++
	}
	for s.i < len(s.b) && isAlpha(s.b[s.i]) {
		s.i++
	}
	s.emit(host.Regex, start, s.i)
}

func (s *scanner) sigilVariable() {
	start := s.i
	c := s.b[s.i]
	s.i++

	if c == '@' {
		if s.i < len(s.b) && s.b[s.i] == '@' {
			s.i++
		}
		for s.i < len(s.b) && isIdentPart(s.b[s.i]) {
			s.i++
		}
	} else {
		if s.i < len(s.b) && isIdentStart(s.b[s.i]) {
			for s.i < len(s.b) && isIdentPart(s.b[s.i]) {
				s.i++
			}
		} else if s.i < len(s.b) {
			s.i++
		}
	}

	kind := host.Variable
	if s.i == start+1 {
		kind = host.Sigil
	}
	s.emit(kind, start, s.i)
}

func (s *scanner) word() {
	start := s.i
	for s.i < len(s.b) && isIdentPart(s.b[s.i]) {
		s.i++
	}
	if s.i < len(s.b) && (s.b[s.i] == '?' || s.b[s.i] == '!') {
		s.i++
	}

	text := string(s.b[start:s.i])
	kind := host.Identifier
	if isUpper(s.b[start]) {
		kind = host.Constant
	}
	if _, ok := keywords[text]; ok {
		kind = host.Keyword
	}
	s.emit(kind, start, s.i)
}

// unicodeWord consumes an identifier that starts with a non-ASCII letter, so
// that prose containing accented words stays in one token.
func (s *scanner) unicodeWord() {
	start := s.i
	first := true
	for s.i < len(s.b) {
		r, size := utf8.DecodeRune(s.b[s.i:])
		if r == utf8.RuneError && size == 1 {
			break
		}
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
			break
		}
		if first && !unicode.IsLetter(r) {
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

func (s *scanner) heredocMarker() bool {
	start := s.i
	j := s.i
	if j+1 >= len(s.b) || s.b[j] != '<' || s.b[j+1] != '<' {
		return false
	}
	j += 2

	indented := false
	if j < len(s.b) && (s.b[j] == '~' || s.b[j] == '-') {
		indented = true
		j++
	}
	if j >= len(s.b) {
		return false
	}

	// Quoted terminator: <<~'SQL', <<"SQL", <<`SQL`
	if q := s.b[j]; q == '\'' || q == '"' || q == '`' {
		k := j + 1
		for k < len(s.b) && s.b[k] != q && s.b[k] != '\n' {
			k++
		}
		if k >= len(s.b) || s.b[k] != q {
			return false
		}
		term := string(s.b[j+1 : k])
		j = k + 1
		s.i = j
		s.emit(host.Heredoc, start, j)
		s.pending = append(s.pending, heredoc{term: term, indented: indented})
		return true
	}

	// Bare terminator must be adjacent to the <<, which distinguishes
	// <<~SQL from the shift operator in "a << b".
	if !isIdentStart(s.b[j]) {
		return false
	}
	k := j
	for k < len(s.b) && isIdentPart(s.b[k]) {
		k++
	}
	term := string(s.b[j:k])
	s.i = k
	s.emit(host.Heredoc, start, k)
	s.pending = append(s.pending, heredoc{term: term, indented: indented})
	return true
}

func (s *scanner) consumeHeredocBodies() {
	if len(s.pending) == 0 {
		return
	}
	pending := s.pending
	s.pending = nil

	for _, h := range pending {
		start := s.i
		for s.i < len(s.b) {
			lineStart := s.i
			k := s.i
			for k < len(s.b) && s.b[k] != '\n' {
				k++
			}
			line := string(s.b[lineStart:k])
			if k < len(s.b) {
				k++
			}
			s.i = k

			// Terminator matching must not depend on the line ending. A
			// non-indented heredoc still allows a CRLF file, where the line
			// text ends in "\r" before the newline was consumed.
			cmp := strings.TrimSuffix(line, "\r")
			if h.indented {
				cmp = strings.TrimSpace(line)
			}
			if cmp == h.term {
				break
			}
		}
		s.emit(host.Heredoc, start, s.i)
	}
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
	// Unknown byte: keep it, so lexing stays total.
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
