package perl

import (
	"strings"
	"testing"

	"enx/internal/core"
)

// FuzzLexTotality is the lexer contract: every byte of the input belongs to
// exactly one token, for arbitrary input.
func FuzzLexTotality(f *testing.F) {
	f.Add([]byte("my $x = 1;\n"))
	f.Add([]byte("print \"total\\n\";\n"))
	f.Add([]byte("my $s = <<EOF;\nbody\nEOF\n"))
	f.Add([]byte("\xff\xfe not utf8\n"))

	f.Fuzz(func(t *testing.T, b []byte) {
		src := core.NewSource("fuzz.pl", b)
		toks, _ := New().Lex(src)
		var sb strings.Builder
		for _, tok := range toks {
			sb.WriteString(tok.Text)
		}
		if sb.String() != string(b) {
			t.Fatalf("token texts do not reproduce %q", b)
		}
	})
}
