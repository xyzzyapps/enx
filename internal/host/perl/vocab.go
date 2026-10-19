package perl

// Vocabulary is the set of names that are Perl built-ins often enough to be
// worth telling the decoder about.
//
// It matters because Perl's list operators are ordinary English words: "print"
// and "say" must be kept as code even though the lexicon would rather skip
// them. The machine already refuses to treat any other bareword as a call, so
// the vocabulary only has to cover the built-ins.
type Vocabulary struct{}

// NewVocabulary returns the Perl vocabulary.
func NewVocabulary() *Vocabulary { return &Vocabulary{} }

// Knows implements host.Vocabulary.
func (v *Vocabulary) Knows(word string) bool { return listOps[word] }
