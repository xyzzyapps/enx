package host

// Vocabulary tells the cost model which bare words are names in the host
// language.
//
// This is the one piece of host knowledge that spelling alone cannot supply.
// "puts answer" and "we start" are both two adjacent lower-case words; the only
// difference is that "puts" is a Ruby method. Without a vocabulary the decoder
// has to weigh keeping two words against skipping them, and for a two word run
// either answer can be made to win by tuning constants. With one, the choice is
// decided by knowledge instead of by arithmetic.
//
// Implementations should be conservative: including a word that is common in
// prose makes prose cheaper to keep, which is the expensive kind of mistake.
type Vocabulary interface {
	// Knows reports whether word is a well known name in the host language.
	Knows(word string) bool
}
