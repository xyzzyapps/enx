# enx — SPEC

Status: Ruby, Perl and C are complete end to end (M2, M3, M4), with the M5
features and a review-hardening pass on top.

## 1. What this is

`enx` takes a source file in which **English prose and code are freely
interleaved**, deletes the prose, and emits the code alone, byte for byte
unchanged.

```
input (mixed)                                    output (Ruby)
-----------------------------------------------  ---------------------------
we start with x as five, that is x = 5,          x = 5
and y as five too: y = 5                         y = 5
and their sum is a function of two arguments,    sum = lambda { |a, b| a + b }
so sum = lambda { |a, b| a + b }                 answer = sum.call(x, 5)
finally we call sum with x and five and print    puts answer
the answer: answer = sum.call(x, 5)
and then we show it: puts answer
```

There is **no new language**, **no delimiters**, and **no cross-language
translation**. The code is real Ruby, real Perl, or real C. One dialect exists
per host language; a mixed-Ruby file emits Ruby and nothing else.

## 2. The model: code-switching, not islands

This is the most important design decision in the project, and two earlier
designs got it wrong. Both are recorded so they are not re-proposed.

### 2.1 Rejected design A — a neutral language with delimiters

Transform a neutral "mixed language" into Ruby, Perl, or C through an
intermediate representation. **Rejected:** it invents a new language and
requires cross-language translation, which is not what was asked for.

### 2.2 Rejected design B — islands

Mark code with delimiters, treat everything outside as "water" to be discarded
(the classic *island grammar* / *bounded seas* approach). It works, but it
imposes a **clean separation**: the reader always knows where code begins. It
is not how bilingual speech behaves, and it was rejected on those grounds.

### 2.3 Accepted design — code-switching

The file is **one stream**. There are **no delimiters**. Every token is
labelled individually:

- `code` — belongs to the program, emitted verbatim,
- `english` — prose, deleted,
- `trivia` — spacing and comments, always preserved. Comments are also a hard
  boundary for region grouping: two English sentences separated only by a
  comment stay two regions, so the comment cannot be swallowed and deleted with
  the prose around it.

The linguistic framing is exact. The host language is the **matrix language**
(it supplies the frame); English is the **embedded** contribution. The points
where the labelling changes are **switch points**, and they are inferred, never
declared.

## 3. The invariant

By default the tool **decides but never rewrites**:

```
mixed  =  e1 ‖ c1 ‖ e2 ‖ c2 ‖ … ‖ en ‖ cn      (e = english, c = code)
output =  c1 ‖ c2 ‖ … ‖ cn                     (plus separators)
```

Consequences, all tested:

1. The emitted program is a **subsequence** of the input token stream. Nothing
   is reordered, substituted, or invented.
2. For input containing no English at all, output is **byte-identical** to
   input.
3. Every non-trivia token labelled `code` is emitted with its original bytes.

Rule 2 is the strongest practical guarantee and the cheapest to test.

### 3.1 The one declared exception: anaphora

Anaphora (section 18) is the single feature that rewrites, and it is **opt-in at
the command line**. Without `--anaphor`, the invariant above holds exactly as
written.

With it, an anaphor token is replaced by the name of a previous assignment:

```
stock = 10            stock = 10
mugs  = stock - 3     mugs  = stock - 3
total = !-1 + !-2       total = mugs + stock
```

The rewrite is deliberately the narrowest possible. The referent is **always a
bare variable name**, because an anaphor may only refer to an assignment, so
there is no expression to duplicate and no side effect to evaluate twice. That
makes the whole feature a **token substitution**: the bytes of the anaphor token
are replaced by the bytes of a name, and everything else is untouched. No
reordering, no insertion, no hoisting, no synthesised names.

Two consequences worth stating plainly:

- An anaphor can only name what the author already named. It never introduces a
  new binding, so it cannot collide with anything in the file.
- The substitute is the variable's **name**, not its value, so anaphora carries
  no evaluation-order question at all.

## 4. The objective

The agreed wording was *take the longest sequence of code as code*. That is the
right intuition, but **unit length is provably wrong** and must be weighted:

```
maximise   Σ_code  w_code(tok)  −  Σ_english  w_english(tok)
subject to the code subsequence parses under the host grammar
```

With unit weights the objective degenerates to "longest valid code
subsequence", which fails on:

```
Add the first two numbers and return what you get
```

This is plain English. It is also **valid Ruby**:

```ruby
Add(the(first(two(numbers)))) and return(what(you(get)))
```

Zero insertions are needed, so a unit-weighted objective labels all ten tokens
code and emits a valid program that does nothing the author intended. No
grammar-only method can fix this, because the ambiguity is lexical, not
structural. Hence `w_code` falls for tokens that look like code and `w_english`
falls for common English words.

Longest-parse is still used, but only as a **tie-break**: on equal cost, the
parse that consumes more tokens wins. See section 10.4.

### 4.1 Relation to known problems

This is a **grammar-constrained weighted longest common subsequence** with
insertions only — that is, **minimum-distance error-correcting parsing** (Aho &
Peterson 1972; Lyon 1974), the same machinery speech recognisers use for filler
words such as "um": optional zero-grammar insertions with a frequency-based
cost. English prose here *is* a filler model.

## 5. Architecture

```
                    cmd/enx  (CLI: label, check, mix, run, langs)
                       |
                       v
   internal/loader ── reads a file and its recursive YAML imports
        |              uses internal/frontmatter for the header
        v
   internal/pipeline ── orchestrates: lex → decode → split → plan
        |        |
        |        +--> internal/registry  (service locator: name -> host.Spec)
        |        +--> internal/lang      (registers concrete languages)
        |
        v
   internal/host            Kind, Token, Lexer, Machine, Costs, Path, Spec,
        |                   Vocabulary
        |         +--> internal/host/parse   the shared machinery
        |         +--> internal/host/ruby    Lexer, Machine, Vocabulary, adapter
        |         +--> internal/host/perl    Lexer, Machine, Vocabulary, adapter
        |         +--> internal/host/c       Lexer, Machine, Vocabulary, adapter
        |
        v
   internal/codeswitch      the decision layer
        |-- lexicon.go     embedded English word frequencies
        |-- label.go       Label, Decision, Labelling, LooksLikeCode,
        |                  calibration constants
        |-- costs.go       host.Costs: the price of keep and skip
        |-- decode.go      runs the machine, builds a Labelling
        |-- plan.go        Run grouping, Separator, Plan, rendering
        |
        v
   internal/core            Span, Source, Diagnostic, Sink, logging
```

Data flow:

```
  file ──loader/frontmatter──> Source
                                   │
  bytes ──Lexer──> []Token ──Machine.Decode(+Costs)──> host.Path
             (total)              (weighted, grammar-constrained)
                                                       │
                        Labelling + Separator ──Split──> []Run ──Plan──> text
```

