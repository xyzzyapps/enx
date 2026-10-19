// Package codeswitch decides, token by token, whether a token belongs to the
// code or to the surrounding English.
//
// The model is code-switching: the file is one stream, English and the host
// language alternate without any delimiter, and every token is labelled code,
// English, or trivia. English tokens are deleted, code tokens are preserved
// byte for byte, so the emitted program is always a subsequence of the input.
package codeswitch

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"

	"enx/data"
)

// UnknownEnglishCost is the cost assigned to a word that is absent from the
// lexicon. It must be high enough that an identifier the lexicon has never
// seen is only treated as English when the grammar leaves no alternative.
const UnknownEnglishCost = 24.0

// Lexicon is the English half of the model: a word-frequency table used to
// estimate how likely a bare word is to be prose rather than code.
type Lexicon struct {
	// count maps a word to its corpus frequency.
	count map[string]float64
	// total is the sum of all counts, used to normalise counts into
	// probabilities.
	total float64
	// minCost and maxCost bound LogCost, for reporting only.
	minCost, maxCost float64
}

var (
	embeddedOnce sync.Once
	embeddedLex  *Lexicon
	embeddedErr  error
)

// LoadLexicon returns the lexicon embedded in the binary.
//
// The table is immutable, so it is parsed once and shared. Loading the full
// 333k-word table is not cheap, and every command and every test needs the same
// one.
func LoadLexicon() (*Lexicon, error) {
	embeddedOnce.Do(func() {
		embeddedLex, embeddedErr = parseEmbeddedLexicon()
	})
	return embeddedLex, embeddedErr
}

func parseEmbeddedLexicon() (*Lexicon, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data.EnglishFreqGz))
	if err != nil {
		return nil, fmt.Errorf("lexicon: open embedded gzip: %w", err)
	}
	defer zr.Close()
	return ParseLexicon(zr)
}

// ParseLexicon reads a "word<TAB>count" table, one record per line.
//
// Malformed lines are skipped rather than rejected: the table is generated
// data, and one bad line should not make the tool unusable.
func ParseLexicon(r io.Reader) (*Lexicon, error) {
	l := &Lexicon{count: make(map[string]float64)}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		if line == "" {
			continue
		}
		tab := strings.IndexByte(line, '\t')
		if tab <= 0 || tab == len(line)-1 {
			continue
		}
		word := line[:tab]
		var c float64
		if _, err := fmt.Sscanf(line[tab+1:], "%g", &c); err != nil || c <= 0 {
			continue
		}
		l.count[word] += c
		l.total += c
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("lexicon: read table: %w", err)
	}
	if l.total <= 0 {
		return nil, fmt.Errorf("lexicon: empty or invalid table after %d lines", n)
	}
	l.computeBounds()
	return l, nil
}

// NewLexicon builds a lexicon from explicit word counts.
//
// It exists for tests, and for callers that want a domain-specific or
// restricted vocabulary instead of the general English table.
func NewLexicon(counts map[string]float64) *Lexicon {
	l := &Lexicon{count: make(map[string]float64, len(counts))}
	for word, c := range counts {
		if c <= 0 {
			continue
		}
		l.count[strings.ToLower(word)] += c
		l.total += c
	}
	if l.total > 0 {
		l.computeBounds()
	}
	return l
}

func (l *Lexicon) computeBounds() {
	l.minCost = math.Inf(1)
	for _, c := range l.count {
		cost := -math.Log(c / l.total)
		if cost < l.minCost {
			l.minCost = cost
		}
		if cost > l.maxCost {
			l.maxCost = cost
		}
	}
}

// Size returns the number of distinct words.
func (l *Lexicon) Size() int {
	if l == nil {
		return 0
	}
	return len(l.count)
}

// Total returns the sum of all corpus counts.
func (l *Lexicon) Total() float64 {
	if l == nil {
		return 0
	}
	return l.total
}

// Has reports whether word is in the lexicon.
func (l *Lexicon) Has(word string) bool {
	_, ok := l.Probability(word)
	return ok
}

// Probability returns the corpus probability of word.
//
// A nil Lexicon behaves as an empty one: nothing is an English word, so no
// token can ever be labelled English.
func (l *Lexicon) Probability(word string) (float64, bool) {
	if l == nil {
		return 0, false
	}
	c, ok := l.count[strings.ToLower(word)]
	if !ok {
		return 0, false
	}
	return c / l.total, true
}

// LogCost returns -ln(P(word)), the cost of calling word English. Lower is
// cheaper, so common English words are cheap and rare ones expensive.
func (l *Lexicon) LogCost(word string) (float64, bool) {
	p, ok := l.Probability(word)
	if !ok {
		return 0, false
	}
	return -math.Log(p), true
}

// EnglishCost is LogCost with a fallback for words the lexicon has never seen.
func (l *Lexicon) EnglishCost(word string) float64 {
	if c, ok := l.LogCost(word); ok {
		return c
	}
	return UnknownEnglishCost
}

// CostRange returns the cheapest and dearest English costs in the lexicon.
func (l *Lexicon) CostRange() (minCost, maxCost float64) {
	if l == nil {
		return 0, 0
	}
	return l.minCost, l.maxCost
}
