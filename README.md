# enx

Write code the way you would explain it.

`enx` removes the English from a source file where English and code are mixed,
and leaves you with a normal program you can run. No markers, no fences, no
special syntax for the English.

```
$ enx mix hello.enx
x = 5
y = 10
puts x + y
```

From this file, `hello.enx`:

```
we set x to five
x = 5
and y to ten
y = 10
then we print their sum
puts x + y
```

Then run it:

```
$ enx run hello.enx
15
```

The code inside is **real Ruby**, real Perl, or real C. `enx` does not translate
between languages and does not invent a new one. It decides, token by token,
which tokens are English and which are code, deletes the English, and leaves
everything else byte for byte.

## What it is not

- Not a new programming language. The islands are your language.
- Not a translator between languages. A `.enx` holding Ruby produces Ruby.
- Not magic. If a decision goes wrong, `enx label` will show you exactly which
  token it got wrong and why. There is a list of known limits at the end.

## Try it in one minute

You need Go 1.26 or newer. Ruby, Perl and gcc are optional: they are needed to
run the emitted programs and to exercise the interpreter and compiler oracle
tests. The suite skips those tests when a toolchain is absent.

```sh
git clone <repo> && cd rb
go build ./...
go test ./...
```

Write a file called `hello.enx` with exactly this:

```
we set x to five
x = 5
and y to ten
y = 10
then we print their sum
puts x + y
```

Then:

```sh
go run ./cmd/enx run hello.enx     # emit and execute: prints 15
go run ./cmd/enx mix hello.enx     # emit only
go run ./cmd/enx label hello.enx   # show every decision
```

Or build a binary once and use it as `enx`:

```sh
go build -o enx ./cmd/enx
./enx run hello.enx
```

## More examples

The `examples/` directory holds runnable adaptations of programs in the style of
*Eloquent Ruby*, the *Perl Cookbook* and *The C Programming Language*. They are
written in the same mixed-prose style as everything else, and each one has a
recorded expected output.

```sh
go run ./cmd/enx run --anaphor '!' examples/eloquent_ruby/01_document.enx
go run ./cmd/enx run --anaphor '!' --lang perl examples/perl_cookbook/01_price_report.enx
go run ./cmd/enx run --anaphor '!' --lang c examples/c_programming_language/01_hello.enx
```

See [examples/README.md](examples/README.md) for the full list.

## Imports and frontmatter

An `.enx` file may start with a small YAML header. Only `imports` is interpreted;
other keys are ignored.

```yaml
---
imports:
  - lib/helpers.enx
  - lib/money.enx
---
```

Imports are depth-first textual includes. The imported file's frontmatter is
removed, its imports are resolved relative to its own directory, and its body is
placed before the importing file's body. A file is included at most once, and a
cycle is an error. Imported prose is decoded like any other prose, so an import
can contain both explanation and code.

The header reader supports a scalar (`imports: helpers.enx`), an inline list
(`imports: [a.enx, b.enx]`), or a block list. It is a deliberately small YAML
subset, not a general YAML parser.

## How it works, in one paragraph

With no delimiters there are two possible languages at every token, so `enx`
reads the file as a **code-switched** document. Your language's grammar is a
hard constraint: the code it keeps must form a program that parses. An English
word-frequency list is soft evidence: common words are likely prose. It picks
the cheapest consistent reading, deleting the words it decided were English.
A file with no English in it comes out **identical**, byte for byte, which is
the property everything else is built on. Comments are never English: they are
preserved exactly as written, even when they stand next to a dropped sentence.

Perl is the easiest target, because its grammar is modelled on `use strict`,
where a bare word is not even a valid expression. C is next: two adjacent names
are never valid C. Ruby is hardest, because `puts answer` and `print the
answer` can both be valid Ruby, so Ruby needs word frequencies to tell them
apart.

### The algorithm, briefly

**Memoised best-cost recursive descent.** Each grammar production is cached per
production-and-position, and each may skip tokens it cannot use, paying their
price. At the statement level it is a linear dynamic program:

```
program(i) = min( statement(i) + program(after the statement),
                  skip(i) + switch + program(i + 1) )
```