### 5.1 Package responsibilities

| Package | Responsibility | Must not |
|---|---|---|
| `core` | spans, sources, diagnostics, logging | know about languages or files |
| `frontmatter` | parse the narrow YAML header and return its `imports` | resolve paths or read files |
| `loader` | recursive import expansion, deduplication, cycle detection | decode or tokenize |
| `host` | kinds, `Lexer`, `Machine`, `Costs`, `Path`, `Vocabulary`, `Spec` | implement a language |
| `host/parse` | memoised best-cost machinery, `Body`, `Program`, `Decode` | know any concrete language |
| `host/ruby`, `host/perl`, `host/c` | productions, slots, one boundary predicate | know about the pipeline |
| `codeswitch` | costs, labelling, run grouping, emission | tokenize |
| `pipeline` | wire the stages, enforce readiness | know a concrete language |
| `registry` | service locator returning `host.Spec` | construct languages |
| `lang` | the one place that lists concrete languages | be imported by languages |

### 5.2 The shared parser core

`internal/host/parse` holds the one implementation of the machinery. A host
grammar supplies three things and nothing else:

1. its productions, as `parse.Func` values,
2. a **slot** per production, so memoisation cannot collide. Sharing a slot lets
   the first production to run at a position decide the answer for the others,
   and that bug has now been made twice — once in Perl, where a failing `use`
   statement made every `my` statement fail, and once in C, where `functionDef`
   and `declOrFunc` shared a key and every function definition was rejected;
3. a **boundary predicate** saying which tokens a terminal lookup must not cross.
   Ruby's is a newline or a semicolon, because a newline ends a Ruby statement.
   Perl's and C's is a semicolon only.

That is the whole interface. Each language then has a ~135-line adapter of thin
wrappers that keep the production code readable, and the productions themselves.
Adding a language costs roughly the productions plus the adapter, which is
about a quarter of what it cost before the extraction.

### 5.3 Leaf and integrator functions

The code follows a dependency rule rather than a package-size rule:

- **Leaf function** — calls only standard-library or imported-library
  functions, plus field access on model values. It may be impure or
  nondeterministic; the only criterion is that it does not call another enx
  function or method. A leaf is readable in one place and needs no trace through
  the project.
- **Integrator** — may call leaves and other integrators. It owns state, I/O,
  ordering, process execution, logging and the grammar productions.

Leaf functions live in `leaf.go` files where practical: `frontmatter`,
`loader`, `codeswitch`, `anaphor`, `host`, `host/parse` and `cmd/enx` each have
one.
The split is about the call graph, not purity:

```text
leaf:   unquote, splitList, isLayout, LooksLikeCode, typeMatches, Combine
        formatCost, anaphorMarker, substitutePaths
calls:  parse → Split; Costs.Keep → isReceiver; Resolve → typeMatches
        pipeline.Run → Decode → Machine.Decode → productions → leaves
```

Leaves get direct table tests. Integrators get examples, fixtures and oracle
runs. When a function grows a helper, prefer extracting the helper as a leaf
instead of adding another layer of project-function calls.

## 6. Data structures

```go
// core
type Pos struct{ Line, Col int }                 // 1-based; Col counts runes
type Span struct{ Start, End int; StartPos, EndPos Pos }  // half-open bytes
type Source struct{ Name string; … }             // immutable bytes + line index
type Diagnostic struct{ Severity, Code, File, Span, Message, Hint }
type Sink struct{ … }                            // ordered diagnostics

// host
type Kind uint8   // Space, Newline, Comment, Identifier, Constant, Keyword,
                  // Operator, Punct, Int, Float, String, Symbol, Regex,
                  // Heredoc, Variable, Sigil, Preproc, Char, Label
type Token struct{ Kind Kind; Text string; Span core.Span }

type Lexer interface{ Language() string; Lex(*core.Source) ([]Token, []Diagnostic) }

type Costs interface{ Keep(i int) float64; Skip(i int) float64 }   // +∞ forbids

type Action uint8                 // KeepToken | SkipToken
type Path struct{
    Actions        []Action
    SeparatorAfter map[int]bool   // i ends a statement: a separator may follow
    Cost           float64
}

type Machine interface{ Language() string; Decode([]Token, Costs) (*Path, bool) }

type Vocabulary interface{ Knows(word string) bool }

type Spec struct{ Language, Extension, CommentPrefix string
                  Interpreter []string; Lexer Lexer; Machine Machine
                  Vocabulary Vocabulary }

// codeswitch
type Label uint8                      // Undecided, Code, English, Trivia
type Decision struct{ Index; Token; Label; CodeCost; EnglishCost; Reason }
type Labelling struct{ Mode string; Decisions []Decision; CodeCost, EnglishCost float64 }
type Run struct{ Kind RunKind; First, Last int; Span core.Span; Replacement string }
type Plan struct{ Source *core.Source; Runs []Run }
type Separator interface{ Replacement(regionText string, prev, next int) string }
```

### 6.1 Why `Machine` owns the search

An earlier interface exposed the grammar as a `Step`-based automaton with
`AtStatementBoundary` and `Accepting`, leaving the search to `codeswitch`. That
split does not work: whether a token may be *skipped* is a question only the
grammar can answer, because a cheaper-looking decision may be illegal one token
later. The grammar and the weighted search are therefore one thing, and
`Machine.Decode` takes the cost model as a parameter. The grammar still knows
nothing about English: costs arrive as a `Costs` value, and `Machine` never
sees a lexicon.

### 6.2 Lexer totality

**A `Lexer` must be total**: every byte of the input belongs to exactly one
token, in order, so `concat(token.Text) == input`. This is what makes the
byte-identical guarantee possible, and it is enforced by the per-language
`TestTotality` tests and by the lexer fuzz tests.

## 7. Cost calibration

All constants live in `internal/codeswitch` and `internal/host/ruby/machine.go`.
They are the product of empirical tuning against the test corpus.

| Constant | Value | Meaning |
|---|---|---|
| `CodeBias` | 4.0 | added to the cost of keeping a bare common word |
| `PlausibleEnglishCost` | 10.0 | skippability of a word the lexicon lacks |
| `CodeShapedEnglishCost` | 22.0 | skippability of a word that looks like an identifier |
| `UnknownEnglishCost` | 24.0 | fallback for the lexicon-only classifier |
| `FunctionWordCutoff` | 7.0 | `LogCost` below which a word is a function word |
| `FunctionWordKeep` | 28.0 | cost of calling a function word code |
| `NoiseCost` | 5.0 | cost of skipping punctuation, operators, literals |
| `StrongCodeCost` | 14.0 | cost of skipping a preprocessor line, regex, heredoc or sigil variable |
| `EscapeCost` | 1.0 | cost of skipping a `~ ... ~` span the author marked as prose |
| `HostNamePenalty` | 8.0 | added to skipping a known host name |
| `switchCost` (both) | 6.0 | charged once per maximal run of English |
| `maxIdentifierLength` | 14 | beyond this a word is assumed to be code |

