# TODO — enx

`enx` removes English prose from a source file in which English and code are
freely interleaved, and emits the code alone, unchanged. The source is one
stream with no delimiters. Every token is labelled code, English or trivia; the
code subsequence must parse under the host grammar, while an English
word-frequency lexicon prices each token, and the cheapest labelling wins.

Design, measured numbers and unsupported constructs are in **SPEC.md**. Review
fixes, tests and examples are in `git log`; this file is the remaining plan.

## Pending

- **Source mapping for imports.** Imported bodies are textually prepended, so a
  diagnostic inside an import reports the importing file's line numbers. A
  small source map would let `Diagnostic.File` and `Span` point at the original
  file and line.
- **A labelled corpus.** Ground truth for which tokens are prose, so the cost
  constants can be tuned against a measurement rather than a handful of files.
- **Per-block scope tracking for anaphora.** `ScopeTracker` exists but every
  language currently reports one scope for the whole file.
- **Let `Program` weigh more than one statement candidate.** Statement forms are
  chosen on their own cost, so a cheaper prefix can beat the whole construct.
- **Wider real-world coverage.** SPEC section 15 lists the known unsupported
  Ruby, Perl and C constructs.

## Live risks

- C declarator ambiguity produces genuine ties; longest-parse may not be the
  right tie-break there.
- The English lexicon tail contains corpus artefacts.
- Perl has no local parser, so `perl -c` and execution are its only oracles.
- A lone capitalised word in Ruby prose may be kept as a constant.
- The C grammar is the shallowest of the three; declarators and the preprocessor
  are modelled only to the depth the corpus needs.
