// Package anaphor resolves anaphor markers such as !-1 to the name of an earlier
// assignment.
//
// An anaphor may only refer to an assignment, which is what keeps the referent a
// bare variable name. That in turn makes the whole feature a token substitution:
// the marker's bytes are replaced by a name the author already wrote, so nothing
// is re-evaluated, nothing is shadowed, and no synthesised name can collide.
// See SPEC section 18.
package anaphor

import (
	"sort"

	"enx/internal/core"
	"enx/internal/host"
)

// Binding is one assignment that an anaphor may reach.
type Binding struct {
	// Name is the assigned variable, as it will appear in the output.
	Name string
	// Type is the type the host wrote for this assignment, or empty when the
	// host did not write one. A typed anaphor can only reach a binding whose
	// Type is set.
	Type string
	// Pointer reports that the referent is a pointer. Only C distinguishes it.
	Pointer bool
	// Token is the index of the token that carries the name.
	Token int
	// Span covers the name token.
	Span core.Span
}

// AssignmentOp is in leaf.go.

// Result is the outcome of resolving a token stream.
type Result struct {
	toks []host.Token
	// Resolved maps an anaphor token index to the binding it names.
	Resolved map[int]Binding
	// Unresolved holds the token indices of anaphors that named nothing.
	Unresolved []int
	// Bindings is every assignment seen, in token order, for reporting.
	Bindings []Binding
}

// Substitute returns the text to emit for the token at index i: the referent's
// name when i is a resolved anaphor, and the token's own text otherwise.
func (r *Result) Substitute(i int) string {
	if b, ok := r.Resolved[i]; ok {
		return b.Name
	}
	return r.toks[i].Text
}

// SortedUnresolved returns the unresolved token indices in source order.
func (r *Result) SortedUnresolved() []int {
	out := make([]int, len(r.Unresolved))
	copy(out, r.Unresolved)
	sort.Ints(out)
	return out
}

// Resolve walks the kept tokens once, recording assignments and resolving
// markers against the assignments that precede them.
//
// kept must have one entry per token. Trivia is always traversed, because scope
// delimiters and statement boundaries live there, but it is never a binding.
//
// An anaphor that names nothing is left alone and reported, so the host
// compiler produces the error. Inventing one here would be guessing, and the
// host already diagnoses it better.
func Resolve(toks []host.Token, kept []bool, tracker host.ScopeTracker, types host.TypeReader) *Result {
	res := &Result{toks: toks, Resolved: make(map[int]Binding)}

	// One slice per open scope, each holding that scope's bindings in order.
	scopes := [][]Binding{{}}

	// pending holds bindings seen in the current statement, which become
	// visible only once the statement ends.
	var pending []Binding

	for i, tok := range toks {
		if kept != nil && !kept[i] && !tok.IsTrivia() {
			continue
		}

		if tracker != nil && !tok.IsTrivia() {
			if tracker.Closes(tok) && len(scopes) > 1 {
				scopes = scopes[:len(scopes)-1]
				continue
			}
			if tracker.Opens(tok) {
				scopes = append(scopes, nil)
				continue
			}
		}

		switch {
		case tok.Kind == host.Anaphor:
			if b, ok := resolveAt(toks, i, scopes); ok {
				res.Resolved[i] = b
			} else {
				res.Unresolved = append(res.Unresolved, i)
			}
		case isNameToken(tok) && assignmentTarget(toks, kept, i):
			// Record the binding only after the assignment's own statement has
			// been passed, so a statement cannot see its own left hand side. The
			// deferral is what stops "!-1" in "total = !-1" resolving to "total".
			b := Binding{Name: tok.Text, Token: i, Span: tok.Span}
			if types != nil {
				if eq := eqAfter(toks, kept, i); eq >= 0 {
					if name, pointer, ok := types.AssignedType(toks, i, eq); ok {
						b.Type, b.Pointer = name, pointer
					}
				}
			}
			pending = append(pending, b)
		}

		// Flush deferred bindings at a statement boundary.
		if len(pending) > 0 && (tok.Kind == host.Newline || tok.Text == ";") {
			scopes[len(scopes)-1] = append(scopes[len(scopes)-1], pending...)
			res.Bindings = append(res.Bindings, pending...)
			pending = pending[:0]
		}
	}
	if len(pending) > 0 {
		scopes[len(scopes)-1] = append(scopes[len(scopes)-1], pending...)
		res.Bindings = append(res.Bindings, pending...)
	}
	return res
}

