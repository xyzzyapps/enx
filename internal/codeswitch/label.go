package codeswitch

import (
	"math"

	"enx/internal/host"
)

// Label is the decision for one token.
type Label uint8

const (
	// Undecided means no decision has been made yet.
	Undecided Label = iota
	// Code means the token belongs to the program and is emitted.
	Code
	// English means the token is prose and is deleted.
	English
	// Trivia means spacing or a comment: always preserved, never English.
	Trivia
)

// String renders the label in lower case.
func (l Label) String() string {
	switch l {
	case Code:
		return "code"
	case English:
		return "english"
	case Trivia:
		return "trivia"
	default:
		return "undecided"
	}
}

// ImpossibleCost marks a labelling that the model forbids outright.
var ImpossibleCost = math.Inf(1)

// CodeBias is added to the code cost of a bare word that is also a common
// English word, so that in the absence of grammar evidence the English reading
// wins. Without a grammar this is what makes prose prose.
//
// It also has to be large enough that a short run of prose is cheaper to skip
// than to keep, while a genuine two-word call such as "puts answer" is cheaper
// to keep: the switch cost is paid once per run, so a run of one or two words
// turns on this value. See SPEC.md section on cost calibration.
const CodeBias = 4.0

// PlausibleEnglishCost is charged for a word the lexicon has never seen that
// still looks like a word: lower case letters only, no digits or underscores.
//
// Such a token could be a variable or could be rare English, so it must be
// skippable, but not so cheaply that prose wins by default.
const PlausibleEnglishCost = 10.0

// CodeShapedEnglishCost is charged for a word that looks like an identifier —
// underscores, digits, CamelCase, SCREAMING case — but still has to be
// skippable.
//
// It is deliberately large and deliberately finite. Making such words
// impossible to skip looks safe and is not: prose contains acronyms, and "SKU"
// is indistinguishable from a constant. An unskippable token that the grammar
// also cannot use leaves no parse at all, so the whole file fails. Expensive
// and possible is strictly better than free and impossible.
const CodeShapedEnglishCost = 22.0

// FunctionWordCutoff is the English cost below which a word is treated as a
// function word: "the", "of", "and", "is", "we", "it", "as". Roughly the
// highest-frequency few hundred words fall below it. LogCost is -ln P(word), so
// a smaller cost means a more frequent word.
const FunctionWordCutoff = 7.0

// FunctionWordKeep is the cost of calling a function word code.
//
// It is deliberately large. Function words are the one class of English word
// that is almost never an identifier in real code, so keeping one is evidence
// that a whole run is prose. This is what separates "print the answer", which
// is prose, from "puts answer", which is a call: the first contains "the".
const FunctionWordKeep = 28.0

// shortFunctionWords are function words the lexicon cannot supply because it
// keeps only words of two letters or more. "a" is the commonest word in
// English and is also a legal Ruby identifier, so it has to be named here.
//
// "i" is deliberately absent: lower case "i" is a loop variable far more often
// than it is prose.
var shortFunctionWords = map[string]bool{
	"a": true,
}

// maxIdentifierLength is the length beyond which a bare word is assumed to be
// a code identifier rather than an English word.
const maxIdentifierLength = 14

// LooksLikeCode is in leaf.go: it is a lexical prior only. The grammar, not
// this package, has the final say on whether a word is code.

// CodeCost estimates how unlikely it is that tok is code. It is zero for
// tokens that can only be code, and rises for bare words that are common
// English.
//
// Only words compete between the two languages, so everything that is not a
// word falls through to zero: keywords, operators, literals and variables have
// no English reading. Whether the grammar will actually accept them in place
// is the decoder's problem, not this function's.
func CodeCost(tok host.Token, lex *Lexicon) float64 {
	if tok.Kind == host.Prose {
		// The author marked the span as prose, so the grammar cannot keep it.
		return ImpossibleCost
	}
	if !tok.Kind.IsWord() {
		return 0
	}
	if LooksLikeCode(tok.Text) {
		return 0
	}
	if c, ok := lex.LogCost(tok.Text); ok {
		return CodeBias + c
	}
	return 0
}