Nothing is ever `ImpossibleCost` to skip. An earlier version made
identifier-shaped words unskippable, on the theory that they can only be code.
That fails on prose: "SKU" is indistinguishable from a constant, and a token
that can be neither kept nor skipped leaves the grammar with **no parse at
all**, failing the whole file. Expensive and possible beats free and impossible.
`CodeShapedEnglishCost` is that price.

```
LooksLikeCode(w)  = w has "_" or a digit, is CamelCase, is SCREAMING case,
                    or is longer than 14 characters

LogCost(w)        = −ln P(w),  P(w) = count(w) / Σcount
```

### 7.1 The contextual rules

Spelling alone is not enough; each of these rules supplies knowledge the
lexicon cannot.

1. **Function words are expensive to keep.** `Keep(w) = FunctionWordKeep` when
   `LogCost(w) < FunctionWordCutoff`. Function words — "the", "of", "and",
   "we", "it", "as" — are the one class of English word that is almost never an
   identifier in real code. This is what separates `print the answer` (prose)
   from `puts answer` (a call).
2. **Host vocabulary.** A word the host `Vocabulary` recognises is free to keep
   and costly to skip. This is what makes `puts answer` a call: `puts` is a
   Ruby method, and no amount of arithmetic over word frequencies could know
   that.
3. **A word written against what follows is code.** If the next significant
   token touches the word and is `(` or `[`, the word is a call or a subscript.
   Prose never glues a word to a bracket, and this is what keeps `sum(a, b)` a
   call in C and Perl when "sum" is also a common English word. A companion
   rule covers a name glued to a preceding `.`, which is what stops the full
   stop ending an English sentence from turning the next word into a call.
4. **A word in operand position is a variable, not a function word.** A word
   followed by a comma, an operator or a closer is at once common English and an
   ordinary variable name — `first`, `total`, `count`, and of course `a`. The
   neighbours separate the two uses: "the first of them" is prose, while
   `first, *rest = list` is code. Pricing it as code is safe even when it is
   prose, because no statement form ends at a comma or an operator.
   English punctuation is deliberately excluded from this rule: a period, colon,
   dash, question mark or exclamation mark all follow a word in prose, and
   including them made the last word of a sentence — "and print everything
   out." — cheap to keep.
5. **`a` is a function word.** The lexicon keeps only words of two letters or
   more, so the commonest word in English is supplied separately. `i` is
   deliberately excluded: lower case `i` is a loop variable far more often than
   it is prose.
6. **A token that cannot be prose is expensive to skip.** A preprocessor
   directive, a regex literal, a heredoc and a sigil variable are charged
   `StrongCodeCost`. The price has to exceed a switch cost, because keeping an
   isolated token between two paragraphs of prose ends one English run and
   starts another; at `NoiseCost` it was cheaper to throw away an isolated
   `#include <stdio.h>`, and that is exactly what happened.

### 7.2 Perl needs no lexical evidence at all

Perl inverts the problem. The grammar models **`use strict`**, under which a
bareword is a compile error, so a call needs parentheses or a leading sigil, and
only the built-in list operators (`print`, `say`, `sort`, `grep`, …) may take a
bare list. Ordinary English is therefore not parseable as code at all:

```
we set the first amount;
```

has no parentheses, so every word in it is refused by the grammar and dropped.
Perl's disambiguation comes from **syntax, not statistics**, which is the
opposite of how Ruby works and a good deal more reliable. It also means Perl
does not need the function-word rule to do any work.

The same strictness applies to bare capitals: under `strict subs` a bare
capital is only a filehandle such as `STDOUT`, or a package name already in
scope. The Perl grammar enforces that, which is what removes acronyms like
"SKU" from prose — a case Ruby cannot resolve, because there a lone capital is
a valid constant.

### 7.3 Ruby still needs the constant rule

In Ruby a capitalised name that is not at the start of a line is treated as a
constant and is free to keep. Without it, `include Comparable` is cheaper to
skip than to keep, because "comparable" is an English word and the switch cost
is paid either way. The rule is retained for Ruby and deliberately absent for
Perl, where `strict subs` has a better answer.

### 7.4 The switch cost

`switchCost` is charged once per maximal run of English, because prose comes in
phrases. Without it, a two-word run is cheaper to skip than to keep (two words
cost `2 × CodeBias` to keep but only one switch to skip), so `puts answer`
would lose its argument and become two English words.

It also protects the mirror case. A lone code word between prose — `puts Foo`,
where `Foo` is not attached to anything — is cheaper to keep than to skip,
because skipping *starts a new English run* and pays a switch. The switch cost
is what makes "inside a run, skip; outside a run, keep" fall out of the
arithmetic rather than needing a special rule.

## 8. The English lexicon

- Source: Peter Norvig's `count_1w.txt` (Google Web Trillion Word Corpus):
  333,333 entries with real counts.
- Filter: `^[a-z]+$`, length 2…24 → **333,187 words**, 5.28 MB raw.
- Stored gzipped at `data/english_freq.tsv.gz` (**2.29 MB**) and embedded with
  `go:embed`, so the binary is self-contained.
- Regenerate with `task wordlist`; the generator is `tools/gen_wordlist.pl`.
- The tail contains corpus artefacts (`gollgo`, `golgw`). Harmless for prose
  detection, but worth knowing when tuning.

## 9. Emission rules

1. An English region absorbs the layout (spaces and newlines) immediately before
   and after it, so it can be replaced as a unit. Absorbing the trailing layout
   is what lets a separator remove a newline that must not survive, and re-emit
   the indentation of the line that follows. Comments are absorbed by nothing:
   they are content and stay outside every region.
2. Code runs are copied verbatim, including their internal spacing.
3. An English region is replaced by `Separator.Replacement(regionText, prev, next)`
   with `prev` and `next` the indices of the surrounding code tokens, or `-1` at
   the file boundaries.

`MachineSeparator` implements the rule that makes emission correct:

```
if prev < 0 or next < 0                      → ""
else if regionText contains "\n"             → "\n" + trailingIndent(regionText)
else if path.SeparatorAfter[prev]            → "\n" + trailingIndent(regionText)
else                                         → " "
```

Keeping a line break that was already present is the least surprising choice:
it protects a bare `return` followed by prose on the next line, and it costs
nothing where the grammar had already decided a newline was legal.

### 9.1 Why the separator needs the grammar

Ruby and Perl treat `return` at end of line as a statement terminator:

