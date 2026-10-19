package parse

import (
	"math"
	"testing"

	"enx/internal/host"
)

func TestCombineConcatenatesInOrder(t *testing.T) {
	a := Result{OK: true, Next: 3, Cost: 1.5, Kept: []int{0, 2}, Ends: []int{2}}
	b := Result{OK: true, Next: 7, Cost: 2.5, Kept: []int{4}, Ends: []int{4}}

	got := Combine(a, b)
	if !got.OK {
		t.Fatal("Combine should succeed")
	}
	if got.Next != 7 {
		t.Errorf("Next = %d, want 7", got.Next)
	}
	if got.Cost != 4.0 {
		t.Errorf("Cost = %v, want 4", got.Cost)
	}
	wantKept := []int{0, 2, 4}
	if len(got.Kept) != len(wantKept) {
		t.Fatalf("Kept = %v, want %v", got.Kept, wantKept)
	}
	for i := range wantKept {
		if got.Kept[i] != wantKept[i] {
			t.Fatalf("Kept = %v, want %v", got.Kept, wantKept)
		}
	}
	if len(got.Ends) != 2 || got.Ends[0] != 2 || got.Ends[1] != 4 {
		t.Fatalf("Ends = %v, want [2 4]", got.Ends)
	}
}

// TestCombineDoesNotAlias guards against one result mutating another through a
// shared backing array, which would corrupt a memoised parse.
func TestCombineDoesNotAlias(t *testing.T) {
	a := Result{OK: true, Kept: []int{1}, Ends: []int{1}}
	b := Result{OK: true, Kept: []int{2}, Ends: []int{2}}
	got := Combine(a, b)
	got.Kept[0] = 99
	got.Ends[0] = 99
	if a.Kept[0] != 1 || a.Ends[0] != 1 {
		t.Fatal("Combine aliased its input")
	}
}

func TestCombineFailsIfEitherFails(t *testing.T) {
	ok := Result{OK: true, Next: 1}
	if Combine(ok, Fail).OK || Combine(Fail, ok).OK {
		t.Fatal("Combine must fail when either side fails")
	}
}

func TestBetterPrefersLowerCost(t *testing.T) {
	cheap := Result{OK: true, Next: 1, Cost: 1}
	dear := Result{OK: true, Next: 5, Cost: 9}
	if !Better(dear, cheap) {
		t.Error("the cheaper parse should win even when shorter")
	}
	if Better(cheap, dear) {
		t.Error("the dearer parse should not win")
	}
}

// TestBetterPrefersLongerOnTie pins the stated objective: on equal cost, take
// the longest sequence of code as code.
func TestBetterPrefersLongerOnTie(t *testing.T) {
	short := Result{OK: true, Next: 3, Cost: 2}
	long := Result{OK: true, Next: 9, Cost: 2}
	if !Better(short, long) {
		t.Error("on a tie the longer parse should win")
	}
	if Better(long, short) {
		t.Error("the shorter parse should not displace a longer one at equal cost")
	}
}

func TestBetterFromNothing(t *testing.T) {
	if !Better(Fail, Result{OK: true, Cost: math.Inf(1)}) {
		t.Error("any success should beat fail")
	}
	if Better(Result{OK: true}, Fail) {
		t.Error("fail should not displace a success")
	}
}

func TestLastKept(t *testing.T) {
	if got := (Result{}).LastKept(); got != -1 {
		t.Errorf("empty LastKept = %d, want -1", got)
	}
	if got := (Result{Kept: []int{3, 7, 8}}).LastKept(); got != 8 {
		t.Errorf("LastKept = %d, want 8", got)
	}
}

func TestEndStatementRecordsTheLastKeptToken(t *testing.T) {
	r := Result{OK: true, Kept: []int{2, 5}, Ends: []int{2}}
	got := r.EndStatement()
	if len(got.Ends) != 2 || got.Ends[1] != 5 {
		t.Fatalf("Ends = %v, want [2 5]", got.Ends)
	}
	// A result with nothing kept records nothing.
	empty := Result{OK: true}.EndStatement()
	if len(empty.Ends) != 0 {
		t.Fatalf("Ends = %v, want empty", empty.Ends)
	}
}

