package pipeline_test

import (
	"errors"
	"strings"
	"testing"

	"enx/internal/codeswitch"
	"enx/internal/core"
	"enx/internal/host"
	"enx/internal/host/ruby"
	"enx/internal/lang"
	"enx/internal/pipeline"
	"enx/internal/registry"
)

// loadLexiconForBench loads the lexicon for the benchmark, which has no
// *testing.T to report through.
func loadLexiconForBench() (*codeswitch.Lexicon, error) { return codeswitch.LoadLexicon() }

// language pairs a host language with the toolchain that runs an emitted file.
type language struct {
	name string
	// interpreter runs an emitted file directly, with syntaxFlag asking it to
	// check without running.
	interpreter string
	syntaxFlag  string
	// compiler builds an emitted file; {src} and {out} are substituted. A
	// compiled language has no interpreter.
	compiler    string
	compileArgs []string
}

var languages = []language{
	{name: "ruby", interpreter: "ruby", syntaxFlag: "-c"},
	{name: "perl", interpreter: "perl", syntaxFlag: "-c"},
	{
		name:        "c",
		compiler:    "gcc",
		compileArgs: []string{"-x", "c", "-std=c11", "-Wall", "-Wextra"},
	},
}

func options(t *testing.T, lang string) pipeline.Options {
	t.Helper()
	lex, err := codeswitch.LoadLexicon()
	if err != nil {
		t.Fatalf("LoadLexicon: %v", err)
	}
	return pipeline.Options{Language: lang, Lexicon: lex}
}

func TestUnknownLanguage(t *testing.T) {
	src := core.NewSource("x.enx", []byte("x = 1\n"))
	_, err := pipeline.Run(src, lang.New(), pipeline.Options{Language: "klingon"})
	if err == nil || !strings.Contains(err.Error(), "unknown language") {
		t.Fatalf("err = %v, want an unknown-language error", err)
	}
}

// TestRefusesToEmitWithoutGrammar is the safety property: a language with no
// grammar machine must not silently produce plausible-looking output.
func TestRefusesToEmitWithoutGrammar(t *testing.T) {
	reg := registry.New()
	reg.Register(host.Spec{Language: "nostalgia", Lexer: ruby.New()})

	src := core.NewSource("x.enx", []byte("x = 1\n"))
	_, err := pipeline.Run(src, reg, pipeline.Options{Language: "nostalgia"})
	if !errors.Is(err, pipeline.ErrNoGrammar) {
		t.Fatalf("err = %v, want ErrNoGrammar", err)
	}
}

// TestLanguagesAreReady checks that every registered language can emit, since
// that is the state each milestone leaves behind.
func TestLanguagesAreReady(t *testing.T) {
	reg := lang.New()
	for _, name := range reg.Languages() {
		spec, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		if !spec.Ready() {
			t.Errorf("%s has no grammar machine", name)
		}
		if spec.Vocabulary == nil {
			t.Errorf("%s has no vocabulary", name)
		}
	}
}

