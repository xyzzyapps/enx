package main

import (
	"fmt"
	"io"
	"strings"

	"enx/internal/codeswitch"
	"enx/internal/core"
)

// This file is the leaf layer: formatting, flag validation and path expansion.
// The command handlers are integrators.

func formatCost(c float64) string {
	if c == codeswitch.ImpossibleCost {
		return "inf"
	}
	return fmt.Sprintf("%.3f", c)
}

// reportDiagnostics prints warnings for commands whose stdout is the program or
// the decision table. check reports its own diagnostics, so it does not use
// this helper.
func reportDiagnostics(w io.Writer, diags []core.Diagnostic) {
	for _, d := range diags {
		fmt.Fprintln(w, d)
	}
}

// substitutePaths expands {src} and {out} in a compile command template.
func substitutePaths(args []string, src, out string) []string {
	expanded := make([]string, len(args))
	for i, a := range args {
		a = strings.ReplaceAll(a, "{src}", src)
		a = strings.ReplaceAll(a, "{out}", out)
		expanded[i] = a
	}
	return expanded
}

// anaphorMarker validates --anaphor and returns the claimed marker, or zero when
// anaphora is off.
//
// A marker that the host already uses for something else is rejected rather than
// accepted and then misread: "#" begins a comment in Ruby and Perl and a
// directive in C, so claiming it would make every comment an anaphor.
func (c *common) anaphorMarker() byte {
	if c.anaphor == "" {
		return 0
	}
	r := []rune(c.anaphor)
	if len(r) != 1 || r[0] > 127 {
		return 0
	}
	switch byte(r[0]) {
	case '#', ' ', '\t', '\n', '\r':
		return 0
	}
	return byte(r[0])
}

// flagConflict reports a flag that was accepted by the shared flag set but has
// no meaning for this command. Silently ignoring it is worse than refusing it.
func (c *common) flagConflict() string {
	switch c.fs.Name() {
	case "mix":
		return ""
	case "run":
		if c.out != "" {
			return "--out is only valid for mix, because run executes a temporary file"
		}
		return ""
	case "label", "check":
		if c.out != "" {
			return "--out is only valid for mix"
		}
		if c.keepProse {
			return "--keep-prose is only valid for mix and run"
		}
		if c.provisional {
			return "--provisional is only valid for mix and run; label and check always inspect"
		}
		return ""
	}
	return ""
}
