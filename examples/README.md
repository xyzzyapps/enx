# Examples

These are small programs written in the `enx` style: real Ruby, Perl or C,
interleaved with English that explains what the code is doing. There are eleven
examples per book, covering the major chapters and techniques. They are original
adaptations inspired by *Eloquent Ruby*, the *Perl Cookbook* and *The C
Programming Language*; they are not verbatim excerpts.

Most examples use anaphora. Run them all with `--anaphor '!'` so that `!-1`,
`!-2` and the typed forms are resolved:

```sh
go run ./cmd/enx run --anaphor '!' examples/eloquent_ruby/01_document.enx
go run ./cmd/enx run --anaphor '!' --lang perl examples/perl_cookbook/01_price_report.enx
go run ./cmd/enx run --anaphor '!' --lang c examples/c_programming_language/01_hello.enx
```

Every `.enx` file has two sidecars:

- `.enx.expected` is the emitted program after the English is removed.
- `.enx.stdout` is what the emitted program prints.

`internal/pipeline/examples_test.go` treats the examples as the end-to-end test
suite: it compares the emission and, when the toolchain is available, runs the
program and compares its stdout.

## Eloquent Ruby

| File | What it shows |
|---|---|
| `eloquent_ruby/01_document.enx` | classes, attributes and a computed value |
| `eloquent_ruby/02_inventory.enx` | hashes, iteration and anaphor-built data |
| `eloquent_ruby/03_prices.enx` | `select`, `map` and string interpolation |
| `eloquent_ruby/04_greeter.enx` | a module mixed into a class |
| `eloquent_ruby/05_symbols.enx` | symbol keys, sorting and minimum values |
| `eloquent_ruby/06_enumerable.enx` | `select`, `map` and `reduce` |
| `eloquent_ruby/07_case.enx` | `case`, `when` and ranges |
| `eloquent_ruby/08_exceptions.enx` | `begin`, `rescue` and `ensure` |
| `eloquent_ruby/09_lambda.enx` | lambdas and closures |
| `eloquent_ruby/10_text.enx` | splitting, mapping, joining and counting |
| `eloquent_ruby/11_prose_in_body.enx` | prose inside a class and a method body |

## Perl Cookbook

| File | What it shows |
|---|---|
| `perl_cookbook/01_price_report.enx` | subroutines, formatting and list operators |
| `perl_cookbook/02_catalogue.enx` | hashes, sorted keys and list context |
| `perl_cookbook/03_order_lines.enx` | references, nested arrays and dereferencing |
| `perl_cookbook/04_log_pattern.enx` | regular expressions and captures |
| `perl_cookbook/05_strings.enx` | trimming, substitutions and case changes |
| `perl_cookbook/06_arrays.enx` | sorting, indexing and slices through anaphora |
| `perl_cookbook/07_hashes_of_lists.enx` | hash values that are array references |
| `perl_cookbook/08_sorting.enx` | `grep`, `sort`, `join` and context |
| `perl_cookbook/09_nested_data.enx` | nested hashes and quoted subscript keys |
| `perl_cookbook/10_pattern_replace.enx` | non-destructive and in-place substitution |
| `perl_cookbook/11_prose_in_sub.enx` | prose inside a subroutine body |

## The C Programming Language

| File | What it shows |
|---|---|
| `c_programming_language/01_hello.enx` | the first program |
| `c_programming_language/02_word_count.enx` | a character walk and a word count |
| `c_programming_language/03_power.enx` | a function with a loop and a table |
| `c_programming_language/04_point.enx` | a structure and a function taking it by value |
| `c_programming_language/05_temperature.enx` | a conversion that reuses the previous result |
| `c_programming_language/06_char_frequency.enx` | an array histogram and anaphoric indexing |
| `c_programming_language/07_reverse.enx` | in-place string reversal through a pointer |
| `c_programming_language/08_bitcount.enx` | bit operations and a helper function |
| `c_programming_language/09_linked_list.enx` | structs, pointers and `malloc` |
| `c_programming_language/10_quicksort.enx` | recursion and an in-place array sort |
| `c_programming_language/11_prose_in_function.enx` | prose inside a function body |

## Showcase

`examples/showcase/` holds the larger mixed-prose programs: the Ruby greeting
and shop, the Perl shop and the C shop. They are the canonical end-to-end
fixtures, with the same `.expected` and `.stdout` sidecars as every other
example.

Each showcase language also has an `imports.enx` with a YAML frontmatter header
that pulls a helper from its `lib/` directory. Run these with `--anaphor '!'` as
well; the imported helper is textually prepended before the main file.

`examples/strict/clean.enx` is a small mixed file with no ambiguous decisions.
It is what the CI strict check and `task strict` exercise.