func TestProseIsDropped(t *testing.T) {
	src := core.NewSource("x.enx", []byte("we set the answer to one\nanswer = 1\nand then we show it\nputs answer\n"))
	opts := options(t, "ruby")
	res, err := pipeline.Run(src, lang.New(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := res.Output(opts)
	if strings.Contains(got, "we set") || strings.Contains(got, "and then") {
		t.Fatalf("prose survived:\n%s", got)
	}
	if !strings.Contains(got, "answer = 1") || !strings.Contains(got, "puts answer") {
		t.Fatalf("code was lost:\n%s", got)
	}
}

// TestDropWarnings covers the safety net for silent drops: a prose-only file,
// and a file where almost every non-trivia token was deleted.
func TestDropWarnings(t *testing.T) {
	opts := options(t, "ruby")

	src := core.NewSource("prose.enx", []byte("this file is only a note to the reader\n"))
	res, err := pipeline.Run(src, lang.New(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasDiagnostic(res.Diags, "no-code-kept") {
		t.Fatalf("expected no-code-kept, got %v", res.Diags)
	}

	src = core.NewSource("mostly.enx", []byte(
		"we compute the sum of the values and print it out\n"+
			"we compute the sum of the values and print it out\n"+
			"we compute the sum of the values and print it out\n"+
			"we compute the sum of the values and print it out\n"+
			"x = 1\n"))
	res, err = pipeline.Run(src, lang.New(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hasDiagnostic(res.Diags, "mostly-dropped") {
		t.Fatalf("expected mostly-dropped, got %v", res.Diags)
	}
}

func hasDiagnostic(diags []core.Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

// normalise removes the CR that Windows interpreters put before each newline.
func normalise(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func TestKeepProseOption(t *testing.T) {
	src := core.NewSource("x.enx", []byte("x = 1\nwe compute the sum\ny = 2\n"))
	opts := options(t, "ruby")

	res, err := pipeline.Run(src, lang.New(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := res.Output(opts); strings.Contains(got, "compute") {
		t.Fatalf("expected English deleted, got %q", got)
	}

	opts.KeepProse = true
	got := res.Output(opts)
	if !strings.Contains(got, "# we compute the sum") {
		t.Fatalf("expected prose as a comment, got %q", got)
	}
	if !strings.Contains(got, "x = 1") || !strings.Contains(got, "y = 2") {
		t.Fatalf("code was lost: %q", got)
	}
}

// TestEscapeForcesProse is the point of the escape: a span the model would else
// keep as code can be declared prose by the author instead.
//
// Ruby and C need it most, because there a lone lower case word is a valid
// expression statement and cheap enough to keep. Perl needs it least: under
// strict subs a bareword alone is not a statement at all, so the grammar
// already refuses it.
func TestEscapeForcesProse(t *testing.T) {
	cases := []struct {
		lang    string
		without string
		with    string
		code    []string
	}{
		{
			lang:    "ruby",
			without: "x = 5\ntotal\ny = 6\n",
			with:    "x = 5\n~ total ~\ny = 6\n",
			code:    []string{"x = 5", "y = 6"},
		},
		{
			lang:    "perl",
			without: "my $x = 5;\nmy $y = 6;\n",
			with:    "my $x = 5;\n~ note to self ~\nmy $y = 6;\n",
			code:    []string{"my $x = 5;", "my $y = 6;"},
		},
		{
			lang:    "c",
			without: "int x = 5;\ntotal;\nint y = 6;\n",
			with:    "int x = 5;\n~ total ~\nint y = 6;\n",
			code:    []string{"int x = 5;", "int y = 6;"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			opts := options(t, tc.lang)
			run := func(text string) string {
				res, err := pipeline.Run(core.NewSource("e.enx", []byte(text)), lang.New(), opts)
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				return res.Output(opts)
			}

			if got := run(tc.without); !strings.Contains(got, "total") && !strings.Contains(got, "note") {
				t.Logf("the span was already dropped without the escape: %q", got)
			}

			got := run(tc.with)
			if strings.Contains(got, "total") || strings.Contains(got, "note") {
				t.Fatalf("the escape did not remove the span: %q", got)
			}
			if strings.Contains(got, "~") {
				t.Fatalf("the delimiters survived: %q", got)
			}
			for _, want := range tc.code {
				if !strings.Contains(got, want) {
					t.Fatalf("code around the escape was lost: %q", got)
				}
			}
		})
	}
}

// TestEscapeLeavesRealTildesAlone guards the narrow rule: a tilde that is an
// operator must not be mistaken for an escape.
func TestEscapeLeavesRealTildesAlone(t *testing.T) {
	src := core.NewSource("t.c", []byte("int f(void) {\n    int x = ~a + ~b;\n    return x;\n}\n"))
	opts := options(t, "c")
	res, err := pipeline.Run(src, lang.New(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := res.Output(opts)
	if got != string(src.Bytes()) {
		t.Fatalf("output differs from input:\n%q\n%q", got, string(src.Bytes()))
	}
}

// TestAnaphoraEndToEnd is the feature across all three hosts, including the
// guard that the marker is not claimed unless asked for.
func TestAnaphoraEndToEnd(t *testing.T) {
	cases := []struct {
		lang string
		text string
		want []string
	}{
		{
			lang: "ruby",
			text: "stock = 10\nmugs = stock - 3\ntotal = !-1 + !-2\n",
			want: []string{"total = mugs + stock"},
		},
		{
			lang: "perl",
			text: "my $stock = 10;\nmy $mugs = $stock - 3;\nmy $total = !-1 + !-2;\n",
			want: []string{"my $total = $mugs + $stock;"},
		},
		{
			lang: "c",
			text: "int main(void) {\n  int stock = 10;\n  int mugs = stock - 3;\n  int total = !-1 + !-2;\n  return total;\n}\n",
			want: []string{"int total = mugs + stock;"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			src := core.NewSource("a.enx", []byte(tc.text))

			// Off by default: the marker is not claimed. Ruby and C copy the
			// text through unchanged; Perl reads *1 as a typeglob and mangles
			// it, which is precisely why the marker must be claimed rather than
			// assumed (SPEC 18.2). The test therefore only requires that the
			// feature did not run.
			off := options(t, tc.lang)
			res, err := pipeline.Run(src, lang.New(), off)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if res.Anaphora != nil {
				t.Fatal("anaphora ran without being asked for")
			}

			// On: the marker is claimed and resolved.
			on := options(t, tc.lang)
			on.AnaphorMarker = '!'
			res, err = pipeline.Run(src, lang.New(), on)
			if err != nil {
				t.Fatalf("Run with anaphora: %v", err)
			}
			if res.Anaphora == nil {
				t.Fatal("no anaphora result")
			}
			got := res.Output(on)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("output %q does not contain %q", got, want)
				}
			}
			if strings.Contains(got, "!-1") || strings.Contains(got, "!-2") {
				t.Fatalf("a marker survived into the output: %q", got)
			}
		})
	}
}

// TestAnaphoraDoesNotChangeCodeThatUsedTheMarker is the reason the marker is
// claimed rather than assumed: in Ruby and Perl "*1" already means something.
func TestAnaphoraDoesNotChangeCodeThatUsedTheMarker(t *testing.T) {
	// In Ruby, "y = *1" splats the literal 1. With anaphora off it must be
	// copied unchanged.
	text := "y = *1\n"
	src := core.NewSource("s.enx", []byte(text))
	opts := options(t, "ruby")
	res, err := pipeline.Run(src, lang.New(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := res.Output(opts); got != text {
		t.Fatalf("output %q, want the input unchanged", got)
	}
}

// TestCommentsSurviveEndToEnd is the same guarantee at the pipeline level: a
// comment next to prose is still emitted byte for byte.
func TestCommentsSurviveEndToEnd(t *testing.T) {
	text := "we compute the sum\n# keep this line\nx = 5\n"
	src := core.NewSource("c.enx", []byte(text))
	opts := options(t, "ruby")
	res, err := pipeline.Run(src, lang.New(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := res.Output(opts)
	if !strings.Contains(got, "# keep this line") {
		t.Fatalf("comment was deleted: %q", got)
	}
	if strings.Contains(got, "compute") {
		t.Fatalf("prose survived: %q", got)
	}
	if !strings.Contains(got, "x = 5") {
		t.Fatalf("code was lost: %q", got)
	}
}

func TestProseOnlyFileEmitsNothing(t *testing.T) {
	for _, lg := range languages {
		src := core.NewSource("x.enx", []byte("this file is only a note to the reader, about the products\n"))
		opts := options(t, lg.name)
		res, err := pipeline.Run(src, lang.New(), opts)
		if err != nil {
			t.Fatalf("%s: Run: %v", lg.name, err)
		}
		if strings.TrimSpace(res.Output(opts)) != "" {
			t.Fatalf("%s: expected empty output, got %q", lg.name, res.Output(opts))
		}
	}
}

// TestKnownLimitationCapitalInProse records a limitation rather than asserting
// something desirable, so that the behaviour cannot drift unnoticed.
//
// A lone capitalised word is a valid Ruby constant, so when one stands alone in
// prose the grammar can keep it and no lexical evidence argues against it. The
// only way to call it prose would be to rule out a bare constant, which real
// code uses. Lower case prose is unaffected, and Perl is unaffected because
// strict subs refuses bare capitals outright.
//
// If this test fails, the limitation has been fixed: delete the test and note
// the change in SPEC.md.
func TestKnownLimitationCapitalInProse(t *testing.T) {
	opts := options(t, "ruby")
	src := core.NewSource("x.enx", []byte("this file is only a note about SKUs\n"))
	res, err := pipeline.Run(src, lang.New(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := strings.TrimSpace(res.Output(opts)); got != "SKUs" {
		t.Fatalf("limitation changed: got %q; if this is a fix, delete this test and update SPEC.md", got)
	}
}