```ruby
return   the total   x + y     # English dropped, region replaced by a space
return x + y                   # right
```

```ruby
return   the total              # region spans a newline
return                          # WRONG: returns nil
```

So a newline may be inserted only where the grammar says a statement boundary
is legal. `Path.SeparatorAfter` carries that information, and it is populated
only for the last kept token of each complete statement. A bare control keyword
such as `return`, `break` or `next` is not recorded: it may still take an
operand, so inserting a newline where the source had a space would change the
statement. A region that already contained a line break keeps one regardless,
which is what protects `return` followed by prose on the next line.
`LexicalSeparator` exists as a fallback for languages without a machine and is
known to be wrong in exactly this case.

### 9.2 Prose between two statements on one line

```
x = 5 and then some words here and y = 6
```

decodes as two statements with the prose between them replaced by a newline, so
the result is `x = 5\ny = 6`. In general the grammar decides whether a
separator is a newline or a space, which is why the language's own grammar has
to be present before `mix` will run.

## 10. The Ruby grammar machine

`internal/host/ruby/machine.go` and `expr.go`. Roughly a thousand lines of
hand-written recursive descent with an unusual twist: every production may
**skip tokens it does not need**, paying the cost model's price.

### 10.1 Design

- Every production is memoised on `(production, position)`, with an in-progress
  guard so that an accidental left recursion is refused rather than looped.
- Each result carries `kept` (token indices consumed as code) and `ends` (token
  indices that terminate a statement), so the labelling and the boundary map are
  read off the winning parse.
- `program` and `body` cross prose a token at a time, charging one switch cost
  per run. Everything else is adjacency-only.
- `body` compares "parse a statement" with "skip this token and continue" and
  memoises the choice per `(position, prevKept)` for that body. It only skips a
  word over a successful statement when the cost model says the English reading
  is cheaper, and it skips punctuation only when no statement starts there.

### 10.2 Terminal lookup is adjacency-only

An early version let a terminal search forward while skipping English. In

```
x = 5, and y as five too: y = 5
```

the `and` was then reachable from `x`, so the grammar joined `x` to `and y ...`
— a parse that is valid Ruby and completely wrong. Every terminal is now
resolved against the next significant token only. The same mistake in
miniature: `peek` skips newlines, so a line ending in `y = 5` was joined to the
`and` opening the next line; infix operators and argument lists therefore look
along **the current line only** (`peekLine`).

### 10.3 Coverage

Statements: assignment and expression statements, `return break next redo
retry`, modifier `if unless while until`, `if elsif else end`,
`unless`, `while until`, `for in`, `case when else end` and simple `case/in`
arms, `begin rescue else ensure end`, `def` (with `self.` and simple receiver
singletons, setter names, parenthesised and bare parameter lists, and endless
`def name = expr`), `class` (with superclass, `::` paths and `class << self`),
`module`.

Expressions: a precedence-climbing binary chain, ternary, assignment,
unary prefixes, `**`, postfix `.name`, `&.name`, `::Name`, `[...]`, blocks
(`{ }` and `do … end`), literals, `%w %i %q %Q %r %s` and heredocs, arrays,
hashes, symbols, `->` lambdas, `yield`, `super`, and calls with or without
parentheses.

## 11. Ambiguity policy

Two labellings of equal cost resolve by **longest parse**, which is the agreed
objective rather than a tie-break of convenience.

Close calls are **reported rather than hidden**. `enx check` re-decodes the file
with each region's opposite reading pinned, and reports the regions whose other
reading is within a margin of the one chosen. The pinning needs no grammar
support at all: `Costs` is wrapped so that pinning a token to code makes it
impossible to skip, and pinning it to prose makes it impossible to keep. If no
parse exists under the pin, the decision was forced and there is nothing to
report.

Both directions are tested, and they fail differently, so they are targeted
differently:

- **Prose that could be code.** Every English region is tested. Dropping code is
  the failure that breaks the emitted program, and prose regions are few.
- **Code that could be prose.** This is the dangerous direction, because the
  emitted program still runs and does the wrong thing. It is tested only for
  kept tokens that are words the lexicon knows, most common English first.
  Testing whole code runs instead would be useless — in every language, deleting
  a statement is usually possible, so nearly every statement would look
  ambiguous — and testing every kept word would exceed any sane budget. A kept
  common English word is precisely the shape of the risk.

`TestAmbiguityFindsCommonEnglishKeptAsCode` pins the behaviour: on the flagship
example, `sum` and `answer` are reported, at margins of 1.0 and 2.0.

Two other guarantees:

- The tool must never silently reinterpret prose as code. The mechanism that
  enforces this is the one described in section 15: unsupported syntax is
  *dropped*, which is loud in its own way — the emitted program will not work —
  and `enx label` shows exactly what went.
- A decision that is close is a **warning** by default and an **error** under
  `enx check --strict`, so a caller can choose how much doubt to accept.

## 11.1 The `~ ... ~` escape

For the rare span that is genuinely ambiguous to both the grammar and the
lexicon, the author can say which they meant:

```
x = 5
~ total ~
y = 6
```

With the escape, `total` is prose and is deleted; without it, `total` alone is a
valid Ruby expression statement and cheap enough to keep.

The rule for recognising an escape is deliberately narrow, because `~` is a real
operator in all three languages: a tilde opens an escape only when it stands
alone between whitespace, and the escape closes at another tilde that stands
alone, on the same line. So `x = ~y;` and `x = ~y + ~z;` are ordinary code, and
an unterminated tilde cannot swallow the rest of the file. The delimiters are
dropped along with the prose they mark.

Perl needs the escape least: under `strict subs` a bareword alone is not a
statement at all, so the grammar already refuses it.

## 12. Host languages

| Host | Lexer | Machine | Vocabulary | Own parser available | Oracle in tests |
|---|---|---|---|---|---|
| Ruby | done | done | done | Prism/Ripper on the development machine, not wired into tests | `ruby -c` and execution |
| Perl | done | done | done | **none** (PPI absent; needs cpan) | `perl -c` and execution |
| C | done | done | done | clang on the development machine, not wired into tests | gcc `-Wall -Wextra` and execution |

### 12.1 Ruby

Hardest to disambiguate, because Ruby allows paren-less calls, so English-shaped
token runs are frequently valid code. The lexicon and the contextual rules of
section 7.1 carry that load.

Lexical facts the tokenizer handles: line and `=begin`/`=end` comments, numeric
literals (`0x`, `0b`, `0o`, `_`, floats, exponents, `r`/`i` suffixes), strings
with interpolation, percent literals, symbols including quoted and operator
symbols, regex-versus-division by previous significant token,
instance/class/global variables, heredocs (`<<~`, `<<-`, quoted and bare) with
bodies consumed after the newline, and `?`/`!` method-name suffixes.

