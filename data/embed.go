// Package data holds read-only assets embedded into the enx binary.
//
// Assets live here, rather than being read from disk at run time, so that a
// single self-contained executable is enough to run the tool.
package data

import _ "embed"

// EnglishFreqGz is a gzip-compressed, tab-separated English word-frequency
// table with one "word<TAB>count" record per line, ordered by descending
// count.
//
// Source: Peter Norvig's count_1w.txt, derived from the Google Web Trillion
// Word Corpus. Regenerate with tools/gen_wordlist.pl; see Taskfile.yml.
//
//go:embed english_freq.tsv.gz
var EnglishFreqGz []byte
