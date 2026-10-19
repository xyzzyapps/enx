package c

// Vocabulary is the set of C library names that are also ordinary English
// words.
//
// It is much smaller than the other languages' would be, and the reason is
// structural: every C function call needs parentheses, so a call is already
// recognised by shape. `free(p)` is code because `free` is written against a
// `(`. The vocabulary therefore only has to cover names that appear without
// parentheses, which in practice means nothing — but the list is kept explicit
// because these are exactly the words that will cause trouble if a construct is
// ever added that lets a bare name stand as a value.
type Vocabulary struct{}

// NewVocabulary returns the C vocabulary.
func NewVocabulary() *Vocabulary { return &Vocabulary{} }

// Knows implements host.Vocabulary.
func (v *Vocabulary) Knows(word string) bool { return libraryNames[word] }

// libraryNames are C standard library identifiers that read as English.
var libraryNames = map[string]bool{
	"free": true, "exit": true, "abort": true, "abs": true, "div": true,
	"time": true, "clock": true, "sleep": true, "rand": true, "srand": true,
	"open": true, "close": true, "read": true, "write": true,
	"index": true, "rindex": true, "signal": true, "raise": true,
	"system": true, "puts": true, "putchar": true, "getchar": true,
	"main": true, "pow": true, "exp": true, "log": true, "floor": true,
	"ceil": true, "round": true, "assert": true, "memcpy": true,
	"memset": true, "malloc": true, "calloc": true, "realloc": true,
	"strlen": true, "sizeof": true, "offsetof": true,
}