### 12.2 Perl

Perl is the **easiest** host for the grammar and the hardest to tool. The
grammar's model of `use strict` (section 7.2) removes prose ambiguity
syntactically, and sigils (`$`, `@`, `%`) cannot be English words at all. On
the other side, no Perl parser is installed, so `Machine` is hand-written and
the only oracle is `perl -c` plus execution.

Lexical facts the tokenizer handles: `#` comments and POD blocks, all five
sigils including the punctuation globals (`$_`, `$!`, `$@`, `$0`, `$/`, `$^W`),
`$#array`, `${name}` and the block forms, dereference chains (`@$ref`, `$$ref`),
numeric literals (`0x`, `0b`, `0o`, `_`, floats, exponents), single and double
quoted strings, backticks, the quote-like operators `q qq qw qr m s tr y` with
paired and unpaired delimiters and their trailing modifiers, regex literals with
the division-versus-regex heuristic, heredocs with bodies after the newline, and
labels.

Two rules the lexer has to get right, both learned the hard way:

- **A sigil is only a sigil when a name follows.** Otherwise the `*` in
  `$x * 2` is read as a typeglob and the multiply disappears.
- **A trailing comma is legal before a closer.** `( 1, 2, )` is ordinary Perl,
  and refusing it failed every hash literal in the corpus. Ruby had the same
  latent bug.

Grammar coverage: `use`, `no`, `require`, block and statement `package`,
`my`/`our`/`local`/`state` declarations (parenthesised and bare, with
initialisers), `sub` definitions including prototypes, signatures, `:name`
attributes and forward declarations, anonymous `sub { }`, `if`/`elsif`/`else`,
`unless`, `while`/`until` with `continue`, both `for` forms, `foreach` with and
without a declared loop variable, simple `given`/`when`, bare blocks, `LABEL:`
statements, `return`/`last`/`next`/`redo`/`goto`/`die`/`warn`, postfix modifiers
including after a control keyword, list operators with and without parentheses,
`->` method calls, dereference and subscript chains, arrays, hashes, `=>`,
ternary, `eval` and `do` blocks, and the full operator set. `__DATA__` and
`__END__` are preserved as trivia.

### 12.3 C

C is the hardest of the three. Ruby has paren-less calls, which make English
valid code but at least leaves a lexicon to argue with; Perl has sigils and
strict subs, which refuse English outright. C has neither shelter, and a large
share of its keywords are ordinary English words:

```
if else while do for switch case default return break continue goto
static auto register extern union enum long short signed unsigned
float void const volatile
```

plus library names that are also English — `index free time open read write
close exit abs div rand sleep signal`.

What C does have is a hard structural rule: **two adjacent identifiers are never
valid**, because there are no paren-less calls. Most English runs therefore fail
on their own, and the cost model only has to handle the keyword collisions. The
same adjacency property supports the `(`/`[` rule of section 7.1, which keeps
`sum(a, b)` a call when "sum" is also an English word.