// bodyCosts makes skipping cheap and keeping expensive, so a greedy body would
// keep a prose word even though skipping it is cheaper.
type bodyCosts struct{}

func (bodyCosts) Keep(int) float64 { return 10 }
func (bodyCosts) Skip(int) float64 { return 1 }

// TestBodyComparesStatementAndSkip pins the prose-in-body rule: a word that can
// be a statement can still be skipped when the English reading is cheaper.
func TestBodyComparesStatementAndSkip(t *testing.T) {
	toks := []host.Token{
		{Kind: host.Identifier, Text: "word"},
		{Kind: host.Identifier, Text: "word"},
		{Kind: host.Identifier, Text: "word"},
	}
	p := New(toks, bodyCosts{}, nil)
	statement := func(pos int) Result {
		return Result{OK: true, Next: pos + 1, Cost: 20, Kept: []int{pos}}
	}
	got := p.Body(0, nil, 6, statement)
	if len(got.Kept) != 0 {
		t.Fatalf("kept %v, want an empty body", got.Kept)
	}
	if !got.OK || got.Next != len(toks) {
		t.Fatalf("result = %+v, want OK through the end", got)
	}
}

func TestBodyKeepsCodeBetweenProse(t *testing.T) {
	toks := []host.Token{
		{Kind: host.Identifier, Text: "word"},
		{Kind: host.Int, Text: "1"},
		{Kind: host.Identifier, Text: "word"},
	}
	costs := bodyCosts{}
	p := New(toks, costs, nil)
	statement := func(pos int) Result {
		if toks[pos].Text != "1" {
			return Fail
		}
		return Result{OK: true, Next: pos + 1, Cost: 0, Kept: []int{pos}}
	}
	got := p.Body(0, nil, 6, statement)
	if len(got.Kept) != 1 || got.Kept[0] != 1 {
		t.Fatalf("kept %v, want [1]", got.Kept)
	}
}

func TestPick(t *testing.T) {
	a := Result{OK: true, Next: 1, Cost: 1}
	b := Result{OK: true, Next: 2, Cost: 0}
	if got := Pick(a, b); got.Next != 2 {
		t.Fatalf("Pick = %v, want b", got)
	}
	if got := Pick(Fail, b); got.Next != 2 {
		t.Fatalf("Pick = %v, want b when a fails", got)
	}
}

func TestMatchText(t *testing.T) {
	tok := host.Token{Kind: host.Operator, Text: "="}
	if !MatchText(tok, []string{"=", "=="}) {
		t.Error("expected a match")
	}
	if MatchText(tok, []string{"==", "!="}) {
		t.Error("expected no match")
	}
	trivia := host.Token{Kind: host.Space, Text: " "}
	if MatchText(trivia, []string{" "}) {
		t.Error("trivia must never match")
	}
}

// TestMemoCachesAndGuardsReentry checks the two properties the languages rely
// on: a production is computed once per (slot, position, flag), and a slot
// re-entered while still computing is refused rather than allowed to loop.
func TestMemoCachesAndGuardsReentry(t *testing.T) {
	p := New(nil, nil, nil)

	calls := 0
	first := p.Memo(0, 0, false, func() Result { calls++; return Result{OK: true, Next: 1} })
	second := p.Memo(0, 0, false, func() Result { calls++; return Result{OK: true, Next: 1} })
	if calls != 1 {
		t.Fatalf("computed %d times, want 1", calls)
	}
	if first.Next != second.Next {
		t.Fatal("cached result differs")
	}

	// A different flag is a different key.
	p.Memo(0, 0, true, func() Result { calls++; return Result{OK: true} })
	if calls != 2 {
		t.Fatalf("computed %d times, want 2", calls)
	}

	// Re-entry is refused.
	var reentrant Result
	reentrant = p.Memo(1, 0, false, func() Result {
		inner := p.Memo(1, 0, false, func() Result { return Result{OK: true} })
		if inner.OK {
			t.Error("re-entry should have been refused")
		}
		return Result{OK: true, Next: 1}
	})
	if !reentrant.OK {
		t.Fatal("the outer computation should still succeed")
	}
}
