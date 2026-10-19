package host

import "testing"

func TestMatchAnaphor(t *testing.T) {
	cases := []struct {
		text   string
		at     int
		marker byte
		want   int
		ok     bool
	}{
		// Ordinal forms.
		{"!-1", 0, '!', 3, true},
		{"!-12", 0, '!', 4, true},
		{"x = !-2 + 1", 4, '!', 7, true},
		{"!-0", 0, '!', 3, true}, // parses; position zero resolves to nothing

		// Type forms.
		{"!Point", 0, '!', 6, true},
		{"!A::B", 0, '!', 5, true},
		{"!char*", 0, '!', 6, true},
		{"!-1:Point", 0, '!', 9, true},
		{"!-2:A::B*", 0, '!', 9, true},

		// Not markers.
		{"!1", 0, '!', 0, false},      // no minus: logical-not, not an ordinal
		{"!-1x", 0, '!', 0, false},    // runs into a name
		{"!-", 0, '!', 0, false},      // no digits
		{"!-1:", 0, '!', 0, false},    // empty type
		{"!", 0, '!', 0, false},       //
		{"!A::", 0, '!', 0, false},    // dangling namespace separator
		{"!-1:A::", 0, '!', 0, false}, // dangling namespace separator
		{"!-1", 0, 0, 0, false},       // no marker claimed
		{"!-1", 0, '*', 0, false},     // a different marker
		{"@-1", 0, '@', 3, true},      // a configurable marker
	}

	for _, c := range cases {
		got, ok := MatchAnaphor([]byte(c.text), c.at, c.marker)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("MatchAnaphor(%q, %d, %q) = %d, %v; want %d, %v",
				c.text, c.at, c.marker, got, ok, c.want, c.ok)
		}
	}
}

func TestParseAnaphor(t *testing.T) {
	cases := map[string]struct {
		pos int
		typ string
		ok  bool
	}{
		"!-1":       {1, "", true},
		"!-2":       {2, "", true},
		"!-12":      {12, "", true},
		"!Point":    {1, "Point", true},
		"!A::B":     {1, "A::B", true},
		"!-1:Point": {1, "Point", true},
		"!-2:Point": {2, "Point", true},
		"!-1:char*": {1, "char*", true},
		"!-3:A::B*": {3, "A::B*", true},
		"!-0":       {0, "", false}, // position zero is not a position
		"!":         {0, "", false},
		"!-1:":      {0, "", false},
		"":          {0, "", false},
		"!x":        {1, "x", true}, // a type named x is still a type
	}
	for text, c := range cases {
		got, ok := ParseAnaphor(text)
		if ok != c.ok {
			t.Errorf("ParseAnaphor(%q) ok = %v, want %v", text, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if got.Position != c.pos || got.Type != c.typ {
			t.Errorf("ParseAnaphor(%q) = {%d, %q}, want {%d, %q}",
				text, got.Position, got.Type, c.pos, c.typ)
		}
	}
}

// TestParseAnaphorSaturates keeps a huge ordinal from overflowing into a
// plausible-looking small one.
func TestParseAnaphorSaturates(t *testing.T) {
	got, ok := ParseAnaphor("!-999999999999999999999")
	if !ok {
		t.Fatal("expected an anaphor")
	}
	if got.Position <= 0 {
		t.Fatalf("saturated position = %d, want positive", got.Position)
	}
}

func TestMatchAnaphorAtEndOfInput(t *testing.T) {
	for _, text := range []string{"!-1", "!Point", "!-1:Point", "!-1:char*"} {
		if _, ok := MatchAnaphor([]byte(text), 0, '!'); !ok {
			t.Fatalf("%q should match at end of input", text)
		}
	}
	if _, ok := MatchAnaphor([]byte(""), 0, '!'); ok {
		t.Fatal("empty input must not match")
	}
}
