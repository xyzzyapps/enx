package pipeline_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"enx/internal/core"
	"enx/internal/lang"
	"enx/internal/pipeline"
)

// syntheticRuby builds a mixed file of n statements with a line of prose in
// front of each, which is the shape of every real input in the examples but long
// enough to measure.
func syntheticRuby(n int) string {
	var b strings.Builder
	b.Grow(n * 80)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "we set the value of thing %d here\n", i)
		fmt.Fprintf(&b, "thing_%d = compute(thing_%d, %d)\n", i, i, i)
	}
	return b.String()
}

func decodeSynthetic(t *testing.T, n int) (time.Duration, int) {
	t.Helper()
	text := syntheticRuby(n)
	src := core.NewSource("synthetic.enx", []byte(text))
	opts := options(t, "ruby")

	start := time.Now()
	res, err := pipeline.Run(src, lang.New(), opts)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Labelling == nil {
		t.Fatal("no labelling")
	}
	return elapsed, len(res.Tokens)
}

// TestDecodeScalesWithFileSize reports the decoding time at three sizes rather
// than asserting a tight bound, because the numbers are what matter and a
// timing assertion in CI would be flaky. A super-linear blow-up shows up as a
// ratio far above 2 between successive sizes.
//
// The cost of the search is not obviously linear: every result carries the
// token indices it kept, and combining two results copies them, so the total
// work can grow with the square of the file. This measures whether it does.
func TestDecodeScalesWithFileSize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the scaling measurement in short mode")
	}

	sizes := []int{250, 500, 1000, 2000}
	var prev, last time.Duration
	for _, n := range sizes {
		elapsed, tokens := decodeSynthetic(t, n)
		ratio := "first"
		if prev > 0 {
			ratio = fmt.Sprintf("%.1fx", float64(elapsed)/float64(prev))
		}
		t.Logf("statements=%-5d tokens=%-6d elapsed=%-10v growth=%s", n, tokens, elapsed.Round(time.Millisecond), ratio)
		prev = elapsed
		last = elapsed
	}

	// A loose ceiling, chosen so a pathological regression fails loudly without
	// ordinary machine noise doing the same.
	if last > 30*time.Second {
		t.Fatalf("decoding 2000 statements took %v, which is far beyond expectation", last)
	}
}

func BenchmarkDecode(b *testing.B) {
	for _, n := range []int{100, 500, 1000} {
		b.Run(fmt.Sprintf("statements=%d", n), func(b *testing.B) {
			src := core.NewSource("synthetic.enx", []byte(syntheticRuby(n)))
			lex, err := loadLexiconForBench()
			if err != nil {
				b.Fatalf("lexicon: %v", err)
			}
			opts := pipeline.Options{Language: "ruby", Lexicon: lex}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := pipeline.Run(src, lang.New(), opts); err != nil {
					b.Fatalf("Run: %v", err)
				}
			}
		})
	}
}
