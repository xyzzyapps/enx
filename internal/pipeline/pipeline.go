// Package pipeline wires the stages together: tokenize, label, group, emit.
//
// It knows nothing about any particular language; a language arrives as a
// host.Spec from the registry.
package pipeline

import (
	"errors"
	"fmt"
	"log/slog"

	"enx/internal/anaphor"
	"enx/internal/codeswitch"
	"enx/internal/core"
	"enx/internal/host"
	"enx/internal/registry"
)

// ErrNoGrammar is returned when a language has no grammar machine yet and the
// caller asked for emission rather than inspection.
var ErrNoGrammar = errors.New("grammar machine not implemented for this language yet")

// ErrNoParse is returned when a language has a grammar machine but no labelling
// of the input has a valid code subsequence, which means the input uses syntax
// the grammar does not cover yet.
var ErrNoParse = errors.New("no valid code subsequence exists for this input")

// DropWarningRatio is the fraction of non-trivia tokens that may be labelled
// English before enx warns that it may have deleted code. Ordinary mixed prose
// usually stays well below this; a file of garbage sits at one.
const DropWarningRatio = 0.8

// Options configures a run.
type Options struct {
	// Language selects the host language.
	Language string
	// Logger receives structured trace output. A nil logger discards.
	Logger *slog.Logger
	// Lexicon is the English word-frequency model. A nil lexicon disables
	// English detection, which is only useful in tests.
	Lexicon *codeswitch.Lexicon
	// KeepProse renders dropped English as host comments instead of deleting
	// it. It is a debugging aid and does not change the labelling.
	KeepProse bool
	// AllowProvisional permits emission using the lexicon-only labelling, which
	// is known to be wrong in documented cases. It exists so the pipeline can
	// be exercised before the grammar decoder lands.
	AllowProvisional bool
	// AnaphorMarker claims a character as an anaphor marker, so that "!-1"
	// names the most recent assignment. Zero means anaphora is off, which is
	// the default: the marker collides with real syntax somewhere in every
	// host. See SPEC section 18.
	AnaphorMarker byte
}

// Result holds everything a command needs to render its output.
type Result struct {
	Source    *core.Source
	Spec      host.Spec
	Tokens    []host.Token
	Labelling *codeswitch.Labelling
	// Path is the decoded labelling, or nil when the language has no grammar
	// machine. It carries the statement boundaries and is what the ambiguity
	// analysis re-decodes against.
	Path  *host.Path
	Plan  *codeswitch.Plan
	Diags []core.Diagnostic
	// Anaphora is the resolution of every anaphor marker, or nil when the
	// feature is off.
	Anaphora *anaphor.Result
}

// Ambiguities reports English regions that could equally have been code, at a
// cost within margin of the labelling that was chosen. The second result says
// whether the probe cap was reached, so a caller can report a partial analysis
// rather than claim a clean one.
func (r *Result) Ambiguities(opts Options, margin float64) ([]codeswitch.Ambiguity, bool) {
	return codeswitch.FindAmbiguities(
		r.Source, r.Spec.Machine, opts.Lexicon, r.Spec.Vocabulary, r.Tokens, r.Path, margin)
}

// Output renders the emitted program according to opts.
func (r *Result) Output(opts Options) string {
	if r.Plan == nil {
		return ""
	}
	if opts.KeepProse {
		return r.Plan.TextWithProse(r.Spec.CommentPrefix)
	}
	return r.Plan.Text()
}

