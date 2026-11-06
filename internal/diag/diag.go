// Package diag logs every external call for -v and DROVER_LOG=debug, redacting env assignments so secrets never show.
package diag

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	debugFile = "debug.log"
	dirMode   = 0o700
	fileMode  = 0o600
)

// Options configures a Logger.
type Options struct {
	Verbose  bool                // -v: log each call to Stderr
	Stderr   io.Writer           // destination of -v lines
	StateDir string              // debug.log lives here; created with MkdirAll when debug is on
	Getenv   func(string) string // DROVER_LOG=debug enables <StateDir>/debug.log
	Now      func() time.Time    // clock; Exec calls it exactly twice per call (start, end)
}

// Logger records external calls. Callers that want logging optional keep a nil check themselves.
type Logger struct {
	mu      sync.Mutex
	verbose bool
	stderr  io.Writer
	now     func() time.Time
	file    *os.File
}

// New builds a Logger, opening <StateDir>/debug.log (0600, dir 0700) when DROVER_LOG=debug.
func New(o Options) (*Logger, error) {
	l := &Logger{verbose: o.Verbose, stderr: o.Stderr, now: o.Now}
	if l.now == nil {
		l.now = time.Now
	}
	if o.Getenv != nil && o.Getenv("DROVER_LOG") == "debug" {
		if err := os.MkdirAll(o.StateDir, dirMode); err != nil {
			return nil, fmt.Errorf("create state dir: %w", err)
		}
		path := filepath.Join(o.StateDir, debugFile)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, fileMode)
		if err != nil {
			return nil, fmt.Errorf("open debug log: %w", err)
		}
		l.file = f
	}

	return l, nil
}

// Close closes the debug log. It is safe to call twice.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil

	return err
}

// Exec runs fn and logs `<base(bin)> <args> (<duration>)`. fn's error is returned unchanged; failures are logged too.
// Only the redacted command line is ever written, never fn's error text.
func (l *Logger) Exec(bin string, args []string, fn func() error) error {
	start := l.now()
	err := fn()
	took := l.now().Sub(start).Round(time.Millisecond)
	line := filepath.Base(bin) + " " + FormatArgs(args) + " (" + took.String() + ")"

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.verbose && l.stderr != nil {
		fmt.Fprintln(l.stderr, line)
	}
	if l.file != nil {
		status := "ok"
		if err != nil {
			status = "failed"
		}
		fmt.Fprintf(l.file, "%s %s %s\n", start.Format(time.RFC3339), line, status)
	}

	return err
}

// FormatArgs joins args with RedactCommand applied, %q-quoting any arg that contains whitespace.
func FormatArgs(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		a = RedactCommand(a)
		if strings.ContainsAny(a, " \t\n") {
			a = strconv.Quote(a)
		}
		parts[i] = a
	}

	return strings.Join(parts, " ")
}
