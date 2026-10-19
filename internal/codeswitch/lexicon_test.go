package codeswitch

import (
	"strings"
	"testing"
)

func TestLoadEmbeddedLexicon(t *testing.T) {
	lex, err := LoadLexicon()
	if err != nil {
		t.Fatalf("LoadLexicon: %v", err)
	}
	if lex.Size() < 300000 {
		t.Fatalf("lexicon size = %d, want at least 300000", lex.Size())
	}
	for _, w := range []string{"the", "we", "sum", "answer", "finally", "compute"} {
		if !lex.Has(w) {
			t.Errorf("lexicon is missing %q", w)
		}
	}
	if lex.Has("zzzznotaword") {
		t.Error("lexicon contains a nonsense word")
	}

	minCost, maxCost := lex.CostRange()
	if !(minCost > 0 && maxCost > minCost) {
		t.Fatalf("cost range = %v..%v, want increasing positive values", minCost, maxCost)
	}

	// A common word must be cheaper to call English than a rare one.
	the, okThe := lex.LogCost("the")
	rare, okRare := lex.LogCost("arguments")
	if !okThe || !okRare {
		t.Fatal("expected both words in the lexicon")
	}
	if !(the < rare) {
		t.Fatalf("cost(the) = %v should be less than cost(arguments) = %v", the, rare)
	}
}

// TestLoadLexiconIsCached checks the table is parsed once and shared. It is
// immutable, so sharing is safe and saves a substantial parse per call.
func TestLoadLexiconIsCached(t *testing.T) {
	first, err := LoadLexicon()
	if err != nil {
		t.Fatalf("LoadLexicon: %v", err)
	}
	second, err := LoadLexicon()
	if err != nil {
		t.Fatalf("LoadLexicon: %v", err)
	}
	if first != second {
		t.Fatal("LoadLexicon returned two different instances")
	}
}

func TestLexiconLookupIsCaseInsensitive(t *testing.T) {
	lex, err := LoadLexicon()
	if err != nil {
		t.Fatalf("LoadLexicon: %v", err)
	}
	lower, ok := lex.LogCost("we")
	upper, ok2 := lex.LogCost("We")
	if !ok || !ok2 || lower != upper {
		t.Fatalf("case sensitivity: %v/%v vs %v/%v", lower, ok, upper, ok2)
	}
}

func TestNilLexiconIsEmpty(t *testing.T) {
	var lex *Lexicon
	if lex.Size() != 0 || lex.Has("the") || lex.Total() != 0 {
		t.Fatal("nil lexicon should behave as empty")
	}
	if c := lex.EnglishCost("the"); c != UnknownEnglishCost {
		t.Fatalf("EnglishCost = %v, want %v", c, UnknownEnglishCost)
	}
	if minCost, maxCost := lex.CostRange(); minCost != 0 || maxCost != 0 {
		t.Fatal("nil lexicon cost range should be zero")
	}
}

func TestParseLexiconSkipsMalformedLines(t *testing.T) {
	table := "the\t100\nof\t50\nbadline\nnoTabCount\t\nzero\t0\nnegative\t-3\n"
	lex, err := ParseLexicon(strings.NewReader(table))
	if err != nil {
		t.Fatalf("ParseLexicon: %v", err)
	}
	if lex.Size() != 2 {
		t.Fatalf("size = %d, want 2 (the, of)", lex.Size())
	}
	if !lex.Has("the") || !lex.Has("of") {
		t.Fatal("expected the and of")
	}
}

func TestParseLexiconRejectsEmptyTable(t *testing.T) {
	if _, err := ParseLexicon(strings.NewReader("nothing here\n")); err == nil {
		t.Fatal("expected an error for an empty table")
	}
}

func TestEnglishCostFallsBackForUnknownWords(t *testing.T) {
	// A second word keeps the probabilities below one, so the cost is positive.
	lex := NewLexicon(map[string]float64{"we": 100, "other": 100})
	if c := lex.EnglishCost("we"); c <= 0 {
		t.Fatalf("known word cost = %v", c)
	}
	if c := lex.EnglishCost("yesterdayz"); c != UnknownEnglishCost {
		t.Fatalf("unknown word cost = %v, want %v", c, UnknownEnglishCost)
	}
}