It is **not** beam search: there is no beam width and no pruning, so the answer
does not depend on a tuning knob. One consequence is worth knowing before you
read the limitations: each production returns only its own cheapest reading, so
a cheaper *prefix* can occasionally beat a whole construct. That is the cause of
the body limitation below, and the fix is to let the statement layer weigh more
than one candidate.

Cost: 56,000 tokens decode in about 1.1 seconds, growing 2.3× to 2.7× per
doubling of input.

## Examples

### Ruby: a class

```
A greeter class.

class Greeter
  def greet
    "hi"
  end
end

Then we use it.

puts Greeter.new.greet
```

`enx mix` gives you:

```ruby
class Greeter
  def greet
    "hi"
  end
end
puts Greeter.new.greet
```

and `enx run` prints `hi`.

### Ruby: a whole small program

`examples/showcase/ruby/shop.enx` mixes prose with a module, two classes, blocks,
a conditional and a sort. It runs as written:

```
$ enx run examples/showcase/ruby/shop.enx
items: 2
subtotal: $34.48
tax: $2.75
total: $37.23
that is a big order
Ceramic Mug at $8.50
Coffee Beans at $12.99
```

### Perl

```
We keep prices in whole cents.

use strict;
use warnings;

A helper turns cents into a dollar string.

sub price_str {
    my ($cents) = @_;
    return sprintf('$%d.%02d', $cents / 100, $cents % 100);
}

Two coffees and a mug.

my $coffee = 1299;
my $mug = 850;
my $total = $coffee * 2 + $mug;
print "total: ", price_str($total), "\n";
```

```
$ enx run --lang perl thatfile.enx
total: $34.48
```

The emitted Perl passes `perl -c` and runs.

### C

```
We need stdio for printing.

#include <stdio.h>

Two coffees and a mug, all in cents.

int main(void) {
    int coffee = 1299;
    int mug = 850;
    int total = coffee * 2 + mug;
    printf("total: $%d.%02d\n", total / 100, total % 100);
    return 0;
}
```

```
$ enx run --lang c thatfile.enx
total: $34.48
```

C is compiled, not interpreted, so `run` builds it first with gcc and then runs
the executable. The emitted C passes `gcc -std=c11 -Wall -Wextra`.

### Prose inside prose, code inside prose

English and code can share a line. Nothing needs to be on its own line:

```
we start with x as five, that is x = 5, and y as five too: y = 5
and their sum is a function of two arguments, so sum = lambda { |a, b| a + b }
finally we call sum with x and five and print the answer: answer = sum.call(x, 5)
and then we show it: puts answer
```

```
$ enx run examples/showcase/ruby/greeting.enx
10
```

## See every decision

When something looks wrong, `enx label` shows one row per token, with the cost of
calling it code, the cost of calling it English, the decision, and the reason:

```
$ enx label hello.enx
  line kind       text           code$  english$  label    reason
     1 identifier "we"           28.000     6.013  english  skipped: the grammar has no use for it here
     1 identifier "set"          11.503     7.503  english  skipped: the grammar has no use for it here
     1 identifier "x"             4.000    10.000  english  skipped: the grammar has no use for it here
     2 identifier "x"             4.000    10.000  code     kept: the grammar needs it here
```

Costs are negative log probabilities, so lower is likelier. English costs come
from word frequency; code costs come from what the token looks like and where it
sits.

## When the tool is unsure

`enx check` re-reads the file with each region's opposite reading forced, and
reports the ones that were close:

```
$ enx check examples/showcase/ruby/greeting.enx
examples/showcase/ruby/greeting.enx: 139 tokens, code=31 english=46, ambiguous=4
examples/showcase/ruby/greeting.enx:3:59: warning [ambiguous-span]: "answer" is code at 453.819 but english at 454.819
    hint: say which you meant, or lower --margin to report fewer of these
```

`sum` and `answer` are ordinary English words that the grammar kept as code, and
the other reading is within a hair of the chosen one. Perl and C report nothing
here, because their decisions are structural rather than statistical.

