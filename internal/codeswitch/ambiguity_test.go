package codeswitch_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"enx/internal/codeswitch"
	"enx/internal/core"
	"enx/internal/host"
	"enx/internal/host/ruby"
)

// unrestrained is a cost model in which everything is equally cheap, used to
// check the pinning wrapper in isolation.
type unrestrained struct{}

func (unrestrained) Keep(int) float64 { return 1 }
func (unrestrained) Skip(int) float64 { return 1 }

func TestPinnedForbidsThePinnedDecision(t *testing.T) {
	// A token pinned to code cannot be skipped; one pinned to prose cannot be
	// kept. It is the whole mechanism: no grammar changes, only costs.
	costs := unrestrained{}

	if got := codeswitch.PinForTest(costs, map[int]bool{3: true}, nil).Skip(3); got != host.ImpossibleCost {
		t.Errorf("pinned-to-code Skip = %v, want impossible", got)
	}
	if got := codeswitch.PinForTest(costs, map[int]bool{3: true}, nil).Keep(3); got != 1 {
		t.Errorf("pinned-to-code Keep = %v, want the inner cost", got)
	}
	if got := codeswitch.PinForTest(costs, nil, map[int]bool{4: true}).Keep(4); got != host.ImpossibleCost {
		t.Errorf("pinned-to-prose Keep = %v, want impossible", got)
	}
	if got := codeswitch.PinForTest(costs, nil, map[int]bool{4: true}).Skip(4); got != 1 {
		t.Errorf("pinned-to-prose Skip = %v, want the inner cost", got)
	}
	// An unpinned token is untouched.
	if got := codeswitch.PinForTest(costs, map[int]bool{3: true}, nil).Skip(9); got != 1 {
		t.Errorf("unpinned Skip = %v, want the inner cost", got)
	}
}

func decodeFile(t *testing.T, path string) (*core.Source, []host.Token, *host.Path, *codeswitch.Lexicon) {
	t.Helper()
	lex, err := codeswitch.LoadLexicon()
	if err != nil {
		t.Fatalf("LoadLexicon: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	src := core.NewSource(path, data)
	toks, _ := ruby.New().Lex(src)
	_, path2, ok := codeswitch.Decode(ruby.NewMachine(), lex, ruby.NewVocabulary(), toks)
	if !ok {
		t.Fatalf("decode failed for %s", path)
	}
	return src, toks, path2, lex
}

func exampleFile(name string) string {
	return filepath.Join("..", "..", "examples", "showcase", "ruby", name)
}

// TestAmbiguityFindsCommonEnglishKeptAsCode is the case the analysis exists
// for: "sum" and "answer" are ordinary English words that the grammar kept as
// code, and the opposite reading is within a hair of the chosen one.
func TestAmbiguityFindsCommonEnglishKeptAsCode(t *testing.T) {
	src, toks, path, lex := decodeFile(t, exampleFile("greeting.enx"))

	ambiguous, truncated := codeswitch.FindAmbiguities(
		src, ruby.NewMachine(), lex, ruby.NewVocabulary(), toks, path, codeswitch.DefaultMargin)
	if truncated {
		t.Log("analysis was truncated; results are partial")
	}

	var texts []string
	for _, a := range ambiguous {
		texts = append(texts, strings.TrimSpace(a.Text))
		if a.Chosen != codeswitch.Code {
			t.Errorf("%q: Chosen = %v, want code", a.Text, a.Chosen)
		}
		if a.Margin() < 0 || a.Margin() > codeswitch.DefaultMargin {
			t.Errorf("%q: margin %.3f is outside the reported range", a.Text, a.Margin())
		}
		if a.Alternative() != codeswitch.English {
			t.Errorf("%q: Alternative = %v, want english", a.Text, a.Alternative())
		}
	}
	joined := strings.Join(texts, " ")
	for _, want := range []string{"sum", "answer"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%q was not reported as ambiguous; got %v", want, texts)
		}
	}
}

// TestAmbiguityIsSilentForCodeShapedNames checks the analysis is not simply
// reporting everything: a name that looks like code is never suspicious.
func TestAmbiguityIsSilentForCodeShapedNames(t *testing.T) {
	src, toks, path, lex := decodeFile(t, exampleFile("shop.enx"))

	ambiguous, _ := codeswitch.FindAmbiguities(
		src, ruby.NewMachine(), lex, ruby.NewVocabulary(), toks, path, codeswitch.DefaultMargin)

	for _, a := range ambiguous {
		text := strings.TrimSpace(a.Text)
		if strings.ContainsAny(text, "_0123456789") {
			t.Errorf("reported a code-shaped name as ambiguous: %q", text)
		}
		if codeswitch.LooksLikeCode(text) {
			t.Errorf("reported a word that looks like code: %q", text)
		}
	}
}

// TestAmbiguityMarginShrinksTheReport checks that the margin is what controls
// the report, so a caller can ask for only the tightest cases.
func TestAmbiguityMarginShrinksTheReport(t *testing.T) {
	src, toks, path, lex := decodeFile(t, exampleFile("greeting.enx"))

	wide, _ := codeswitch.FindAmbiguities(src, ruby.NewMachine(), lex, ruby.NewVocabulary(), toks, path, 5)
	tight, _ := codeswitch.FindAmbiguities(src, ruby.NewMachine(), lex, ruby.NewVocabulary(), toks, path, 0.5)

	if len(tight) > len(wide) {
		t.Fatalf("a tighter margin reported more: %d > %d", len(tight), len(wide))
	}
	none, _ := codeswitch.FindAmbiguities(src, ruby.NewMachine(), lex, ruby.NewVocabulary(), toks, path, -1)
	if len(none) != 0 {
		t.Fatalf("an impossible margin still reported %d", len(none))
	}
}

func TestAmbiguityHandlesDegenerateInput(t *testing.T) {
	if got, _ := codeswitch.FindAmbiguities(nil, nil, nil, nil, nil, nil, 1); got != nil {
		t.Fatal("a nil machine must report nothing")
	}
	src := core.NewSource("empty.rb", nil)
	if got, _ := codeswitch.FindAmbiguities(src, ruby.NewMachine(), nil, nil, nil, nil, 1); got != nil {
		t.Fatal("empty input must report nothing")
	}
}

func TestLabelsFromPath(t *testing.T) {
	toks := []host.Token{
		{Kind: host.Identifier, Text: "x"},
		{Kind: host.Space, Text: " "},
		{Kind: host.Operator, Text: "="},
	}
	path := &host.Path{Actions: []host.Action{host.KeepToken, host.SkipToken, host.SkipToken}}
	got := codeswitch.LabelsFromPath(toks, path)
	want := []codeswitch.Label{codeswitch.Code, codeswitch.Trivia, codeswitch.English}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("labels = %v, want %v", got, want)
		}
	}
}