Lexical facts the tokenizer handles: `//` and `/* */` comments, line splices
(`\` at end of line, which joins physical lines everywhere in C and not only in
macros), integer and floating literals with all their prefixes and suffixes,
character constants versus strings, and operators.

**The preprocessor is its own token.** A directive is lexed as one token
covering its whole logical line, continuations included, because a macro body is
a different language and the parser has no business inside it. A `#` only begins
a directive at the start of a line, so a stray one does not swallow the rest of
the file. The grammar then accepts a directive wherever an item or a statement
may appear.

**Capitalisation is not syntax.** The C lexer emits every identifier as
`Identifier`; it does not mark `RED` as a constant the way the Ruby lexer marks
`Foo`. Ruby's grammar distinguishes them and C's does not, and treating `RED` as
a different kind broke every enum.

Grammar coverage: declarations with storage classes, type specifiers, struct,
union and enum definitions, typedef names, declarators including pointer, array
and function forms, bit-fields and the parenthesised `int (*fp)(int, int)` shape,
K&R and prototype parameter lists, initialisers including nested and designated
forms, function definitions, compound statements, `_Generic`, `_Static_assert`,
`_Alignas`, `__attribute__`, `__declspec`, compound literals, `if`/`else`,
`while`, `do`/`while`, `for` including a C99 declaration in the init clause,
`switch` with `case` and `default` kept as labels, `goto`, labels, `return`,
`break`, `continue`, and the full operator set with casts, `sizeof` and
string-literal concatenation.

## 13. Command line

```
enx label [flags] file    every token, its costs, its label, and why
enx check [flags] file    diagnostics and counts
enx mix   [flags] file    emit the program
enx run   [flags] file    emit and execute
enx langs                 available languages and their readiness

flags:
  --lang NAME        host language (default ruby)
  --out FILE         (mix) write the emitted program here instead of stdout
  --keep-prose       (mix, run) emit dropped English as host comments
  --anaphor CHAR     claim CHAR as an anaphor marker, so !-1 is the most recent
                     assignment, !-2 the one before it, !-1:Point the most recent
                     Point (off by default)
  --strict           (check) treat warnings as errors
  --margin N         (check) cost margin below which a decision is ambiguous
  --provisional      (mix, run) allow emission without a grammar machine (unreliable)
  --log-level LEVEL  off, error, warn, info, debug  (default warn)
  --log-json         structured log output
```

Exit codes: `0` success, `1` error, `2` usage error. Flags must precede the
file argument, because Go's `flag` package stops at the first non-flag word.

`mix` and `run` refuse to work on a language without a `Machine`, returning
`ErrNoGrammar`.

`ErrNoParse` exists for the case where no labelling can satisfy the grammar at
all, but it is **effectively unreachable**: every token has a finite skip cost,
so skipping the entire file is always a valid program. A file of pure garbage
therefore emits an empty program and exits `0`, with a `no-code-kept` warning.
The warning thresholds are described in section 15.

## 14. Testing strategy

| Level | What it pins |
|---|---|
| `core` | line index, CRLF, rune columns, span clamping, diagnostic counts |
| `host/ruby` lexer | **totality**, token kinds, regex vs division, heredocs including CRLF terminators, percent literals, numbers, unterminated input, Unicode punctuation |
| `host/ruby` machine | grammar coverage over 50 real Ruby snippets, kept tokens plus trivia reproduce the input, separator boundaries, no separator after a bare `return`, no memo collision between `ifExpr` and `unlessStmt`, singleton and setter definitions, simple `case/in` arms, empty input |
| `host/perl` lexer | totality, all five sigils and the punctuation globals, quote-like operators as single tokens, substitutions, regex vs division, POD as trivia, heredocs, numbers, labels |
| `host/perl` machine | grammar coverage over 74 real Perl snippets, strict-subs prose rejection, separator boundaries, sub attributes, given/when, package blocks, `__DATA__`, empty input |
| `host/c` lexer | totality, preprocessor as one token with continuations, `#` only at line start, keywords, numbers, char vs string, operators, line splice, Unicode punctuation |
| `host/c` machine | grammar coverage over 83 real C snippets, prose rejected by adjacency, statement boundaries, C11 generics, static assertions, alignment, attributes, bit-fields, compound literals, empty input |
| `host/ruby` types | literal and constructor types, a list right hand side is not typed from its first element, a chained constructor is not typed |
| `host/perl` types | constructor types only when the constructor is the whole right hand side, sigil container types |
| `host/c` types | per-declarator pointer detection, including `char *a, b;` |
| `host` fuzz | marker and escape matchers stay total and return offsets inside the input |
| `host/*` fuzz | each lexer reproduces arbitrary bytes exactly |
| `frontmatter` | leading `---`, closing `---`/`...`, scalar, inline and block imports, CRLF, comments, unknown keys, malformed headers |
| `loader` | depth-first order, imports relative to each file, deduplication, cycles, missing files, frontmatter removal |
| `codeswitch` | lexicon load and ordering (cached), nil safety, malformed rows, cost model, identity when nothing is English, runs partition the file, code text is a subsequence, comments survive prose removal |
| `codeswitch` decode | the flagship mixed example, adjacent call vs prose, constructor and method chains, statement-boundary separator, prose-only file, identity on pure Ruby, multiple assignment as one statement, a statement before a prose comma |
| `codeswitch` separator | an existing line break is preserved, a statement boundary becomes a newline, a same-line region becomes a space, and the next code token's line indentation is reused at the seam |
| `codeswitch` ambiguity | the pinning wrapper's two directions, common English kept as code is reported, code-shaped names are not, the margin controls the report, degenerate input |
| `host` escape | the narrow rule for `~ ... ~`, file-start boundary, and that a non-tilde offset never claims an escape |
| `pipeline` | unknown language, refusal without grammar, readiness of every language, prose dropped, comments preserved, `--keep-prose`, empty output for prose-only input, the escape forces prose in all three languages, real tildes left alone, one documented limitation, warnings for a file with no code or mostly dropped tokens |
| `pipeline` anaphora | resolution end to end, typed filters, unwritten types are not guessed, class-returning constructors are excluded, `--keep-prose` and `--anaphor` compose |
| `pipeline` scaling | decode time at four file sizes, reported rather than asserted |
| `cmd/enx` | usage errors, all five commands, `--out`, `--lang`, `--keep-prose`, `-h`, flag errors on stderr, anaphor referents in `label`, unresolved markers in `check`, lexicon-failure exit code, exit codes, all three languages, the compile step for C and its argument list |
| `examples` | the canonical end-to-end suite: every example matches its `.expected` emission and, when its toolchain is present, its `.stdout`; includes the larger showcase fixtures and the frontmatter import examples |
| `examples/strict` | a clean mixed file that `enx check --strict` accepts; used by `task strict` and CI |

Every `.enx` example has `.expected` and `.stdout` sidecars. The example test is
the strongest end-to-end check: a labelling that is syntactically plausible but
semantically mangled cannot pass it.

## 15. Known gaps

Carried forward deliberately, not overlooked.

- **Unsupported syntax is dropped; gross drops are now reported.** There is no
  partial parse and, because every token has a finite skip cost, skipping the
  whole file is always a valid program. A file containing nothing but garbage
  emits an empty program, but `no-code-kept`, `mostly-dropped` (80% of
  non-trivia tokens), or `no-complete-statement` is reported as a warning.
  Smaller silent drops still need `enx label` or `enx check`.
  Known unsupported Ruby: complex `case/in` guards and alternatives that are not
  a single expression. Known unsupported Perl: formats, and attribute syntax
  beyond a plain `:name` list. Known unsupported C: attributes after a
  declarator, bit-field edge forms, and preprocessor constructs that cross a
  macro body in a way the one-token directive cannot model.
- **Prose inside a body is weighted, not absolute.** `Body` now compares
  "statement then continue" against "skip then continue" with a local memo, so
  an ordinary prose sentence inside a class, subroutine or function is dropped.
  The remaining hazard is English that is itself valid code: a prose sentence
  containing `return`, a bare constant, or a keyword can still become a tiny
  statement. `enx label` and the `~ ... ~` escape are the escape hatches.
- **Statement alternation has no lookahead.** A statement form is chosen on its
  own cost, so a cheaper prefix can beat the whole construct. That is what
  happened with multiple assignment: `a` alone costs less than `a, b = 1, 2`.
  The fix is for a statement form to refuse a reading the grammar knows is
  impossible — `exprStmt` refuses to stop at a comma *when a multiple assignment
  actually starts there* — rather than to add lookahead. Other constructs of the
  same shape would need the same treatment, and the general fix would be to let
  `Program` consider more than one candidate per statement.
- **A lone capitalised word standing in Ruby prose may be kept as a constant.**
  `SKUs` is valid Ruby and no lexical evidence argues against it.
  `TestKnownLimitationCapitalInProse` records this; Perl and C are unaffected,
  Perl because strict subs refuses bare capitals and C because capitalisation is
  not syntax there.
- **A C declaration and a C expression can share a spelling.** `a * b;` reads as
  a declaration of a pointer if `a` is a typedef name, and as a multiplication
  otherwise. Both readings keep the same bytes, so the emitted program is
  identical either way; only the boundary map could differ.
- **Prose inside an expression** is not skipped, only between statements. A
  sentence wedged between two operands will usually stop the expression.
- **The ambiguity analysis is capped and partial.** It probes at most 128
  regions and reports when it hits the cap, so a large file gets a partial
  answer. It also reports near-ties as warnings; a caller wanting none of them
  can lower `--margin`.
- **Indentation at a rewritten seam** is taken from the next code token's own
  line when possible, falling back to the swallowed layout. That is right in the
  common case but is not a formatter.

### 15.1 Scaling, measured

The decoder's cost is not obviously linear: every result carries the token
indices it kept, and combining two results copies them, so the total work can
grow with the square of the file. `TestDecodeScalesWithFileSize` measures it on
synthetic mixed files:

| statements | tokens | elapsed | growth |
|---|---|---|---|
| 250 | 7,000 | 71 ms | — |
| 500 | 14,000 | 165 ms | 2.3× |
| 1,000 | 28,000 | 407 ms | 2.5× |
| 2,000 | 56,000 | 1.09 s | 2.7× |

Growth per doubling is 2.3× to 2.7×, so the search is mildly super-linear
rather than quadratic in practice: a 56,000-token file — roughly three thousand
lines — decodes in about a second. The measurement is reported
rather than asserted, because a timing assertion in CI would be flaky; a loose
30-second ceiling fails loudly on a pathological regression.

## 16. Milestones

| | Scope | State |
|---|---|---|
| M0 | repo, module, embedded lexicon, core, interfaces, CLI skeleton | done |
| M1 | Ruby tokenizer, lexicon, cost model, labeller, plan/emitter, tests | done |
| M2 | Ruby grammar machine, weighted decoder, grammar-aware separator, example and oracle tests | done |
| M3 | Perl tokenizer, grammar machine modelling `use strict`, vocabulary, example and oracle tests | done |
| M4 | shared parser core extracted; C tokenizer, preprocessor, declarators, vocabulary, example and oracle tests | done |
| M5 | ambiguity margin with `check --strict`, the `~ ~` escape, Ruby multiple assignment, measured scaling, documents | done |

The project is feature complete against its stated goals. Everything after this
is depth: wider corpora, more constructs per language, and cost tuning measured
against labelled data rather than against a handful of files.

## 17. Glossary

| Term | Meaning |
|---|---|
| **matrix language** | the host language supplying the grammatical frame |
| **embedded language** | English, contributing only inert prose |
| **switch point** | a position in the token stream where the label changes |
| **trivia** | spacing and comments; always preserved, never English |
| **English region** | a maximal group of English tokens replaced as one unit |
| **code run** | a maximal stretch of verbatim code |
| **insertion** | an English token inserted into the code stream; the only error the model allows |
| **switch cost** | charged once per maximal English run, because prose comes in phrases |
| **oracle** | an external parser (`ruby -c`, `perl -c`, `gcc`) used only in tests |
| **anaphor** | a marker such as `!-1` that names an earlier assignment; see section 18 |
| **referent** | the variable an anaphor stands for |
| **history** | the assignments an anaphor may reach, most recent first |

## 18. Anaphora

Anaphora lets a program refer to a previous result by position instead of by
name:

```
stock = 10
mugs = stock - 3
total = !-1 + !-2          →  total = mugs + stock
```

`!-1` is the most recent assignment, `!-2` the one before it.

### 18.1 Off unless asked

The feature is **opt-in at the command line**, and there is no in-file switch:

```
enx mix --anaphor '!' file
enx run --anaphor '!' file
```

The marker character is configurable, and `--anaphor` is what *claims* it. This
is not caution for its own sake; see 18.2.

The forms are:

```
!-1            the most recent assignment
!-2            the one before it
!Point         the most recent assignment of type Point
!-2:Point      the second most recent Point
!-1:char*      the most recent pointer-to-char
```

A type filter narrows the history first, and the ordinal then indexes into the
narrowed list, so `!-2:Point` is the second most recent *Point* rather than the
second binding overall. Omitting the ordinal means the most recent.

### 18.2 Why the marker must be claimed rather than assumed

Every claimed form collides with real syntax somewhere, because `!` is logical
negation in all three hosts:

| Form | Ruby | Perl | C |
|---|---|---|---|
| `!-1` | logical-not of `-1`, silently `false` | same, silently `false` | same, silently `0` |
| `!Point` | logical-not of a constant, silently `false` | bareword, error under strict | undeclared identifier, error |
| `!-2:Point` | syntax error | syntax error | syntax error |

So two of the three forms are silently valid somewhere, and the type-qualified
form is the only one that fails loudly everywhere. The previous marker, `*`,
had the same problem from the other direction: `*1` is a splat in Ruby and a
typeglob in Perl.

Requiring the flag means the tool never claims a marker nobody asked for, and
`--anaphor` documents the decision in the command line, where it can be reviewed.

A useful side effect: `--anaphor '#'` is rejected, because `#` begins a comment
in Ruby and Perl and a directive in C. A rejected flag is a usage error, not a
silent no-op.

### 18.2.1 Why the marker is `!` and not `*`

`*` is the more natural reading, but it cannot express a pointer type: with `*`
claimed, `*1:char*` is ambiguous at the last character. Moving to `!` frees `*`
to mean pointer inside a type name, which is why `!-1:char*` works.

### 18.3 What may be referenced

**Assignments only.** `puts x` is not an assignment, so it does not enter the
history. This keeps the referent a bare variable name, which is what makes the
whole feature a token substitution (section 3.1) with no evaluation-order
question and no synthesised names. A compound assignment such as `count += 1`
counts, and so does each name in a multiple assignment (`a, b = 1, 2` makes both
`a` and `b` results).

**A statement cannot see its own target.** Bindings become visible when their
statement ends, not when their name is read. Without that, `total = !-1` would
resolve `!-1` to `total` itself, because `total` is the most recent assignment
by the time the right hand side is reached. Deferring the binding by one
statement is what prevents it, and it costs nothing: no anaphor can usefully
refer to the assignment it is part of.

**Scope.** The history is per block, driven by a per-language `ScopeTracker`
that says which tokens open and close a scope. A nil tracker means the whole
file is one scope, which is what all three languages currently use: the
simplest thing that works, and enough because a host that does not like the
result will say so. An anaphor sees the current scope's assignments and then the
enclosing ones, most recent first.

**Nothing else is checked.** An anaphor that names nothing — `!-7` in a file with
three assignments — is left as written, so the host compiler reports it, and
`enx check` reports it as a warning. That is deliberate: inventing an error
message here would be guessing, and the host already has one.

### 18.4 The rewrite

The anaphor token's bytes are replaced by the referent's name. Nothing else in
the file changes.

```
mugs = stock - 3
total = !-1 + 1
```

becomes

```ruby
mugs = stock - 3
total = mugs + 1
```

Because the referent is always a name, the substitution cannot change how many
times anything is evaluated, and it cannot shadow or collide with anything: it
only repeats a name the author already wrote.

### 18.5 Lexing the marker

The marker is claimed by the **lexer**, not the parser, so an anaphor works in
any position: a statement, an argument, a condition, or inside prose.

Two forms are claimed, and everything else falls through to the host as ordinary
code:

```
marker "-" digits [ ":" type ]     an ordinal, optionally filtered
marker type                        the most recent of a type
```

A type is an identifier, optionally namespace-qualified with `::`, optionally
followed by `*`. So with `--anaphor '!'`, `!-1`, `!-2:Point` and `!-1:char*` are
anaphors, while `!1` (no minus), `!-1x` and `!` alone are not.

The token is emitted as `host.Anaphor` and is always code, so it survives the
strip even when it sits inside a sentence:

```
add !-1 and !-2      →  total = mugs + stock
```

`!-1` there is code, `add` and `and` are prose.

Two lexer details that mattered:

- **The anaphor check comes first in the run loop**, before the sigil handling.
  Ruby claims `@` and Perl claims `*` for variables, so a later check would never
  see a marker that also happens to be a sigil.
- **A marker is expensive to skip** (`StrongCodeCost`). At `NoiseCost` the
  decoder preferred to delete a marker rather than keep it, because keeping it
  restarts a prose run and pays a switch.

### 18.6 Reporting

`enx label` shows each anaphor with the name it resolved to, or says that it
resolved to nothing. `enx check` reports an anaphor that resolves to nothing as
a warning rather than an error, because the host will reject it and the tool
does not need to duplicate that judgement.

### 18.7 Types

A type filter asks for the most recent assignment *of a type*, not merely the
most recent assignment:

```
p = Point.new(3, 4)
n = 10
puts !-1:Integer        # 10, not the Point
puts !-1:Point          # #<Point ...>
```

The governing rule is **read the type where the host writes it; never infer it.**
Where a host does not write the type, the binding has none and a typed anaphor
cannot reach it. That is the whole boundary:

| Host | Where the type is written | Example |
|---|---|---|
| C | the declaration | `double ratio = 2.5;` |
| Ruby | a literal's class, or the constant in a constructor | `p = Point.new`, `s = "x"`, `n = 5` |
| Perl | the sigil, or the class in a constructor | `my @list`, `my $p = Point->new` |

So C is exact, and Ruby and Perl are conventional rather than inferred: a
constructor's class is *spelled out* in the source, on the right of the `=`
instead of the left. The constructor or literal must be the whole right hand
side: `Point.new.to_s` is a String, not a Point, and is not typed.

**Excluded, deliberately**: `x = f()`, `x = a + b`, `x = obj.attr`,
`x = cond ? a : b`, and any right hand side with a top-level comma such as
`a, b = 1, "x"` or `x = 1, 2`. The type is not written in any of those, or it
cannot be mapped to a single target, and a wrong guess would silently select the
wrong referent, which is the one failure this project refuses.

**The constructor convention has a bounded exception.** `Point.new` returns a
`Point` by convention, not by guarantee, and four constructors return a class
rather than an instance of themselves: `Struct`, `Class`, `Module` and `Data`.
Those are excluded, so `X = Struct.new(:a)` does not make `X`'s type `Struct`. A
class that overrides `self.new` to return something else is not detected; that is
documented as an assumption rather than a fact.

**Pointers.** `!-1:char*` matches only pointer-to-char, and `!-1:char` matches
either, because `char` is the type in both `char c` and `char *s`. The star is
expressible precisely because `*` is no longer the marker (18.2.1).

Type names are compared exactly, so a type must be written as the host writes it.

#### Not implemented

- **Inferring a type that is not written.** A method call's result, an arithmetic
  expression, an attribute read: the type is not in the source, and a wrong guess
  deletes or keeps the wrong thing. Types are implemented by *reading* them where
  each host writes them; see 18.7.
- **Referring to non-assignments**, for the reason in 18.3.
- **Rewriting an effectful referent.** The referent is a name, so there is
  nothing effectful to rewrite.
- **Per-block scope tracking.** `ScopeTracker` exists and is tested, but no
  language supplies one, so every file is currently a single scope. Adding one
  per language is a small, contained change.
- **A class that overrides `self.new`** to return something else. Undetected by
  design, and documented as an assumption.

### 18.8 Implementation notes

| Piece | Where |
|---|---|
| the marker grammar, shared by all three lexers | `host.MatchAnaphor`, `host.ParseAnaphor` |
| `Anaphor` kind, `AnaphorLexer`, `ScopeTracker`, `TypeReader` | `internal/host` |
| resolution, type filtering, substitution | `internal/anaphor` |
| type readers per host | `internal/host/{ruby,perl,c}/types.go` |
| emission with substitution | `codeswitch.Plan.Substitute` |
| the CLI flag and its guard | `cmd/enx --anaphor` |

Five findings from building it, each of which shaped the result:

- **The grammar has to accept an `Anaphor` token as a value.** It was rejected
  as a primary, so `total = !-1` failed to parse and only fragments survived.
  `Anaphor` is now accepted wherever a literal is, in all three grammars.
- **The marker must be expensive to skip.** At `NoiseCost` the decoder preferred
  to delete a marker rather than keep it, because keeping it restarts a prose
  run and pays a switch. It is now `StrongCodeCost`.
- **A binding must not be visible to its own statement**, as described in 18.3.
- **Ruby had a latent semicolon bug.** `;` is a statement separator and was being
  dropped, so `def foo; 1; end` became `def foo 1`, which is invalid Ruby. The
  line-restricted lookup could not fix it: Ruby's stop predicate includes `;`, so
  the lookup refused to return the very token it stopped at.
- **Two pre-existing Perl bugs surfaced.** `new` was priced as an English
  function word, so `sub new` was dropped; and every list operator tried to take
  a block, so `bless {}, shift` failed and took the enclosing `sub` with it. A
  word after a definition keyword is now code, and only `map`, `grep` and `sort`
  take blocks.

## 19. Frontmatter imports

An enx file may begin with a small YAML header:

```yaml
---
imports:
  - lib/helpers.enx
  - lib/money.enx
---
```

The header is metadata, not source. It is removed before lexing, so it never
appears in the emitted program.

### 19.1 Supported subset

`internal/frontmatter` is deliberately not a general YAML parser. It recognises
a leading `---` line, a closing `---` (or `...`) line, and the `imports` key.
Unknown keys are ignored, including an indented block below them. The value may
be:

```yaml
imports: helpers.enx
imports: [helpers.enx, lib/money.enx]
imports:
  - helpers.enx
  - lib/money.enx
```

Single and double quotes and trailing comments are supported. A file without a
header is returned unchanged.

### 19.2 Expansion

`internal/loader` resolves imports recursively:

1. Read the importing file and split its header.
2. For each import in order, resolve a relative path against the importing
   file's directory.
3. Expand that file the same way, then append its body.
4. Append the importing file's own body.

The result is one `core.Source` named after the file the user asked for. A file
is included at most once, even if it is reachable through two paths. A cycle is
an error. Imported bodies are separated by a newline if the previous body did
not end with one.

The imported body is ordinary enx source: its prose is decoded and removed, its
code is emitted, and it shares the combined file's anaphor history. Imports are
therefore useful for shared helpers and examples, not for syntactic fragments
that would only parse inside another construct.

### 19.3 Known limitation

Because imports are textually prepended, a diagnostic raised inside imported
code reports the importing file's name and composite line number. A future
source map can attribute each range to its original file without changing the
labelling model.

### 19.4 Tests

`internal/frontmatter` tests the header forms, CRLF, comments, unknown keys and
malformed headers. `internal/loader` tests depth-first order, recursive relative
paths, deduplication, cycles and missing files. The showcase import examples
exercise the feature end to end in Ruby, Perl and C.
