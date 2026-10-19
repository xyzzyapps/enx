package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"enx/internal/codeswitch"
)

func invoke(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// example returns the path of a Ruby showcase example.
func example(name string) string {
	return exampleLang("ruby", name)
}

func exampleLang(lang, name string) string {
	return filepath.Join("..", "..", "examples", "showcase", lang, name)
}

// requireTool skips a test when the interpreter or compiler it needs is not on
// PATH. The pipeline oracle tests already behave this way; the CLI tests must
// too, or the suite fails on machines that only build and inspect.
func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s is not on PATH", name)
	}
}

func TestNoArgumentsIsUsageError(t *testing.T) {
	code, _, stderr := invoke(t)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "usage:") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestUnknownCommand(t *testing.T) {
	code, _, stderr := invoke(t, "frobnicate")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestHelp(t *testing.T) {
	code, stdout, _ := invoke(t, "--help")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{"label", "check", "mix", "run", "langs"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help does not mention %q", want)
		}
	}
}

func TestLangs(t *testing.T) {
	code, stdout, _ := invoke(t, "langs")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{"ruby", "perl", "ready"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("langs output %q does not mention %q", stdout, want)
		}
	}
}

func TestMixPerlMatchesExpected(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "..", "examples", "showcase", "perl", "shop.enx.expected"))
	if err != nil {
		t.Fatalf("read expectation: %v", err)
	}
	code, stdout, stderr := invoke(t, "mix", "--lang", "perl", exampleLang("perl", "shop.enx"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if stdout != string(want) {
		t.Fatalf("emission differs:\n--- got ---\n%s\n--- want ---\n%s", stdout, want)
	}
}

func TestRunExecutesPerl(t *testing.T) {
	requireTool(t, "perl")
	code, stdout, stderr := invoke(t, "run", "--lang", "perl", exampleLang("perl", "shop.enx"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "Kitchen items: 1") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestMixCMatchesExpected(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "..", "examples", "showcase", "c", "shop.enx.expected"))
	if err != nil {
		t.Fatalf("read expectation: %v", err)
	}
	code, stdout, stderr := invoke(t, "mix", "--lang", "c", exampleLang("c", "shop.enx"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if stdout != string(want) {
		t.Fatalf("emission differs:\n--- got ---\n%s\n--- want ---\n%s", stdout, want)
	}
}

// TestRunCompilesC exercises the build step: a compiled language has no
// interpreter, so run must compile the emitted file first.
func TestRunCompilesC(t *testing.T) {
	requireTool(t, "gcc")
	code, stdout, stderr := invoke(t, "run", "--lang", "c", exampleLang("c", "shop.enx"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "total: $34.48") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestMixMatchesExpected(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "..", "examples", "showcase", "ruby", "greeting.enx.expected"))
	if err != nil {
		t.Fatalf("read expectation: %v", err)
	}
	code, stdout, stderr := invoke(t, "mix", example("greeting.enx"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if stdout != string(want) {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

func TestMixOutFlagWritesFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "greeting.rb")
	code, stdout, stderr := invoke(t, "mix", "--out", out, example("greeting.enx"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("expected nothing on stdout, got %q", stdout)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(data), "puts answer") {
		t.Fatalf("file = %q", data)
	}
}

func TestMixLangFlag(t *testing.T) {
	code, _, stderr := invoke(t, "mix", "--lang", "cobol", example("greeting.enx"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "unknown language") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestLabelShowsGrammarMode(t *testing.T) {
	code, stdout, stderr := invoke(t, "label", example("greeting.enx"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "mode=grammar") {
		t.Fatalf("stdout = %q", stdout)
	}
	if !strings.Contains(stdout, "identifier") || !strings.Contains(stdout, "english") {
		t.Fatalf("labelling is not shown:\n%s", stdout)
	}
}

func TestCheckReportsCounts(t *testing.T) {
	code, stdout, stderr := invoke(t, "check", example("greeting.enx"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "tokens") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestRunExecutesTheProgram(t *testing.T) {
	requireTool(t, "ruby")
	code, stdout, stderr := invoke(t, "run", example("greeting.enx"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "10" {
		t.Fatalf("stdout = %q, want 10", stdout)
	}
}

func TestMissingFile(t *testing.T) {
	code, _, stderr := invoke(t, "mix", example("does-not-exist.enx"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if stderr == "" {
		t.Fatal("expected an error message")
	}
}

// TestInvalidAnaphorMarkerIsRejected covers the CLI guard: a marker the host
// already uses must not be accepted.
func TestInvalidAnaphorMarkerIsRejected(t *testing.T) {
	for _, marker := range []string{"#", "  ", "ab"} {
		code, _, stderr := invoke(t, "mix", "--anaphor", marker, example("greeting.enx"))
		if code != 2 {
			t.Errorf("--anaphor %q: exit = %d, want 2", marker, code)
		}
		if !strings.Contains(stderr, "--anaphor") {
			t.Errorf("--anaphor %q: stderr = %q", marker, stderr)
		}
	}
}

func TestAnaphorFlagIsAccepted(t *testing.T) {
	code, stdout, stderr := invoke(t, "mix", "--anaphor", "!", example("greeting.enx"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "puts answer") {
		t.Fatalf("stdout = %q", stdout)
	}
}

// TestRunWithAnaphora executes a program that uses the feature.
func TestRunWithAnaphora(t *testing.T) {
	requireTool(t, "ruby")
	dir := t.TempDir()
	path := filepath.Join(dir, "a.enx")
	text := "stock = 10\nmugs = stock - 3\ntotal = !-1 + !-2\nputs total\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	code, stdout, stderr := invoke(t, "run", "--anaphor", "!", path)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "17" {
		t.Fatalf("stdout = %q, want 17", stdout)
	}
}

func TestKeepProseFlag(t *testing.T) {
	code, stdout, stderr := invoke(t, "mix", "--keep-prose", example("greeting.enx"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "#") {
		t.Fatalf("expected comments, got %q", stdout)
	}
	if !strings.Contains(stdout, "puts answer") {
		t.Fatalf("expected code, got %q", stdout)
	}
}

// TestHelpFlagReturnsZero pins the conventional exit code for -h.
func TestHelpFlagReturnsZero(t *testing.T) {
	code, _, stderr := invoke(t, "mix", "-h")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stderr, "Usage") {
		t.Fatalf("expected usage text, got %q", stderr)
	}
}

// TestInvalidFlagGoesToStderr checks that flag errors reach the same writer the
// command was given, which is what makes run() testable.
func TestInvalidFlagGoesToStderr(t *testing.T) {
	code, _, stderr := invoke(t, "mix", "--definitely-not-a-flag")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "flag provided but not defined") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestLabelHeaderHasReason(t *testing.T) {
	code, stdout, stderr := invoke(t, "label", example("greeting.enx"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "reason") {
		t.Fatalf("label header is missing the reason column:\n%s", stdout)
	}
}

// TestStrictFailsWarnings covers the CI contract: strict mode turns the
// no-code-kept warning into a failed check.
func TestStrictFailsWarnings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prose.enx")
	if err := os.WriteFile(path, []byte("this file is only a note to the reader\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	code, _, stderr := invoke(t, "check", "--strict", path)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "no-code-kept") {
		t.Fatalf("stderr = %q", stderr)
	}
}

// TestIgnoredFlagsAreRejected covers the shared flag set: a flag with no
// meaning for the chosen command must not be silently ignored.
func TestIgnoredFlagsAreRejected(t *testing.T) {
	cases := [][]string{
		{"check", "--out", "out.rb", example("greeting.enx")},
		{"run", "--out", "out.rb", example("greeting.enx")},
		{"label", "--keep-prose", example("greeting.enx")},
		{"check", "--provisional", example("greeting.enx")},
	}
	for _, args := range cases {
		code, _, stderr := invoke(t, args...)
		if code != 2 {
			t.Errorf("%v: exit = %d, want 2", args, code)
		}
		if !strings.Contains(stderr, "only valid") {
			t.Errorf("%v: stderr = %q", args, stderr)
		}
	}
}

// TestLabelShowsAnaphorReferent implements SPEC section 18.6: label says what
// each marker resolved to.
func TestLabelShowsAnaphorReferent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.enx")
	if err := os.WriteFile(path, []byte("x = 1\ny = !-1\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	code, stdout, stderr := invoke(t, "label", "--anaphor", "!", path)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "resolved to x") {
		t.Fatalf("label did not show the referent:\n%s", stdout)
	}
}

// TestCheckReportsUnresolvedAnaphor implements the other half of SPEC 18.6: an
// anaphor that names nothing is a warning, not a silent failure.
func TestCheckReportsUnresolvedAnaphor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.enx")
	if err := os.WriteFile(path, []byte("x = 1\ny = !-7\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	code, stdout, stderr := invoke(t, "check", "--anaphor", "!", path)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "tokens") {
		t.Fatalf("missing counts: %q", stdout)
	}
	if !strings.Contains(stderr, "unresolved-anaphor") {
		t.Fatalf("unresolved anaphor was not reported:\n%s", stderr)
	}
}

// TestLexiconFailureAborts checks the safety property: without the English
// model, every word is code, so the command must fail instead of emitting a
// plausible-looking wrong answer.
func TestLexiconFailureAborts(t *testing.T) {
	old := lexiconLoad
	lexiconLoad = func() (*codeswitch.Lexicon, error) { return nil, errors.New("boom") }
	defer func() { lexiconLoad = old }()

	code, _, stderr := invoke(t, "mix", example("greeting.enx"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "lexicon unavailable") {
		t.Fatalf("stderr = %q", stderr)
	}
}

// TestRunCompiledProgramGetsNoExtraArguments covers the compiled path: the
// source file must not be passed as argv[1] to the executable.
func TestRunCompiledProgramGetsNoExtraArguments(t *testing.T) {
	requireTool(t, "gcc")
	dir := t.TempDir()
	path := filepath.Join(dir, "argc.enx")
	text := "#include <stdio.h>\n" +
		"int main(int argc, char **argv) {\n" +
		"    printf(\"%d\\n\", argc);\n" +
		"    return 0;\n" +
		"}\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	code, stdout, stderr := invoke(t, "run", "--lang", "c", path)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "1" {
		t.Fatalf("argc = %q, want 1", strings.TrimSpace(stdout))
	}
}