// Run executes the pipeline for src.
//
// Run inspects the language before touching the input, so a caller asking for
// emission from a language without a grammar machine gets ErrNoGrammar rather
// than a plausible-looking wrong answer.
func Run(src *core.Source, reg *registry.Registry, opts Options) (*Result, error) {
	log := loggerOf(opts)

	spec, ok := reg.Lookup(opts.Language)
	if !ok {
		return nil, fmt.Errorf("unknown language %q (available: %v)", opts.Language, reg.SortedLanguages())
	}
	if !spec.Ready() && !opts.AllowProvisional {
		return nil, fmt.Errorf("%w: %s", ErrNoGrammar, spec.Language)
	}

	log.Debug("pipeline: start", "file", src.Name, "language", spec.Language, "bytes", src.Len())

	toks, lexDiags := lexFor(spec, src, opts.AnaphorMarker)
	log.Debug("pipeline: lexed", "tokens", len(toks), "diagnostics", len(lexDiags))

	lab, path, err := decide(spec, opts, toks)
	if err != nil {
		return nil, err
	}
	code, english, trivia := lab.Counts()
	log.Debug("pipeline: labelled",
		"mode", lab.Mode, "code", code, "english", english, "trivia", trivia)

	var sep codeswitch.Separator = codeswitch.LexicalSeparator{}
	if path != nil {
		sep = codeswitch.NewMachineSeparator(path, src, toks)
	}

	plan := &codeswitch.Plan{
		Source: src,
		Runs:   codeswitch.Split(toks, lab.Labels(), sep),
		Tokens: toks,
	}

	var resolved *anaphor.Result
	if opts.AnaphorMarker != 0 {
		resolved = resolveAnaphora(spec, toks, lab.Labels())
		plan.Substitute = resolved.Substitute
		log.Debug("pipeline: anaphora resolved",
			"bindings", len(resolved.Bindings),
			"resolved", len(resolved.Resolved),
			"unresolved", len(resolved.Unresolved))
	}

	diags := append([]core.Diagnostic{}, lexDiags...)
	diags = append(diags, dropDiagnostics(src, lab, path)...)

	return &Result{
		Source:    src,
		Spec:      spec,
		Tokens:    toks,
		Labelling: lab,
		Path:      path,
		Plan:      plan,
		Diags:     diags,
		Anaphora:  resolved,
	}, nil
}

// dropDiagnostics warns when the decoder removed almost everything. It cannot
// distinguish a prose-only file from one whose syntax the grammar does not
// cover, which is exactly why the warning exists: silence would otherwise make
// both look like success.
func dropDiagnostics(src *core.Source, lab *codeswitch.Labelling, path *host.Path) []core.Diagnostic {
	code, english, _ := lab.Counts()
	nonTrivia := code + english
	if nonTrivia == 0 {
		return nil
	}

	sink := &core.Sink{}
	span := src.Span(0, src.Len())
	if code == 0 {
		sink.Warnf("no-code-kept", src, span,
			"no code tokens were kept; check whether the file is prose or unsupported syntax")
		return sink.Diagnostics()
	}
	if float64(english)/float64(nonTrivia) >= DropWarningRatio {
		sink.Warnf("mostly-dropped", src, span,
			"dropped %d of %d non-trivia tokens; run enx label to inspect the decisions",
			english, nonTrivia)
	}
	if path != nil && len(path.SeparatorAfter) == 0 {
		sink.Warnf("no-complete-statement", src, span,
			"the kept code has no complete statement boundary; the grammar may not cover this input")
	}
	return sink.Diagnostics()
}

// lexFor tokenizes src, claiming the anaphor marker when the lexer supports it.
//
// Claiming the marker is opt-in and happens here, never inside a lexer's default
// behaviour: "*N" already means a splat in Ruby and a typeglob in Perl, so a
// lexer that claimed it unasked would change working code.
func lexFor(spec host.Spec, src *core.Source, marker byte) ([]host.Token, []core.Diagnostic) {
	if marker == 0 {
		return spec.Lexer.Lex(src)
	}
	if al, ok := spec.Lexer.(host.AnaphorLexer); ok {
		return al.LexWithAnaphora(src, marker)
	}
	return spec.Lexer.Lex(src)
}

// resolveAnaphora resolves every anaphor marker against the assignments before
// it.
func resolveAnaphora(spec host.Spec, toks []host.Token, labels []codeswitch.Label) *anaphor.Result {
	kept := make([]bool, len(toks))
	for i, l := range labels {
		kept[i] = l != codeswitch.English
	}
	return anaphor.Resolve(toks, kept, spec.Scopes, spec.Types)
}

// decide produces the labelling for toks.
//
// With a grammar machine, the machine searches for the lowest-cost labelling
// whose code subsequence parses, and its statement boundaries drive the
// emitter. Without one, the lexicon-only classifier stands in; that is
// inspection-grade, not emission-grade.
func decide(spec host.Spec, opts Options, toks []host.Token) (*codeswitch.Labelling, *host.Path, error) {
	if spec.Machine != nil {
		lab, path, ok := codeswitch.Decode(spec.Machine, opts.Lexicon, spec.Vocabulary, toks)
		if !ok {
			return nil, nil, fmt.Errorf("%w: %s", ErrNoParse, spec.Language)
		}
		return lab, path, nil
	}
	return codeswitch.ClassifyLexical(opts.Lexicon, toks), nil, nil
}

func loggerOf(opts Options) *slog.Logger {
	if opts.Logger != nil {
		return opts.Logger
	}
	return core.NewLogger(core.LogConfig{})
}
