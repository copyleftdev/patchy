package observability

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// LogConfig holds logging configuration.
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	Output string `yaml:"output"`
	File   string `yaml:"file"`
}

// LogCloser wraps a logger with an optional cleanup function for file handles.
type LogCloser struct {
	Logger *slog.Logger
	close  func() error
}

// Close releases any file handles opened by the logger.
func (lc *LogCloser) Close() error {
	if lc.close != nil {
		return lc.close()
	}
	return nil
}

// NewLogger creates a configured slog.Logger.
// The returned LogCloser must be closed to release file handles.
func NewLogger(cfg LogConfig) *LogCloser {
	level := parseLevel(cfg.Level)
	opts := &slog.HandlerOptions{Level: level}

	var writers []io.Writer
	var logFile *os.File

	switch strings.ToLower(cfg.Output) {
	case "file":
		if cfg.File != "" {
			f, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err == nil {
				writers = append(writers, f)
				logFile = f
			} else {
				writers = append(writers, os.Stderr)
			}
		} else {
			writers = append(writers, os.Stderr)
		}
	case "both":
		writers = append(writers, os.Stderr)
		if cfg.File != "" {
			f, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err == nil {
				writers = append(writers, f)
				logFile = f
			}
		}
	default: // stderr
		writers = append(writers, os.Stderr)
	}

	var w io.Writer
	if len(writers) == 1 {
		w = writers[0]
	} else {
		w = io.MultiWriter(writers...)
	}

	var handler slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "text":
		handler = slog.NewTextHandler(w, opts)
	default: // json
		handler = slog.NewJSONHandler(w, opts)
	}

	lc := &LogCloser{Logger: slog.New(handler)}
	if logFile != nil {
		lc.close = logFile.Close
	}
	return lc
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
