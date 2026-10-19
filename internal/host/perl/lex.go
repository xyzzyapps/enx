// Package perl provides the Perl host: a tokenizer and the grammar machine
// that constrains code-switch labelling.
package perl

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"

	"enx/internal/core"
	"enx/internal/host"
)

// Lexer tokenizes Perl source.
//
// Lexing is total: every byte of the input belongs to exactly one token, so
// concatenating the token texts reproduces the input exactly.
type Lexer struct{}

// New returns a new Perl lexer.
func New() *Lexer { return &Lexer{} }

// Language returns "perl".
func (l *Lexer) Language() string { return "perl" }

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

// keywords is the Perl set of control-flow words. Functions such as print and
// split are deliberately absent: Perl parses them as list operators, and
// treating them as ordinary identifiers keeps the grammar simpler. Many of
// these words are also ordinary English, which is why the decoder must not
// treat a keyword as code without evidence.
var keywords = map[string]struct{}{
	"if": {}, "elsif": {}, "else": {}, "unless": {}, "while": {},
	"until": {}, "for": {}, "foreach": {}, "continue": {}, "do": {},
	"sub": {}, "my": {}, "our": {}, "local": {}, "state": {},
	"return": {}, "last": {}, "next": {}, "redo": {}, "goto": {},
	"die": {}, "warn": {}, "use": {}, "no": {}, "require": {},
	"package": {}, "BEGIN": {}, "END": {}, "CHECK": {}, "INIT": {},
	"eval": {}, "and": {}, "or": {}, "not": {}, "xor": {},
	"eq": {}, "ne": {}, "lt": {}, "gt": {}, "le": {}, "ge": {},
	"cmp": {}, "defined": {}, "undef": {}, "ref": {}, "scalar": {},
	"wantarray": {}, "given": {}, "when": {}, "default": {},
	"__END__": {}, "__DATA__": {}, "__FILE__": {}, "__LINE__": {},
	"__PACKAGE__": {}, "__SUB__": {},
}

// quoteOps are the quote-like operators that take a delimiter pair.
var quoteOps = map[string]bool{
	"q": true, "qq": true, "qw": true, "qr": true,
	"m": true, "s": true, "tr": true, "y": true,
}

// operators is ordered longest first so a greedy match prefers "<<=" to "<<".
var operators = []string{
	"<=>", "...", "**=", "||=", "&&=", "//=", "<<=", ">>=", "->*",
	"=~", "!~", "==", "!=", "<=", ">=", "=>", "->", "::", "++", "--",
	"**", "..", "&&", "||", "//", "<<", ">>", "+=", "-=", "*=", "/=",
	"%=", ".=", "x=", "|=", "&=", "^=", "!",
	"+", "-", "*", "/", "%", "<", ">", "=", "&", "|", "^", "~", "\\",
	"?", ":", ".", ",", ";", "(", ")", "[", "]", "{", "}",
}

// punctuation is the subset of operator-like tokens classified as Punct.
var punctuation = map[string]struct{}{
	",": {}, ";": {}, "(": {}, ")": {}, "[": {}, "]": {}, "{": {}, "}": {},
}

type heredoc struct {
	term string
}

type scanner struct {
	src   *core.Source
	b     []byte
	i     int
	toks  []host.Token
	diags []core.Diagnostic

	pending []heredoc

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
		case c == '=' && s.atLineStart && s.i+1 < len(s.b) && isIdentStart(s.b[s.i+1]):
			s.pod()
		case c >= '0' && c <= '9':
			s.number()
		case c == '.' && s.i+1 < len(s.b) && s.b[s.i+1] >= '0' && s.b[s.i+1] <= '9':
			s.number()
		case c == '\'':
			s.quoted(c)
		case c == '"':
			s.quoted(c)
		case c == '`':
			s.quoted(c)
		case c == '$' || c == '@' || c == '%' || c == '&' || c == '*':
			if !s.sigil() {
				s.operator()
			}
		case c == '<' && s.heredocMarker():
			// consumed
		case c == '/':
			if s.regexAllowed() {
				s.regex()
			} else {
				s.operator()
			}
		case c == '~' && s.escape():
			// consumed
		case isIdentStart(c):
			if !s.word() {
				s.operator()
			}
		case c == '-' && s.i+1 < len(s.b) && s.b[s.i+1] == '>':
			s.operator()
		case c >= utf8.RuneSelf:
			s.unicodeWord()
		default:
			s.operator()
		}
	}
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

