package host

import "testing"

// FuzzMatchAnaphor keeps the marker matcher total for every byte string and
// every offset. It is cheap insurance for the end-of-input paths, where a
// bounds mistake is invisible until a file happens to end in the wrong shape.
func FuzzMatchAnaphor(f *testing.F) {
	f.Add([]byte("!-1"), byte('!'), 0)
	f.Add([]byte("!-1:A::"), byte('!'), 0)
	f.Add([]byte("!Point"), byte('!'), 0)
	f.Add([]byte(""), byte('!'), 0)

	f.Fuzz(func(t *testing.T, b []byte, marker byte, at int) {
		end, ok := MatchAnaphor(b, at, marker)
		if !ok {
			return
		}
		if end <= at || end > len(b) {
			t.Fatalf("MatchAnaphor(%q, %d, %q) = %d, true; offset is outside the input",
				b, at, marker, end)
		}
	})
}

// FuzzMatchEscape keeps the escape matcher total and its offsets inside the
// input, including unterminated tildes and CRLF.
func FuzzMatchEscape(f *testing.F) {
	f.Add([]byte("~ prose ~"), 0)
	f.Add([]byte("x = ~y + ~z;"), 4)
	f.Add([]byte("~ open\nno close ~"), 0)

	f.Fuzz(func(t *testing.T, b []byte, at int) {
		end, ok := MatchEscape(b, at)
		if !ok {
			return
		}
		if end <= at || end > len(b) {
			t.Fatalf("MatchEscape(%q, %d) = %d, true; offset is outside the input", b, at, end)
		}
	})
}
