package core

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// LogConfig configures the structured logger used for tracing, as opposed to
// the Sink, which carries user-facing diagnostics.
type LogConfig struct {
	// Level is the minimum level to emit.
	Level slog.Level
	// JSON selects JSON output instead of text.
	JSON bool
	// Writer receives the log. A nil Writer discards everything.
	Writer io.Writer
}

// NoLogLevel silences the logger.
const NoLogLevel = slog.Level(127)

// NewLogger builds a logger from cfg.
func NewLogger(cfg LogConfig) *slog.Logger {
	if cfg.Writer == nil {
		cfg.Writer = io.Discard
	}
	opts := &slog.HandlerOptions{Level: cfg.Level}
	var h slog.Handler
	if cfg.JSON {
		h = slog.NewJSONHandler(cfg.Writer, opts)
	} else {
		h = slog.NewTextHandler(cfg.Writer, opts)
	}
	return slog.New(h)
}

// ParseLevel parses a log level name. An empty string means "info" and "off"
// silences the logger.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug", "trace":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	case "off", "none", "silent":
		return NoLogLevel, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level %q", s)
	}
}
