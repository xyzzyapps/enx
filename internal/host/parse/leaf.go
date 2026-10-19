package parse

// This file is the leaf layer: result algebra and comparison. Parser, Body and
// Program are the integrators.

// LastKept returns the final kept token index, or -1.
func (r Result) LastKept() int {
	if len(r.Kept) == 0 {
		return -1
	}
	return r.Kept[len(r.Kept)-1]
}

// Combine concatenates two successful results.
func Combine(a, b Result) Result {
	if !a.OK || !b.OK {
		return Fail
	}
	kept := make([]int, 0, len(a.Kept)+len(b.Kept))
	kept = append(kept, a.Kept...)
	kept = append(kept, b.Kept...)
	ends := make([]int, 0, len(a.Ends)+len(b.Ends))
	ends = append(ends, a.Ends...)
	ends = append(ends, b.Ends...)
	return Result{OK: true, Next: b.Next, Cost: a.Cost + b.Cost, Kept: kept, Ends: ends}
}

// Better reports whether candidate b should replace the current best a.
//
// On equal cost the longer parse wins. That is the objective the project agreed
// on — take the longest sequence of code as code — rather than a tie-break of
// convenience.
func Better(a, b Result) bool {
	if !b.OK {
		return false
	}
	if !a.OK {
		return true
	}
	if b.Cost != a.Cost {
		return b.Cost < a.Cost
	}
	return b.Next > a.Next
}