// pod consumes a plain old documentation block, from "=word" to "=cut". POD is
// real Perl syntax rather than prose, and it is documentation, so it is trivia.
func (s *scanner) pod() {
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
		if strings.HasPrefix(line, "=cut") {
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

// sigil consumes a variable.
//
// A sigil is only a sigil when a name follows it. Without that rule the "*" in
// "$x * 2" would be read as a typeglob, and the multiply would vanish.
func (s *scanner) sigil() bool {
	start := s.i
	c := s.b[s.i]

	// ${name} and the block forms ${ $ref }.
	if s.i+1 < len(s.b) && s.b[s.i+1] == '{' {
		end := s.matchBraces(s.i + 1)
		if end < 0 {
			return false
		}
		s.i = end
		s.emit(host.Variable, start, s.i)
		return true
	}

	if c == '$' {
		return s.dollarVar(start)
	}

	// A dereference chain: @$ref, %$href, &$code, *$glob.
	if s.i+1 < len(s.b) && isSigilChar(s.b[s.i+1]) {
		s.i += 2
		for s.i < len(s.b) && isIdentPart(s.b[s.i]) {
			s.i++
		}
		s.emit(host.Variable, start, s.i)
		return true
	}

	if s.i+1 < len(s.b) && isIdentStart(s.b[s.i+1]) {
		s.i++
		for s.i < len(s.b) && isIdentPart(s.b[s.i]) {
			s.i++
		}
		s.emit(host.Variable, start, s.i)
		return true
	}

	// @- and @+ are real arrays, %- and %+ real hashes.
	if (c == '@' || c == '%') && s.i+1 < len(s.b) && (s.b[s.i+1] == '-' || s.b[s.i+1] == '+') {
		s.i += 2
		s.emit(host.Variable, start, s.i)
		return true
	}

	// Not a sigil after all: the caller treats the character as an operator.
	return false
}

// dollarVar consumes a scalar variable, including the punctuation globals such
// as $_ $! $@ $0 $/ $^W and $#array.
func (s *scanner) dollarVar(start int) bool {
	j := start + 1
	if j >= len(s.b) {
		return false
	}
	c := s.b[j]
	switch {
	case isSigilChar(c):
		// $$pid, $@error, $%format page and the dereferences $$ref, $${...}.
		j++
		for j < len(s.b) && isIdentPart(s.b[j]) {
			j++
		}
	case c == '#':
		j++
		for j < len(s.b) && isIdentPart(s.b[j]) {
			j++
		}
	case c == '^':
		j++
		if j < len(s.b) && isIdentPart(s.b[j]) {
			j++
		}
	case isIdentStart(c):
		for j < len(s.b) && isIdentPart(s.b[j]) {
			j++
		}
	case c >= '0' && c <= '9':
		for j < len(s.b) && s.b[j] >= '0' && s.b[j] <= '9' {
			j++
		}
	case strings.IndexByte("_!@/\"$;,=~%+<>.-:?\\|&'`*[]", c) >= 0:
		j++
	default:
		return false
	}
	s.i = j
	s.emit(host.Variable, start, s.i)
	return true
}

func isSigilChar(b byte) bool {
	return b == '$' || b == '@' || b == '%' || b == '&' || b == '*'
}

// matchBraces returns the offset just past the balanced brace group starting at
// open, or -1.
func (s *scanner) matchBraces(open int) int {
	depth := 0
	for i := open; i < len(s.b); i++ {
		switch s.b[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		case '\n':
			// A newline may still be inside a block form; keep going.
		}
	}
	return -1
}

// word consumes an identifier, a keyword, a label, or a quote-like operator.
//
// It returns false when the text is not a word at all, which lets the caller
// fall back to operator handling.
func (s *scanner) word() bool {
	start := s.i
	for s.i < len(s.b) && isIdentPart(s.b[s.i]) {
		s.i++
	}
	text := string(s.b[start:s.i])

	// __DATA__ and __END__ end the program; everything after them is data.
	// Treat the marker and the rest as trivia so the emitter preserves it and
	// the grammar never tries to parse the payload.
	if s.atLineStart && (text == "__DATA__" || text == "__END__") {
		s.i = len(s.b)
		s.emit(host.Comment, start, s.i)
		return true
	}

	// Quote-like operator: q, qq, qw, qr, m, s, tr, y followed by a delimiter.
	if _, ok := quoteOps[text]; ok && s.i < len(s.b) {
		if closed := s.quoteLike(start, text); closed {
			return true
		}
		// Not a quote-like operator after all: rewind and treat as a word.
		s.i = start + len(text)
	}

	kind := host.Identifier
	if isUpper(s.b[start]) {
		kind = host.Constant
	}
	if _, ok := keywords[text]; ok {
		kind = host.Keyword
	}
	if text == "x" && s.i == start+1 && s.lastSig != host.Identifier && s.lastSig != host.Variable {
		// The repetition operator, not an identifier.
		s.emit(host.Operator, start, s.i)
		return true
	}
	s.emit(kind, start, s.i)
	return true
}

// quoteLike consumes q//, qq//, qw//, qr//, m//, s///, tr/// and y///,
// including the second half of s, tr and y. The whole construct is one token.
func (s *scanner) quoteLike(start int, op string) bool {
	j := start + len(op)
	if j >= len(s.b) {
		return false
	}
	// Optional whitespace before the delimiter is allowed for the word-like
	// operators but not for q and qq.
	delim := s.b[j]
	if delim == ' ' || delim == '\t' {
		return false
	}
	if isIdentPart(delim) {
		return false
	}

	open, close := delim, delim
	paired := false
	switch delim {
	case '(':
		close, paired = ')', true
	case '[':
		close, paired = ']', true
	case '{':
		close, paired = '}', true
	case '<':
		close, paired = '>', true
	}

	end := s.scanDelimited(j, open, close)
	if end < 0 {
		return false
	}

	// s, tr and y take a second part. When the first delimiter was a bracket
	// pair the replacement has its own pair, which may use a different bracket;
	// otherwise the replacement simply runs to the next occurrence of the same
	// delimiter, as in s/foo/bar/g.
	switch op {
	case "s", "tr", "y":
		if paired {
			if end >= len(s.b) {
				return false
			}
			o2, c2 := s.b[end], s.b[end]
			switch o2 {
			case '(':
				c2 = ')'
			case '[':
				c2 = ']'
			case '{':
				c2 = '}'
			case '<':
				c2 = '>'
			}
			end2 := s.scanDelimited(end, o2, c2)
			if end2 < 0 {
				return false
			}
			end = end2
		} else {
			end2 := s.scanUntil(end, close)
			if end2 < 0 {
				return false
			}
			end = end2
		}
	}
	// Trailing modifiers.
	k := end
	for k < len(s.b) && isAlpha(s.b[k]) {
		k++
	}
	s.i = k
	kind := host.String
	switch op {
	case "m", "qr", "s", "tr", "y":
		// Substitutions are regex-shaped and are values, so they are lexed as
		// regex literals: the grammar then treats them like any other literal.
		kind = host.Regex
	}
	s.emit(kind, start, s.i)
	return true
}

// scanDelimited returns the offset just past a delimited group starting at the
// delimiter, or -1.
func (s *scanner) scanDelimited(at int, open, close byte) int {
	if at >= len(s.b) || s.b[at] != open {
		return -1
	}
	paired := open != close
	depth := 1
	k := at + 1
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
				return k
			}
			continue
		}
		k++
	}
	return -1
}

