package logger

import (
	"fmt"
	"log"
	"os"
	"sync/atomic"
)

// Severity levels for structured logging.
const (
	LevelDebug = "DEBUG"
	LevelInfo  = "INFO"
	LevelWarn  = "WARN"
	LevelError = "ERROR"
)

// global counter for log sequence numbers (useful for ordering in async scenarios)
var seq uint64

// Fields carries structured context attached to a log line.
type Fields struct {
	RequestID string
	OrgID     uint
	ChannelID uint
	UserID    uint
	Module    string
}

// Logger is a lightweight structured logger that emits key=value lines via the
// standard library logger so we don't introduce external dependencies.
type Logger struct {
	fields Fields
}

// New creates a logger pre-loaded with the given base fields.
func New(f Fields) *Logger {
	return &Logger{fields: f}
}

// basePrefix builds the common prefix: [seq request_id=... org_id=... ...]
func (l *Logger) basePrefix(level string) string {
	n := atomic.AddUint64(&seq, 1)
	prefix := fmt.Sprintf("[%d] %s", n, level)
	if l.fields.RequestID != "" {
		prefix += fmt.Sprintf(" request_id=%s", l.fields.RequestID)
	}
	if l.fields.OrgID > 0 {
		prefix += fmt.Sprintf(" org_id=%d", l.fields.OrgID)
	}
	if l.fields.ChannelID > 0 {
		prefix += fmt.Sprintf(" channel_id=%d", l.fields.ChannelID)
	}
	if l.fields.UserID > 0 {
		prefix += fmt.Sprintf(" user_id=%d", l.fields.UserID)
	}
	if l.fields.Module != "" {
		prefix += fmt.Sprintf(" module=%s", l.fields.Module)
	}
	return prefix
}

// Debug logs a debug message.
func (l *Logger) Debug(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("%s %s", l.basePrefix(LevelDebug), msg)
}

// Info logs an informational message.
func (l *Logger) Info(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("%s %s", l.basePrefix(LevelInfo), msg)
}

// Warn logs a warning message.
func (l *Logger) Warn(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("%s %s", l.basePrefix(LevelWarn), msg)
}

// Error logs an error message.
func (l *Logger) Error(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("%s %s", l.basePrefix(LevelError), msg)
}

// With returns a child logger that inherits base fields and adds/overrides
// the given fields.  Useful when you have extra context mid-request (e.g. after
// resolving a channel).
func (l *Logger) With(f Fields) *Logger {
	child := l.fields
	if f.RequestID != "" {
		child.RequestID = f.RequestID
	}
	if f.OrgID > 0 {
		child.OrgID = f.OrgID
	}
	if f.ChannelID > 0 {
		child.ChannelID = f.ChannelID
	}
	if f.UserID > 0 {
		child.UserID = f.UserID
	}
	if f.Module != "" {
		child.Module = f.Module
	}
	return &Logger{fields: child}
}

// SetOutput is a convenience for tests; it redirects the standard logger.
// In production the default stderr output is fine.
func SetOutput(f *os.File) {
	log.SetOutput(f)
}