Use `--strict` to make any such warning fail the run, which is what you want in a
build script.

## Forcing a reading

For a span that is genuinely ambiguous to both the grammar and the word list,
mark it with tildes:

```
x = 5
~ total ~
y = 6
```

```
$ enx mix thatfile.rb
x = 5
y = 6
```

The tildes are deleted along with the prose. `~` is a real operator in all three
languages, so the rule is narrow: a tilde opens an escape only when it stands
alone between whitespace, and closes at another tilde on the same line. So
`x = ~y + ~z;` is left completely alone.

## Anaphora: referring to earlier results

Turn this on with `--anaphor '!'`. Then `!-1` means the most recent assignment,
`!-2` the one before it:

```
stock = 10
mugs = stock - 3
total = !-1 + !-2
puts total
```

```
$ enx run --anaphor '!' thatfile.rb
17
```

`total = !-1 + !-2` is emitted as `total = mugs + stock`.

`enx label` shows what each marker resolved to, and `enx check` reports a marker
that names nothing as a warning, which is what makes the feature auditable
rather than a hidden rewrite.

A **type filter** asks for the most recent result *of a type*. A type is read
from where your language writes it, never guessed:

```
class Point
  def initialize(x, y); @x = x; @y = y; end
  def sum; @x + @y; end
end

p = Point.new(3, 4)
n = 10

puts !-1:Integer
puts !-1:Point.sum
```

```
$ enx run --anaphor '!' thatfile.rb
10
7
```

- **C** writes the type in the declaration, so `!-1:double` is exact.
- **Ruby** and **Perl** write it on the right of the `=`: a literal's class
  (`5`, `"x"`, `[1]`), or the constant in `Point.new` / `Point->new`.
- **Perl** also has it in the sigil, so `my @list` is an `array` and `my %map` is
  a `hash`.

Where no type is written — a method call result, an arithmetic expression — the
binding has no type, and a typed anaphor cannot reach it. That is deliberate: a
wrong guess would pick the wrong variable without any error. The type-bearing
literal or constructor must also be the whole right hand side: `Point.new.to_s`,
`[1, 2].first`, and a list such as `a, b = 1, "x"` are excluded.

Because `*` is no longer the marker, `*` can mean *pointer* inside a type name:

```c
char *label = "x";
char letter = 'a';
char *ptr  = !-1:char*;   /* the most recent pointer-to-char: label */
char plain = !-1:char;    /* any char binding, pointer or not: ptr */
```

## Commands

```
enx label [flags] file    show every token with its cost and its label
enx check [flags] file    diagnostics, counts, and ambiguous decisions
enx mix   [flags] file    emit the program with English deleted
enx run   [flags] file    emit the program and execute it
enx langs                 list available languages and how they run
```

Flags go **before** the file name:

```
enx mix --out out.rb in.enx
```

| Flag | Meaning |
|---|---|
| `--lang NAME` | host language: `ruby` (default), `perl`, `c` |
| `--out FILE` | `mix` only: write the emitted program to a file instead of stdout |
| `--keep-prose` | `mix`, `run`: emit dropped English as comments, for checking |
| `--anaphor CHAR` | claim `CHAR` as an anaphor marker, e.g. `'!'` |
| `--provisional` | `mix`, `run`: allow emission without a grammar machine (unreliable) |
| `--strict` | `check` only: treat warnings as errors |
| `--margin N` | `check` only: cost margin below which a decision is ambiguous |
| `--log-level LEVEL` | `off`, `error`, `warn`, `info`, `debug` |
| `--log-json` | structured logs |

`enx langs` tells you what is available and how each one runs:

```
$ enx langs
ruby     ready         ext=.rb   comment="#" runner=ruby
perl     ready         ext=.pl   comment="#" runner=perl
c        ready         ext=.c    comment="//" runner=gcc -std=c11 -O0 -o {out} {src}
```

## Checking what will be emitted

`--keep-prose` turns each dropped sentence into a comment, so you can read the
result and see whether the split was right:

```
$ enx mix --keep-prose hello.enx
# we set x to five
x = 5
# and y to ten
y = 10
# then we print their sum
puts x + y
```

