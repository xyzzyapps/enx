#!/usr/bin/env perl
# tools/gen_wordlist.pl
#
# Generates the English word-frequency lexicon used by enx's code-switching
# decoder (internal/codeswitch).
#
# INPUT
#   Peter Norvig's count_1w.txt, derived from the Google Web Trillion Word
#   Corpus: 333,333 lines of "word<TAB>count", ordered by descending count.
#   Source: https://norvig.com/ngrams/count_1w.txt
#
# OUTPUT
#   A two-column TSV: "word<TAB>count", one entry per line, ascending byte
#   order is not guaranteed (input order is descending count).
#
# FILTERING RULES
#   - word must match ^[a-z]+$   (drops digits, punctuation, apostrophes,
#                                 mixed case, and non-ASCII)
#   - length >= 2                (drops single letters)
#   - length <= 24               (drops concatenated corpus artefacts)
#
# USAGE
#   perl tools/gen_wordlist.pl <count_1w.txt> <out.tsv>
#
# The resulting TSV is gzipped by the Taskfile into data/english_freq.tsv.gz,
# which is embedded into the enx binary via go:embed. The plain TSV is not
# committed.

use strict;
use warnings;
use utf8;

my ($in, $out) = @ARGV;
die "usage: $0 <count_1w.txt> <out.tsv>\n" unless defined $in && defined $out;

open my $ifh, '<', $in or die "cannot read $in: $!\n";
open my $ofh, '>:encoding(ascii)', $out or die "cannot write $out: $!\n";

my $kept   = 0;
my $read   = 0;
my $skipped = 0;

while (my $line = <$ifh>) {
    $read++;
    chomp $line;
    next if $line eq '';

    my ($word, $count) = split /\t/, $line, 2;
    if (!defined $count) { $skipped++; next; }

    if ($word !~ /\A[a-z]+\z/ || length($word) < 2 || length($word) > 24) {
        $skipped++;
        next;
    }

    print {$ofh} $word, "\t", $count, "\n";
    $kept++;
}

close $ofh;
close $ifh;

print STDERR "gen_wordlist: read=$read kept=$kept skipped=$skipped -> $out\n";
