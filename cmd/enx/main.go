// Command enx removes English prose from a source file in which English and
// code are freely interleaved, and emits the code alone.
//
// Usage:
//
//	enx label [flags] file     show how each token was decided
//	enx check [flags] file     report diagnostics only
//	enx mix   [flags] file     emit the program with English deleted
//	enx run   [flags] file     emit and execute the program
//	enx langs                  list available host languages
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"enx/internal/codeswitch"
	"enx/internal/core"
	"enx/internal/host"
	"enx/internal/lang"
	"enx/internal/loader"
	"enx/internal/pipeline"
	"enx/internal/registry"
)

// lexiconLoad is a variable so tests can exercise the failure path without
// corrupting the embedded table.
var lexiconLoad = codeswitch.LoadLexicon

const usage = `enx - strip English prose from a source file that mixes English and code

usage: enx <command> [flags] [file]

commands:
  label   show every token with its cost and its final label
  check   report diagnostics without emitting
  mix     emit the program with English deleted
  run     emit the program and execute it
  langs   list available host languages

flags:
  --lang NAME        host language (default ruby)
  --out FILE         write the emitted program here instead of stdout (mix only)
  --keep-prose       (mix, run) emit dropped English as host comments
  --anaphor CHAR     claim CHAR as an anaphor marker, e.g. '!' for !-1
  --strict           (check) treat warnings as errors
  --margin N         (check) cost margin below which a decision is ambiguous
  --provisional      (mix, run) allow emission without a grammar machine (unreliable)
  --log-level LEVEL  off, error, warn, info, debug (default warn)
  --log-json         structured log output
  -h, --help         this message

Flags must precede the file name.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	command := args[0]
	rest := args[1:]

	if command == "-h" || command == "--help" || command == "help" {
		fmt.Fprint(stdout, usage)
		return 0
	}

	reg := newRegistry()

	switch command {
	case "langs":
		return cmdLangs(reg, stdout)
	case "label":
		return cmdLabel(reg, rest, stdout, stderr)
	case "check":
		return cmdCheck(reg, rest, stdout, stderr)
	case "mix":
		return cmdMix(reg, rest, stdout, stderr)
	case "run":
		return cmdRun(reg, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "enx: unknown command %q\n\n%s", command, usage)
		return 2
	}
}

// newRegistry builds the service locator. This is the only place that knows
// which languages exist.
func newRegistry() *registry.Registry {
	return lang.New()
}

// common holds flags shared by every file-based command.
type common struct {
	lang        string
	keepProse   bool
	provisional bool
	anaphor     string
	out         string
	logLevel    string
	logJSON     bool
	fs          *flag.FlagSet
}

func newCommon(name string) *common {
	c := &common{fs: flag.NewFlagSet(name, flag.ContinueOnError)}
	c.fs.StringVar(&c.lang, "lang", "ruby", "host language")
	c.fs.BoolVar(&c.keepProse, "keep-prose", false, "emit dropped English as host comments")
	c.fs.BoolVar(&c.provisional, "provisional", false, "allow emission without a grammar machine")
	c.fs.StringVar(&c.anaphor, "anaphor", "", "claim this character as an anaphor marker, e.g. '!': !-1 is the most recent assignment")
	c.fs.StringVar(&c.out, "out", "", "write the emitted program here instead of stdout")
	c.fs.StringVar(&c.logLevel, "log-level", "warn", "off, error, warn, info, debug")
	c.fs.BoolVar(&c.logJSON, "log-json", false, "structured log output")
	return c
}

// write puts the emitted program on stdout, or into a file when --out was given.
func (c *common) write(text string, stdout, stderr io.Writer) int {
	if c.out == "" {
		if _, err := io.WriteString(stdout, text); err != nil {
			fmt.Fprintf(stderr, "enx: %v\n", err)
			return 1
		}
		return 0
	}
	if err := os.WriteFile(c.out, []byte(text), 0o644); err != nil {
		fmt.Fprintf(stderr, "enx: %v\n", err)
		return 1
	}
	return 0
}

func (c *common) logger(stderr io.Writer) (*slog.Logger, error) {
	level, err := core.ParseLevel(c.logLevel)
	if err != nil {
		return nil, err
	}
	return core.NewLogger(core.LogConfig{Level: level, JSON: c.logJSON, Writer: stderr}), nil
}

func (c *common) options(logger *slog.Logger) pipeline.Options {
	return pipeline.Options{
		Language:         c.lang,
		Logger:           logger,
		KeepProse:        c.keepProse,
		AllowProvisional: c.provisional,
		AnaphorMarker:    c.anaphorMarker(),
	}
}

// anaphorMarker is in leaf.go.

// anaphorError returns a message when --anaphor was given but rejected.
func (c *common) anaphorError() string {
	if c.anaphor == "" || c.anaphorMarker() != 0 {
		return ""
	}
	return fmt.Sprintf("--anaphor %q is not usable: it must be a single ASCII character other than a space or #", c.anaphor)
}

// flagConflict is in leaf.go.

// prepare parses flags and reads the input file.
func (c *common) prepare(args []string, stderr io.Writer) (*core.Source, *registry.Registry, *slog.Logger, int, bool) {
	c.fs.SetOutput(stderr)
	if err := c.fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, nil, nil, 0, false
		}
		return nil, nil, nil, 2, false
	}
	if c.fs.NArg() != 1 {
		fmt.Fprintf(stderr, "enx %s: expected exactly one file argument\n", c.fs.Name())
		return nil, nil, nil, 2, false
	}
	if msg := c.flagConflict(); msg != "" {
		fmt.Fprintf(stderr, "enx %s: %s\n", c.fs.Name(), msg)
		return nil, nil, nil, 2, false
	}

	logger, err := c.logger(stderr)
	if err != nil {
		fmt.Fprintf(stderr, "enx: %v\n", err)
		return nil, nil, nil, 2, false
	}

	if msg := c.anaphorError(); msg != "" {
		fmt.Fprintf(stderr, "enx: %s\n", msg)
		return nil, nil, nil, 2, false
	}

	name := c.fs.Arg(0)
	src, err := loader.Load(name)
	if err != nil {
		fmt.Fprintf(stderr, "enx: %v\n", err)
		return nil, nil, nil, 1, false
	}

	return src, newRegistry(), logger, 0, true
}

// prepareOptions builds pipeline options and loads the embedded English model.
//
// A missing lexicon is fatal rather than a degraded mode: without it every
// word is code, which is exactly the wrong answer and one the tool must not
// pretend is a result.
func (c *common) prepareOptions(stderr io.Writer, logger *slog.Logger) (pipeline.Options, bool) {
	opts := c.options(logger)
	lex, err := lexiconLoad()
	if err != nil {
		fmt.Fprintf(stderr, "enx: English lexicon unavailable: %v\n", err)
		return opts, false
	}
	opts.Lexicon = lex
	return opts, true
}

func cmdLangs(reg *registry.Registry, stdout io.Writer) int {
	for _, name := range reg.Languages() {
		spec, _ := reg.Lookup(name)
		state := "inspect only"
		if spec.Ready() {
			state = "ready"
		}
		runner := strings.Join(spec.Interpreter, " ")
		if len(spec.Compile) > 0 {
			runner = strings.Join(spec.Compile, " ")
		}
		fmt.Fprintf(stdout, "%-8s %-13s ext=%-5s comment=%-3q runner=%s\n",
			name, state, spec.Extension, spec.CommentPrefix, runner)
	}
	return 0
}

func cmdLabel(reg *registry.Registry, args []string, stdout, stderr io.Writer) int {
	c := newCommon("label")
	src, reg, logger, code, ok := c.prepare(args, stderr)
	if !ok {
		return code
	}

	opts, ok := c.prepareOptions(stderr, logger)
	if !ok {
		return 1
	}
	opts.AllowProvisional = true

	res, err := pipeline.Run(src, reg, opts)
	if err != nil {
		fmt.Fprintf(stderr, "enx: %v\n", err)
		return 1
	}
	reportDiagnostics(stderr, res.Diags)

	// Only the interesting tokens are shown: trivia would drown the output.
	fmt.Fprintf(stdout, "# %s  %s  %d tokens  mode=%s\n", src.Name, res.Spec.Language, len(res.Tokens), res.Labelling.Mode)
	fmt.Fprintf(stdout, "%6s %-6s %-8s %9s %9s  %-8s %s\n",
		"line", "kind", "text", "code$", "english$", "label", "reason")

	for _, d := range res.Labelling.Decisions {
		if d.Token.IsTrivia() {
			continue
		}
		text := d.Token.Text
		if len(text) > 24 {
			text = text[:21] + "..."
		}
		reason := d.Reason
		if d.Token.Kind == host.Anaphor && res.Anaphora != nil {
			if b, ok := res.Anaphora.Resolved[d.Index]; ok {
				reason = "resolved to " + b.Name
			} else {
				reason = "unresolved: no matching earlier assignment"
			}
		}
		fmt.Fprintf(stdout, "%6d %-6s %-8q %9s %9s  %-8s %s\n",
			d.Token.Span.StartPos.Line,
			d.Token.Kind.String(),
			text,
			formatCost(d.CodeCost),
			formatCost(d.EnglishCost),
			d.Label.String(),
			reason,
		)
	}

	toksCode, toksEnglish, toksTrivia := res.Labelling.Counts()
	fmt.Fprintf(stdout, "# code=%d english=%d trivia=%d\n", toksCode, toksEnglish, toksTrivia)
	fmt.Fprintf(stdout, "# code cost=%.3f english cost=%.3f\n",
		res.Labelling.CodeCost, res.Labelling.EnglishCost)
	return 0
}

// formatCost and reportDiagnostics are in leaf.go.

func cmdCheck(reg *registry.Registry, args []string, stdout, stderr io.Writer) int {
	c := newCommon("check")
	strict := c.fs.Bool("strict", false, "treat warnings as errors")
	margin := c.fs.Float64("margin", codeswitch.DefaultMargin, "cost margin below which a decision is ambiguous")
	src, reg, logger, code, ok := c.prepare(args, stderr)
	if !ok {
		return code
	}

	opts, ok := c.prepareOptions(stderr, logger)
	if !ok {
		return 1
	}
	opts.AllowProvisional = true

	res, err := pipeline.Run(src, reg, opts)
	if err != nil {
		fmt.Fprintf(stderr, "enx: %v\n", err)
		return 1
	}

	sink := &core.Sink{}
	for _, d := range res.Diags {
		sink.Add(d)
	}

	// Report decisions that were close rather than silently trusting them.
	ambiguous, truncated := res.Ambiguities(opts, *margin)
	for _, a := range ambiguous {
		text := strings.Join(strings.Fields(a.Text), " ")
		if len(text) > 40 {
			text = text[:37] + "..."
		}
		msg := fmt.Sprintf("%q is %s at %.3f but %s at %.3f",
			text, a.Chosen, a.ChosenCost, a.Alternative(), a.AlternativeCost)
		sink.ReportHint(core.SevWarning, "ambiguous-span", src, a.Span, msg,
			"say which you meant, or lower --margin to report fewer of these")
	}
	if res.Anaphora != nil {
		for _, i := range res.Anaphora.SortedUnresolved() {
			tok := res.Tokens[i]
			sink.ReportHint(core.SevWarning, "unresolved-anaphor", src, tok.Span,
				fmt.Sprintf("%q names no earlier assignment", tok.Text),
				"check the ordinal or type filter, or write the name directly")
		}
	}
	if truncated {
		sink.Warnf("analysis-truncated", src, core.Span{},
			"only the first %d regions were tested", codeswitch.MaxAmbiguityProbes)
	}

	for _, d := range sink.Diagnostics() {
		fmt.Fprintln(stderr, d)
	}

	toksCode, toksEnglish, _ := res.Labelling.Counts()
	fmt.Fprintf(stdout, "%s: %d tokens, code=%d english=%d, ambiguous=%d\n",
		src.Name, len(res.Tokens), toksCode, toksEnglish, len(ambiguous))

	if !res.Spec.Ready() {
		fmt.Fprintf(stderr, "enx: %s is inspect-only: %v\n", res.Spec.Language, pipeline.ErrNoGrammar)
		return 1
	}
	if sink.HasErrors() {
		return 1
	}
	if *strict && sink.HasWarnings() {
		return 1
	}
	return 0
}

func cmdMix(reg *registry.Registry, args []string, stdout, stderr io.Writer) int {
	c := newCommon("mix")
	src, reg, logger, code, ok := c.prepare(args, stderr)
	if !ok {
		return code
	}

	opts, ok := c.prepareOptions(stderr, logger)
	if !ok {
		return 1
	}

	res, err := pipeline.Run(src, reg, opts)
	if err != nil {
		fmt.Fprintf(stderr, "enx: %v\n", err)
		return 1
	}
	reportDiagnostics(stderr, res.Diags)
	return c.write(res.Output(opts), stdout, stderr)
}

func cmdRun(reg *registry.Registry, args []string, stdout, stderr io.Writer) int {
	c := newCommon("run")
	src, reg, logger, code, ok := c.prepare(args, stderr)
	if !ok {
		return code
	}

	opts, ok := c.prepareOptions(stderr, logger)
	if !ok {
		return 1
	}

	res, err := pipeline.Run(src, reg, opts)
	if err != nil {
		fmt.Fprintf(stderr, "enx: %v\n", err)
		return 1
	}
	reportDiagnostics(stderr, res.Diags)
	if len(res.Spec.Interpreter) == 0 && len(res.Spec.Compile) == 0 {
		fmt.Fprintf(stderr, "enx: nothing configured to run %s\n", res.Spec.Language)
		return 1
	}

	dir, err := os.MkdirTemp("", "enx-")
	if err != nil {
		fmt.Fprintf(stderr, "enx: %v\n", err)
		return 1
	}
	defer os.RemoveAll(dir)

	out := filepath.Join(dir, filepath.Base(src.Name)+res.Spec.Extension)
	if err := os.WriteFile(out, []byte(res.Output(opts)), 0o600); err != nil {
		fmt.Fprintf(stderr, "enx: %v\n", err)
		return 1
	}

	// A compiled language needs a build step first. The compiled executable
	// takes no arguments; an interpreter takes the emitted file.
	var program, runArgs []string
	if len(res.Spec.Compile) > 0 {
		exe := filepath.Join(dir, "enxprog")
		if runtime.GOOS == "windows" {
			exe += ".exe"
		}
		build := exec.Command(res.Spec.Compile[0], substitutePaths(res.Spec.Compile[1:], out, exe)...)
		build.Stdout = stdout
		build.Stderr = stderr
		if err := build.Run(); err != nil {
			fmt.Fprintf(stderr, "enx: building %s failed: %v\n", res.Spec.Language, err)
			return 1
		}
		program = []string{exe}
	} else {
		program = res.Spec.Interpreter
		runArgs = append(append([]string(nil), program[1:]...), out)
	}

	cmd := exec.Command(program[0], runArgs...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Dir = filepath.Dir(src.Name)
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(stderr, "enx: %v\n", err)
		return 1
	}
	return 0
}

// substitutePaths is in leaf.go.