// scanUntil returns the offset just past the next unescaped delimiter at or
// after at, or -1.
func (s *scanner) scanUntil(at int, delim byte) int {
	for k := at; k < len(s.b); k++ {
		c := s.b[k]
		if c == '\\' {
			k++
			continue
		}
		if c == delim {
			return k + 1
		}
		if c == '\n' {
			return -1
		}
	}
	return -1
}

// regexPreceders are identifiers that take a pattern rather than a value, so a
// slash after them starts a regex rather than a division.
var regexPreceders = map[string]bool{
	"split": true, "grep": true, "map": true, "join": true,
	"print": true, "say": true, "push": true, "unshift": true,
}

func (s *scanner) regexAllowed() bool {
	switch s.lastSig {
	case host.Unknown, host.Newline, host.Operator, host.Punct:
		return true
	case host.Identifier, host.Constant:
		return regexPreceders[s.lastText]
	case host.Keyword:
		switch s.lastText {
		case "if", "unless", "while", "until", "and", "or", "not", "return",
			"split", "grep", "map", "print", "push", "join":
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

func (s *scanner) heredocMarker() bool {
	start := s.i
	j := s.i
	if j+1 >= len(s.b) || s.b[j] != '<' || s.b[j+1] != '<' {
		return false
	}
	j += 2
	if j < len(s.b) && s.b[j] == '~' {
		j++
	}
	if j >= len(s.b) {
		return false
	}

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
		s.pending = append(s.pending, heredoc{term: term})
		return true
	}

	if !isIdentStart(s.b[j]) {
		return false
	}
	k := j
	for k < len(s.b) && isIdentPart(s.b[k]) {
		k++
	}
	// A shift of a variable, "<< $x", is not a heredoc. Adjacency is required.
	term := string(s.b[j:k])
	s.i = k
	s.emit(host.Heredoc, start, k)
	s.pending = append(s.pending, heredoc{term: term})
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
			if strings.TrimSpace(line) == h.term {
				break
			}
		}
		s.emit(host.Heredoc, start, s.i)
	}
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
