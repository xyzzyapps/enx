package core

import "testing"

func TestSourceLineIndex(t *testing.T) {
	src := NewSource("a.txt", []byte("one\ntwo\nthree"))
	if src.LineCount() != 3 {
		t.Fatalf("LineCount = %d, want 3", src.LineCount())
	}
	if got := src.LineText(1); got != "one" {
		t.Errorf("LineText(1) = %q", got)
	}
	if got := src.LineText(3); got != "three" {
		t.Errorf("LineText(3) = %q", got)
	}
	if got := src.LineOf(4); got != 2 {
		t.Errorf("LineOf(4) = %d, want 2", got)
	}
}

func TestTrailingNewlineDoesNotAddALine(t *testing.T) {
	src := NewSource("a.txt", []byte("one\n"))
	if src.LineCount() != 1 {
		t.Fatalf("LineCount = %d, want 1", src.LineCount())
	}
	if got := src.LineText(1); got != "one" {
		t.Errorf("LineText(1) = %q", got)
	}
}

func TestCRLFLineText(t *testing.T) {
	src := NewSource("a.txt", []byte("one\r\ntwo\r\n"))
	if got := src.LineText(1); got != "one" {
		t.Errorf("LineText(1) = %q, want %q", got, "one")
	}
	if got := src.LineText(2); got != "two" {
		t.Errorf("LineText(2) = %q, want %q", got, "two")
	}
}

func TestPosIsOneBased(t *testing.T) {
	src := NewSource("a.txt", []byte("abc\ndef"))
	if got := src.Pos(0); got.Line != 1 || got.Col != 1 {
		t.Errorf("Pos(0) = %v, want 1:1", got)
	}
	if got := src.Pos(5); got.Line != 2 || got.Col != 2 {
		t.Errorf("Pos(5) = %v, want 2:2", got)
	}
}

func TestPosCountsRunesNotBytes(t *testing.T) {
	// "é" is two bytes and one rune.
	src := NewSource("a.txt", []byte("é x"))
	if got := src.Pos(len([]byte("é "))); got.Col != 3 {
		t.Errorf("Pos = %v, want column 3", got)
	}
}

func TestSpanClamping(t *testing.T) {
	src := NewSource("a.txt", []byte("abc"))
	cases := []struct {
		start, end int
		wantStart  int
		wantEnd    int
	}{
		{-5, 2, 0, 2},
		{1, 99, 1, 3},
		{2, 1, 2, 2},
		{0, 0, 0, 0},
	}
	for _, c := range cases {
		sp := src.Span(c.start, c.end)
		if sp.Start != c.wantStart || sp.End != c.wantEnd {
			t.Errorf("Span(%d,%d) = %v, want [%d,%d)", c.start, c.end, sp, c.wantStart, c.wantEnd)
		}
	}
}

func TestSpanHelpers(t *testing.T) {
	src := NewSource("a.txt", []byte("abcdef"))
	sp := src.Span(1, 4)
	if sp.Len() != 3 {
		t.Errorf("Len = %d, want 3", sp.Len())
	}
	if sp.Empty() {
		t.Error("span should not be empty")
	}
	if !sp.Contains(2) || sp.Contains(4) {
		t.Error("Contains is not half-open")
	}
	if got := src.Slice(sp); got != "bcd" {
		t.Errorf("Slice = %q", got)
	}
}

func TestSinkCountsAndFormat(t *testing.T) {
	src := NewSource("a.txt", []byte("one\ntwo\n"))
	var sink Sink
	sp := src.Span(4, 7)

	sink.Errorf("ambiguous-span", src, sp, "cannot decide %q", "two")
	sink.Warnf("kept-as-code", src, sp, "kept")
	sink.Infof("note", src, sp, "note")

	if !sink.HasErrors() {
		t.Fatal("HasErrors should be true")
	}
	info, warn, errs := sink.Counts()
	if info != 1 || warn != 1 || errs != 1 {
		t.Fatalf("counts = %d,%d,%d, want 1,1,1", info, warn, errs)
	}
	if sink.Len() != 3 {
		t.Fatalf("Len = %d, want 3", sink.Len())
	}

	rendered := sink.Diagnostics()[0].String()
	for _, want := range []string{"a.txt:2:1", "error", "ambiguous-span", `cannot decide "two"`} {
		if !contains(rendered, want) {
			t.Errorf("diagnostic %q does not contain %q", rendered, want)
		}
	}
}

func TestSinkHint(t *testing.T) {
	src := NewSource("a.txt", []byte("x"))
	var sink Sink
	sink.ReportHint(SevError, "ambiguous-span", src, src.Span(0, 1), "cannot decide", "wrap it in ~ ~")
	if !contains(sink.Diagnostics()[0].String(), "hint: wrap it in ~ ~") {
		t.Fatalf("hint missing: %q", sink.Diagnostics()[0].String())
	}
}

func TestNilSourceDiagnostic(t *testing.T) {
	var sink Sink
	sink.Errorf("config", nil, Span{}, "bad configuration")
	if got := sink.Diagnostics()[0].File; got != "" {
		t.Fatalf("File = %q, want empty", got)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestParseLevel(t *testing.T) {
	cases := map[string]bool{
		"":      true,
		"info":  true,
		"debug": true,
		"OFF":   true,
		"bogus": false,
	}
	for name, ok := range cases {
		_, err := ParseLevel(name)
		if (err == nil) != ok {
			t.Errorf("ParseLevel(%q) error = %v, want ok=%v", name, err, ok)
		}
	}
}
