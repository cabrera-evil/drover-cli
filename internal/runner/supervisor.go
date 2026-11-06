package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cabrera-evil/drover-cli/internal/ansi"
	"github.com/cabrera-evil/drover-cli/internal/errs"
)

const (
	policyOnFailure = "on-failure"
	baseBackoff     = time.Second
	maxBackoff      = 30 * time.Second
	waitDelay       = 2 * time.Second
	maxLogLine      = 64 * 1024
	logMode         = 0o600
	failedCode      = 1 // reported when the child was killed by a signal.
)

// Options configure the in-pane supervisor.
type Options struct {
	Name      string
	Cmd       string // run via `sh -c`
	LogPath   string // truncated on start; ANSI-stripped copy of output; restart markers appended
	EnvScript string // optional; sourced before Cmd ("" = none)
	Restart   string // "never" or "on-failure"
	Max       int    // on-failure restarts; 0 = unlimited
	Stdout    io.Writer
	Sleep     func(ctx context.Context, d time.Duration) // backoff between restarts; nil = real sleep
}

// Run executes Cmd and applies the restart policy. Child stdout and stderr go unchanged to Stdout and
// ANSI-stripped to LogPath. It returns the child's last exit code; err is for supervisor failures and
// never contains env values. Cancelling ctx kills the child's whole process group and Run returns.
func Run(ctx context.Context, o Options) (int, error) {
	logf, err := os.OpenFile(o.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, logMode)
	if err != nil {
		return 0, &errs.Error{Code: errs.CodeRunFailed, File: o.LogPath, Message: "cannot open log file"}
	}
	defer logf.Close()

	stdout := o.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	sleep := o.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	lw := &logWriter{w: logf}
	out := io.MultiWriter(stdout, lw)

	for attempt := 0; ; attempt++ {
		code, runErr := runOnce(ctx, o, out)
		lw.flush()
		if runErr != nil {
			return code, runErr
		}
		if ctx.Err() != nil || o.Restart != policyOnFailure || code == 0 || (o.Max > 0 && attempt >= o.Max) {
			return code, nil
		}
		sleep(ctx, backoff(attempt))
		if ctx.Err() != nil {
			return code, nil
		}
		fmt.Fprintf(logf, "--- restart %d/%s ---\n", attempt+1, maxLabel(o.Max))
	}
}

// runOnce starts the command once and waits for it, returning its exit code.
func runOnce(ctx context.Context, o Options, out io.Writer) (int, error) {
	// Values stay in the env script (0600); argv only carries its path and the user's command.
	args := []string{"-c", o.Cmd}
	if o.EnvScript != "" {
		script := o.EnvScript
		if !strings.ContainsRune(script, os.PathSeparator) {
			script = "." + string(os.PathSeparator) + script
		}
		args = []string{"-c", `. "$1" && exec sh -c "$2"`, "sh", script, o.Cmd}
	}
	cmd := exec.CommandContext(ctx, "sh", args...)
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = waitDelay
	killGroupOnCancel(cmd)
	if err := cmd.Start(); err != nil {
		return 0, &errs.Error{Code: errs.CodeRunFailed, Message: "cannot start command for " + o.Name}
	}
	err := cmd.Wait()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return max(exitErr.ExitCode(), failedCode), nil // a signal kill reports -1, mapped to 1.
	}
	if ctx.Err() != nil {
		return failedCode, nil //nolint:nilerr // cancellation, not a supervisor failure.
	}

	return 0, &errs.Error{Code: errs.CodeRunFailed, Message: "command for " + o.Name + " failed to complete"}
}

// backoff is 1s doubling per attempt, capped at 30s.
func backoff(attempt int) time.Duration {
	d := baseBackoff
	for range attempt {
		if d >= maxBackoff {
			break
		}
		d *= 2
	}

	return min(d, maxBackoff)
}

func maxLabel(m int) string {
	if m == 0 {
		return "inf"
	}

	return strconv.Itoa(m)
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// logWriter writes ANSI-stripped output to w one line at a time. Write errors are swallowed so a bad
// log never interrupts the live output (ponytail: surface a log write failure if it ever matters).
type logWriter struct {
	w   io.Writer
	buf []byte
}

func (l *logWriter) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		l.emit(l.buf[:i+1])
		l.buf = l.buf[i+1:]
	}
	if len(l.buf) > maxLogLine {
		l.emit(l.buf)
		l.buf = nil
	}

	return len(p), nil
}

// flush writes any unterminated final line, ending it with a newline so a restart marker starts clean.
func (l *logWriter) flush() {
	if len(l.buf) > 0 {
		l.emit(append(l.buf, '\n'))
		l.buf = nil
	}
}

func (l *logWriter) emit(b []byte) {
	_, _ = io.WriteString(l.w, ansi.Strip(string(b)))
}
