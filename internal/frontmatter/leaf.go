package frontmatter

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
)

// This file is the leaf layer: these functions use the standard library and
// their arguments only. Split and parse orchestrate them.

func readLine(data []byte, start int) ([]byte, int) {
	if start >= len(data) {
		return nil, len(data)
	}
	if i := bytes.IndexByte(data[start:], '\n'); i >= 0 {
		return data[start : start+i], start + i + 1
	}
	return data[start:], len(data)
}

func isDelimiter(line []byte) bool {
	text := strings.TrimSpace(string(line))
	return text == "---" || text == "..."
}

func stripComment(line string) string {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == '\\' && quote == '"' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '#':
			return line[:i]
		}
	}
	return line
}

func splitKey(line string) (key, value string, ok bool) {
	if i := strings.IndexByte(line, ':'); i >= 0 {
		return strings.TrimSpace(line[:i]), line[i+1:], true
	}
	return "", "", false
}

func leadingIndent(line string) int {
	n := 0
	for n < len(line) && line[n] == ' ' {
		n++
	}
	return n
}

func isIndented(line string) bool {
	return len(line) > 0 && (line[0] == ' ' || line[0] == '\t')
}

// splitList splits a comma-separated inline list without splitting commas
// inside quotes.
func splitList(s string) []string {
	var parts []string
	start := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' && quote == '"' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case ',':
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func unquote(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	if s[0] == '"' {
		if len(s) < 2 || s[len(s)-1] != '"' {
			return "", errors.New("unterminated double quote")
		}
		v, err := strconv.Unquote(s)
		if err != nil {
			return "", err
		}
		return v, nil
	}
	if s[0] == '\'' {
		if len(s) < 2 || s[len(s)-1] != '\'' {
			return "", errors.New("unterminated single quote")
		}
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
	}
	return s, nil
}