// EnglishCost estimates how unlikely it is that tok is English. It is
// ImpossibleCost for everything that cannot be an English word, and the
// lexicon's negative log probability for a bare word.
func EnglishCost(tok host.Token, lex *Lexicon) float64 {
	if tok.Kind == host.Prose {
		// The author has already decided the span is prose; the escape
		// delimiter itself imposes no extra decision cost.
		return EscapeCost
	}
	if !tok.Kind.IsWord() {
		return ImpossibleCost
	}
	if c, ok := lex.LogCost(tok.Text); ok {
		return c
	}
	return ImpossibleCost
}

// Decision records the labelling of one token and the evidence behind it.
type Decision struct {
	Index       int
	Token       host.Token
	Label       Label
	CodeCost    float64
	EnglishCost float64
	Reason      string
}

// Labelling is the result for a whole file.
type Labelling struct {
	// Mode names the algorithm that produced the labelling, for example
	// "lexical" or "grammar".
	Mode string
	// Decisions holds one entry per token, in token order.
	Decisions []Decision
	// CodeCost and EnglishCost are the totals of the entries that were
	// actually chosen.
	CodeCost    float64
	EnglishCost float64
}

// Labels returns the label of each token, in token order.
func (l *Labelling) Labels() []Label {
	out := make([]Label, len(l.Decisions))
	for i, d := range l.Decisions {
		out[i] = d.Label
	}
	return out
}

// Counts returns the number of tokens at each label.
func (l *Labelling) Counts() (code, english, trivia int) {
	for _, d := range l.Decisions {
		switch d.Label {
		case Code:
			code++
		case English:
			english++
		case Trivia:
			trivia++
		}
	}
	return code, english, trivia
}

// ClassifyLexical labels tokens using the lexicon alone, with no grammar
// constraint.
//
// This is a diagnostic and prior, not a correct decoder: prose punctuation and
// prose words that look like identifiers are both mislabelled, and prose that
// happens to parse as code is taken as code. The grammar decoder replaces it;
// see Decoder.
func ClassifyLexical(lex *Lexicon, toks []host.Token) *Labelling {
	lab := &Labelling{Mode: "lexical", Decisions: make([]Decision, 0, len(toks))}

	for i, tok := range toks {
		d := Decision{Index: i, Token: tok}
		switch {
		case tok.IsTrivia():
			d.Label = Trivia
			d.Reason = "trivia is always preserved"
		case tok.Kind == host.Prose:
			d.CodeCost = ImpossibleCost
			d.EnglishCost = EscapeCost
			d.Label = English
			d.Reason = "the author marked it as prose"
			lab.EnglishCost += d.EnglishCost
		default:
			d.CodeCost = CodeCost(tok, lex)
			d.EnglishCost = EnglishCost(tok, lex)
			if d.EnglishCost != ImpossibleCost && d.EnglishCost < d.CodeCost {
				d.Label = English
				d.Reason = "lexicon: common English word, no grammar evidence to keep it"
				lab.EnglishCost += d.EnglishCost
			} else {
				d.Label = Code
				d.Reason = codeReason(tok)
				lab.CodeCost += d.CodeCost
			}
		}
		lab.Decisions = append(lab.Decisions, d)
	}
	return lab
}

func codeReason(tok host.Token) string {
	switch {
	case tok.Kind.AlwaysCode():
		return "kind " + tok.Kind.String() + " has no English reading"
	case tok.Kind == host.Keyword:
		return "keyword, kept as code until the grammar can refuse it"
	case LooksLikeCode(tok.Text):
		return "identifier shape"
	default:
		return "word unknown to the lexicon"
	}
}