// resolveAt resolves the marker at token index i against the visible bindings,
// most recent first.
//
// A typed marker filters the history before indexing, so "!-1:Point" is the most
// recent Point and "!-2:Point" the Point before it. Filtering first is what makes
// the two forms compose; indexing first would make the digit count every
// binding, typed or not, which is not what "the previous Point" means.
func resolveAt(toks []host.Token, i int, scopes [][]Binding) (Binding, bool) {
	ref, ok := host.ParseAnaphor(toks[i].Text)
	if !ok || ref.Position < 1 {
		return Binding{}, false
	}
	pos := ref.Position
	// Innermost scope first, and most recent first within each scope.
	n := 0
	for s := len(scopes) - 1; s >= 0; s-- {
		b := scopes[s]
		for j := len(b) - 1; j >= 0; j-- {
			if !typeMatches(ref.Type, b[j]) {
				continue
			}
			n++
			if n == pos {
				return b[j], true
			}
		}
	}
	return Binding{}, false
}

// typeMatches is in leaf.go.

// assignmentTarget reports whether the name at index i is the target of an
// assignment, as opposed to a name being read.
//
// This is the whole reason only assignments enter the history: a name being
// assigned is a result, while a name being read is not. The distinction is
// syntactic and local, and it also keeps a statement's own left hand side out of
// reach of its right hand side, so "total = !-1" cannot resolve !-1 to "total"
// itself.
//
// Only bare names count. An attribute target such as "obj.attr = 1" or
// "p->field = 1" is rejected: substituting "attr" for a later anaphor would
// not name the value that was assigned.
func assignmentTarget(toks []host.Token, kept []bool, i int) bool {
	if isAttributeTarget(toks, kept, i) {
		return false
	}

	next := nextKept(toks, kept, i)
	if next < 0 {
		return false
	}
	if AssignmentOp(toks[next].Text) {
		return true
	}
	if toks[next].Text != "," {
		return false
	}

	// A comma list before an assignment, as in "a, b = 1, 2". The comma must
	// be on the same line as the name: a comma on a following line is prose
	// punctuation far more often than it is a multiple assignment.
	if toks[next].Span.StartPos.Line != toks[i].Span.StartPos.Line {
		return false
	}
	// A comma list inside a call or subscript is an argument list, not a
	// multiple assignment. "f(a, b = 1)" assigns b but merely reads a.
	if enclosingGroupDepth(toks, kept, i) > 0 {
		return false
	}
	for j := next; j < len(toks); j++ {
		t := toks[j]
		// Check the newline before IsTrivia: a newline is trivia, so the
		// boundary test would otherwise be skipped and the scan could cross
		// into a later statement.
		if t.Kind == host.Newline || t.Text == ";" {
			return false
		}
		if t.IsTrivia() {
			continue
		}
		if kept != nil && !kept[j] {
			continue
		}
		if AssignmentOp(t.Text) {
			return true
		}
	}
	return false
}

// isAttributeTarget reports whether the name at i is written after a member
// separator, as in "obj.attr", "A::B", "p->field" or "x&.y".
func isAttributeTarget(toks []host.Token, kept []bool, i int) bool {
	prev := prevKept(toks, kept, i)
	if prev < 0 {
		return false
	}
	switch toks[prev].Text {
	case ".", "&.", "::", "->":
		return true
	}
	return false
}

// enclosingGroupDepth returns the number of unclosed "(" and "[" tokens before
// i. Braces are deliberately excluded: a multiple assignment inside a block is
// still a statement-level assignment, while one inside a call's parentheses is
// usually an argument list.
func enclosingGroupDepth(toks []host.Token, kept []bool, i int) int {
	depth := 0
	for j := 0; j < i; j++ {
		if toks[j].IsTrivia() {
			continue
		}
		if kept != nil && !kept[j] {
			continue
		}
		switch toks[j].Text {
		case "(", "[":
			depth++
		case ")", "]":
			if depth > 0 {
				depth--
			}
		}
	}
	return depth
}

// prevKept returns the previous significant kept token index before i, or -1.
func prevKept(toks []host.Token, kept []bool, i int) int {
	for j := i - 1; j >= 0; j-- {
		if toks[j].IsTrivia() {
			continue
		}
		if kept != nil && !kept[j] {
			continue
		}
		return j
	}
	return -1
}

// nextKept returns the next significant kept token index after i, or -1.
func nextKept(toks []host.Token, kept []bool, i int) int {
	for j := i + 1; j < len(toks); j++ {
		if toks[j].IsTrivia() {
			continue
		}
		if kept != nil && !kept[j] {
			continue
		}
		return j
	}
	return -1
}

// isNameToken is in leaf.go.

// eqAfter returns the index of a plain "=" that follows the name at i, or -1.
//
// Compound assignments are deliberately excluded. They do create a result, but
// their right hand side does not declare the variable's type: "x = 1.5;
// x += 1" must not re-type x as an Integer.
func eqAfter(toks []host.Token, kept []bool, i int) int {
	next := nextKept(toks, kept, i)
	if next < 0 {
		return -1
	}
	if toks[next].Text == "=" {
		return next
	}
	return -1
}