## Known limitations

These are real and worth knowing before you rely on the tool.

- **Prose inside a body is now weighted, but English that is itself valid code
  can still be kept.** The body parser compares keeping a statement with skipping
  a token; a prose sentence containing a keyword such as `return` can still be
  read as a tiny statement. Use `~ ... ~` or `enx label` when that happens.
- **Unsupported syntax is usually dropped, not reported.** Every token can be
  skipped, so the decoder almost always finds *some* reading, often by deleting
  what it could not parse. If a program comes out too short, run `enx label`.
- **English that happens to be valid code can be kept.** `Add the first two
  numbers and return what you get` is valid Ruby, so the word list is what keeps
  it prose. `enx check` reports the cases where that judgement is close.
- **A lone capitalised word in Ruby prose may be kept as a constant.** `SKUs` is
  valid Ruby. Perl and C are unaffected.
- **Anaphora is off unless asked.** The marker is never claimed silently, because
  `!-1` already means `false` in all three languages and `!Point` is valid Ruby.
- **No type inference.** Types are read where the host writes them, nothing more.
- **Imported positions refer to the combined file.** Imports are textually
  prepended, so a diagnostic in imported code reports the importing file's
  line numbers.
- **Seam indentation is reused, not reformatted.** A newline copies the next
  code token's own line indentation when it is available; this is not a general
  formatter.

## Project layout

```
cmd/enx/                  command line
internal/core/            spans, sources, diagnostics, logging
internal/host/            token kinds and the Lexer / Machine / Costs interfaces
internal/host/parse/      the shared best-cost parser machinery
internal/host/ruby/       Ruby tokenizer, grammar, vocabulary, type reader
internal/host/perl/       Perl tokenizer, grammar, vocabulary, type reader
internal/host/c/          C tokenizer, grammar, vocabulary, type reader
internal/codeswitch/      lexicon, costs, labelling, emission
internal/anaphor/         anaphor resolution and substitution
internal/frontmatter/     YAML header parsing and the imports list
internal/loader/          recursive import expansion and source combination
internal/pipeline/        wires the stages together
internal/lang/            the one place that lists concrete languages
data/                     embedded English word frequencies (gzipped)
examples/                 runnable book-inspired examples and expected stdout
```

## Development

The code is leaf-first. Files named `leaf.go` contain functions that call only
standard-library or imported-library functions and read model fields; they do
not call other enx functions. Everything above them is an integrator: grammar
productions, state machines, pipeline stages and command handlers. Prefer a new
leaf over another layer of project-function calls.

```sh
task          # format check, vet, all tests, and a strict example check
task test     # tests only
task strict   # enx check --strict on the clean strict fixture
task fmt      # rewrite formatting
task vet      # go vet
```

On Windows, run `task` from a POSIX shell (MSYS2 or Git Bash); the Taskfile
uses shell syntax for the format check and the word-list task.

CI runs the format check, `go vet`, `go test` and `go test -race` on Linux,
and `go test` on Windows.

Each example has two sidecars: `.expected` is the emitted program and `.stdout`
is what the emitted program prints. Regenerate them with `--anaphor '!'` after
an intentional change:

```sh
enx mix --anaphor '!' --out examples/showcase/ruby/greeting.enx.expected examples/showcase/ruby/greeting.enx
enx mix --anaphor '!' --lang perl --out examples/showcase/perl/shop.enx.expected examples/showcase/perl/shop.enx
enx mix --anaphor '!' --lang c --out examples/showcase/c/shop.enx.expected examples/showcase/c/shop.enx
```

The example test compares both sidecars and runs the program when its toolchain
is available, so the examples are the end-to-end test suite.

The English word list is generated, not hand-written:

```sh
task wordlist   # downloads the corpus, filters it, and gzips the result
```

The full design, including why a grammar-only approach cannot work, is in
[SPEC.md](SPEC.md). The plan and the open work are in [TODO.md](TODO.md).

## License

Artistic License 2.0. See [LICENSE](LICENSE).

Copyright (c) 2026 Xyzzy Apps <xyzzyapps@gmail.com>.
