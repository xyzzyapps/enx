// Package frontmatter reads the small YAML header that may preface an enx
// file.
//
// The supported subset is deliberately narrow: a leading "---" line, an
// optional imports key, and a closing "---" line. Unknown keys are ignored so a
// file can carry ordinary metadata without making the tool unusable. The
// imports value may be a scalar, an inline list, or a block list:
//
//	---
//	imports:
//	  - helpers.enx
//	  - lib/math.enx
//	---
//
// A file without frontmatter is returned unchanged.
package frontmatter

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

// Meta is the part of the header the tool currently understands.
type Meta struct {
	// Imports lists paths as written in the file. Resolution is the caller's
	// job, because only the caller knows the importing file's directory.
	Imports []string
}

// Split separates a leading YAML header from the body.
//
// The header is removed, not commented out: it is metadata, not part of the
// program. The body is returned byte for byte, starting immediately after the
// closing delimiter line.
func Split(data []byte) (Meta, []byte, error) {
	if len(data) == 0 {
		return Meta{}, data, nil
	}

	start := 0
	if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		start = 3
	}
	first, next := readLine(data, start)
	if !isDelimiter(first) {
		return Meta{}, data, nil
	}

	var header []string
	pos := next
	for pos < len(data) {
		line, after := readLine(data, pos)
		if isDelimiter(line) {
			meta, err := parse(header)
			if err != nil {
				return Meta{}, nil, err
			}
			return meta, data[after:], nil
		}
		header = append(header, strings.TrimSuffix(string(line), "\r"))
		pos = after
	}
	return Meta{}, nil, errors.New("frontmatter: unterminated YAML header")
}

func parse(lines []string) (Meta, error) {
	var meta Meta
	for i := 0; i < len(lines); i++ {
		line := stripComment(lines[i])
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := leadingIndent(line)
		key, value, ok := splitKey(line)
		if !ok || indent != 0 {
			continue
		}
		if key != "imports" {
			// Skip an unknown key's indented block.
			for i+1 < len(lines) && isIndented(lines[i+1]) {
				i++
			}
			continue
		}

		value = strings.TrimSpace(value)
		if value != "" {
			items, err := parseValue(value)
			if err != nil {
				return Meta{}, err
			}
			meta.Imports = append(meta.Imports, items...)
			continue
		}

		for i+1 < len(lines) {
			next := stripComment(lines[i+1])
			if strings.TrimSpace(next) == "" {
				i++
				continue
			}
			if !isIndented(next) && !strings.HasPrefix(strings.TrimSpace(next), "-") {
				break
			}
			dash := strings.TrimSpace(next)
			if !strings.HasPrefix(dash, "-") {
				i++
				continue
			}
			item, err := unquote(strings.TrimSpace(strings.TrimPrefix(dash, "-")))
			if err != nil || item == "" {
				return Meta{}, fmt.Errorf("frontmatter: invalid imports entry %q", dash)
			}
			meta.Imports = append(meta.Imports, item)
			i++
		}
	}
	return meta, nil
}

func parseValue(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "[") {
		if !strings.HasSuffix(value, "]") {
			return nil, fmt.Errorf("frontmatter: unterminated inline list %q", value)
		}
		inner := strings.TrimSpace(value[1 : len(value)-1])
		if inner == "" {
			return nil, nil
		}
		parts := splitList(inner)
		items := make([]string, 0, len(parts))
		for _, part := range parts {
			item, err := unquote(strings.TrimSpace(part))
			if err != nil || item == "" {
				return nil, fmt.Errorf("frontmatter: invalid imports entry %q", part)
			}
			items = append(items, item)
		}
		return items, nil
	}
	item, err := unquote(value)
	if err != nil || item == "" {
		return nil, fmt.Errorf("frontmatter: invalid imports value %q", value)
	}
	return []string{item}, nil
}
