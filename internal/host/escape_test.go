package host

import "testing"

func TestMatchEscape(t *testing.T) {
	cases := []struct {
		text string
		at   int
		want int
		ok   bool
	}{
		// An escape: tildes standing alone between whitespace, closed on the
		// same line.
		{"~ the total ~", 0, 13, true},
		{"x = 5 ~ a note ~ y = 6", 6, 16, true},
		{"~a~", 0, 0, false},     // neither tilde stands alone
		{"x = ~y;", 4, 0, false}, // no closing tilde
		{"~ open\nno close ~", 0, 0, false},
		{"x = ~y + ~z;", 4, 0, false}, // two tildes, neither stands alone
		{"~ a ~b", 0, 0, false},       // closing tilde is glued to b
		{"a~ b ~", 1, 0, false},       // opening tilde is glued to a
	}

	for _, c := range cases {
		got, ok := MatchEscape([]byte(c.text), c.at)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("MatchEscape(%q, %d) = %d, %v; want %d, %v", c.text, c.at, got, ok, c.want, c.ok)
		}
	}
}

// TestMatchEscapeAtStartOfFile covers the boundary: there is no preceding byte
// to check.
func TestMatchEscapeAtStartOfFile(t *testing.T) {
	got, ok := MatchEscape([]byte("~ prose here ~"), 0)
	if !ok || got != 14 {
		t.Fatalf("got %d, %v", got, ok)
	}
}

// TestMatchEscapeIgnoresNonTilde keeps the function total for any offset, so a
// lexer can call it speculatively.
func TestMatchEscapeIgnoresNonTilde(t *testing.T) {
	for _, text := range []string{"", "a", "x = 5"} {
		for i := 0; i <= len(text); i++ {
			if _, ok := MatchEscape([]byte(text), i); ok {
				t.Fatalf("MatchEscape(%q, %d) claimed an escape", text, i)
			}
		}
	}
	if _, ok := MatchEscape([]byte("~ x ~"), -1); ok {
		t.Fatal("a negative offset must not claim an escape")
	}
}
