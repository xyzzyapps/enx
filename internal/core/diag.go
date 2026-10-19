package core

import (
	"fmt"
	"strings"
)

// Severity ranks a diagnostic.
type Severity uint8

const (
	// SevInfo is an advisory message.
	SevInfo Severity = iota
	// SevWarning signals a decision the user probably wants to know about.
	SevWarning
	// SevError signals a decision the tool refuses to make.
	SevError
)

// String renders the severity in lower case.
func (s Severity) String() string {
	switch s {
	case SevError:
		return "error"
	case SevWarning:
		return "warning"
	default:
		return "info"
	}
}

// Diagnostic is a user-facing message anchored to a source span.
//
// Code is a stable machine-readable identifier such as "ambiguous-span", so
// tests and editor integrations do not need to match on prose.
type Diagnostic struct {
	Severity Severity
	Code     string
	File     string
	Span     Span
	Message  string
	Hint     string
}

// String renders the diagnostic in compiler style.
func (d Diagnostic) String() string {
	var b strings.Builder
	if d.File != "" {
		b.WriteString(d.File)
		b.WriteByte(':')
	}
	b.WriteString(d.Span.StartPos.String())
	b.WriteString(": ")
	b.WriteString(d.Severity.String())
	if d.Code != "" {
		b.WriteString(" [")
		b.WriteString(d.Code)
		b.WriteString("]")
	}
	b.WriteString(": ")
	b.WriteString(d.Message)
	if d.Hint != "" {
		b.WriteString("\n    hint: ")
		b.WriteString(d.Hint)
	}
	return b.String()
}

// Sink accumulates diagnostics in insertion order.
//
// A Sink is not safe for concurrent use; the pipeline is single-threaded.
type Sink struct {
	diags  []Diagnostic
	counts [3]int
}

// Add appends a diagnostic.
func (s *Sink) Add(d Diagnostic) {
	s.diags = append(s.diags, d)
	if d.Severity < 3 {
		s.counts[d.Severity]++
	}
}

// Report appends a formatted diagnostic anchored to src and sp. A nil src
// leaves the file name empty, which is useful for configuration errors.
func (s *Sink) Report(sev Severity, code string, src *Source, sp Span, format string, args ...any) {
	name := ""
	if src != nil {
		name = src.Name
	}
	s.Add(Diagnostic{
		Severity: sev,
		Code:     code,
		File:     name,
		Span:     sp,
		Message:  fmt.Sprintf(format, args...),
	})
}

// ReportHint is Report with an attached hint.
func (s *Sink) ReportHint(sev Severity, code string, src *Source, sp Span, msg, hint string) {
	name := ""
	if src != nil {
		name = src.Name
	}
	s.Add(Diagnostic{
		Severity: sev,
		Code:     code,
		File:     name,
		Span:     sp,
		Message:  msg,
		Hint:     hint,
	})
}

// Errorf records an error.
func (s *Sink) Errorf(code string, src *Source, sp Span, format string, args ...any) {
	s.Report(SevError, code, src, sp, format, args...)
}

// Warnf records a warning.
func (s *Sink) Warnf(code string, src *Source, sp Span, format string, args ...any) {
	s.Report(SevWarning, code, src, sp, format, args...)
}

// Infof records an informational message.
func (s *Sink) Infof(code string, src *Source, sp Span, format string, args ...any) {
	s.Report(SevInfo, code, src, sp, format, args...)
}

// Diagnostics returns a copy of the collected diagnostics.
func (s *Sink) Diagnostics() []Diagnostic {
	out := make([]Diagnostic, len(s.diags))
	copy(out, s.diags)
	return out
}

// Len returns the number of collected diagnostics.
func (s *Sink) Len() int { return len(s.diags) }

// HasErrors reports whether any error was recorded.
func (s *Sink) HasErrors() bool { return s.counts[SevError] > 0 }

// HasWarnings reports whether any warning was recorded.
func (s *Sink) HasWarnings() bool { return s.counts[SevWarning] > 0 }

// Counts returns the number of diagnostics at each severity.
func (s *Sink) Counts() (info, warning, errors int) {
	return s.counts[SevInfo], s.counts[SevWarning], s.counts[SevError]
}
