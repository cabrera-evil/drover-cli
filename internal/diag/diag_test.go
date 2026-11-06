package diag_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cabrera-evil/drover-cli/internal/diag"
)

const (
	secret    = "hunter2"
	debugName = "debug.log"
)

// stepClock advances 12ms on every Now call; Exec must call Now exactly twice per external call.
func stepClock() func() time.Time {
	now := time.Unix(1_700_000_000, 0)

	return func() time.Time {
		t := now
		now = now.Add(12 * time.Millisecond)

		return t
	}
}

func noEnv(string) string { return "" }

func newLogger(t *testing.T, o diag.Options) *diag.Logger {
	t.Helper()
	if o.Now == nil {
		o.Now = stepClock()
	}
	if o.Getenv == nil {
		o.Getenv = noEnv
	}
	l, err := diag.New(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })

	return l
}

// AC39 (R52): -v prints each external call as `herdr <args> (<duration>)` on stderr.
func TestVerboseLogsCallWithDuration(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(t, diag.Options{Verbose: true, Stderr: &buf, StateDir: t.TempDir()})

	ran := false
	err := l.Exec("/opt/bin/herdr", []string{"workspace", "list"}, func() error { ran = true; return nil })
	if err != nil || !ran {
		t.Fatalf("Exec = %v, ran=%v", err, ran)
	}
	if got, want := buf.String(), "herdr workspace list (12ms)\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

// AC39 (R52): args with spaces are quoted so the line stays unambiguous.
func TestVerboseQuotesSpacedArgs(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(t, diag.Options{Verbose: true, Stderr: &buf, StateDir: t.TempDir()})

	_ = l.Exec("herdr", []string{"pane", "run", "w1:p1", "make dev"}, func() error { return nil })
	if got, want := buf.String(), "herdr pane run w1:p1 \"make dev\" (12ms)\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

// AC39 (R52): a failing call is still logged and its error is returned unchanged.
func TestExecReturnsErrorAndStillLogs(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(t, diag.Options{Verbose: true, Stderr: &buf, StateDir: t.TempDir()})

	boom := errors.New("boom")
	if err := l.Exec("herdr", []string{"workspace", "list"}, func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("Exec error = %v, want boom", err)
	}
	if !strings.Contains(buf.String(), "herdr workspace list") || !strings.Contains(buf.String(), "(12ms)") {
		t.Errorf("failed call not logged: %q", buf.String())
	}
}

// R52: without -v nothing is written to stderr and no debug file appears.
func TestQuietByDefault(t *testing.T) {
	var buf bytes.Buffer
	dir := t.TempDir()
	l := newLogger(t, diag.Options{Stderr: &buf, StateDir: dir})

	_ = l.Exec("herdr", []string{"workspace", "list"}, func() error { return nil })
	if buf.Len() != 0 {
		t.Errorf("stderr = %q, want empty", buf.String())
	}
	if _, err := os.Stat(filepath.Join(dir, debugName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("debug log should not exist, stat err = %v", err)
	}
}

// AC39 (R52): DROVER_LOG=debug creates a debug log file in the (created if missing) state dir.
func TestDebugLogFileCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "state")
	getenv := func(k string) string {
		if k == "DROVER_LOG" {
			return "debug"
		}

		return ""
	}
	l := newLogger(t, diag.Options{StateDir: dir, Getenv: getenv, Stderr: &bytes.Buffer{}})

	_ = l.Exec("herdr", []string{"workspace", "list"}, func() error { return nil })
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, debugName))
	if err != nil {
		t.Fatalf("debug log missing: %v", err)
	}
	if !strings.Contains(string(b), "herdr workspace list") {
		t.Errorf("debug log lacks the call: %q", b)
	}
}

// AC37 (R49): secrets in args never reach stderr or the debug log; only keys show.
func TestSecretsNeverLogged(t *testing.T) {
	var buf bytes.Buffer
	dir := t.TempDir()
	getenv := func(k string) string {
		if k == "DROVER_LOG" {
			return "debug"
		}

		return ""
	}
	l := newLogger(t, diag.Options{Verbose: true, Stderr: &buf, StateDir: dir, Getenv: getenv})

	args := []string{"pane", "run", "w1:p1", "API_TOKEN=" + secret + " ./serve"}
	_ = l.Exec("herdr", args, func() error { return nil })
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.ReadFile(filepath.Join(dir, debugName))
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"stderr": buf.String(), "debug log": string(file)} {
		if strings.Contains(text, secret) {
			t.Errorf("secret leaked in %s: %q", name, text)
		}
		if !strings.Contains(text, "API_TOKEN=***") {
			t.Errorf("%s should show API_TOKEN=***: %q", name, text)
		}
	}
}

// AC37 (R49): RedactCommand masks the value of every leading or inline KEY=value token.
func TestRedactCommand(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"no env", "make dev", "make dev"},
		{"single", "TOKEN=abc ./run", "TOKEN=*** ./run"},
		{"several", "A=1 B=two C_3=x go run .", "A=*** B=*** C_3=*** go run ."},
		{"single quoted with space", "API_KEY='s3 cret' run", "API_KEY=*** run"},
		{"double quoted with space", `API_KEY="s3 cret" run`, "API_KEY=*** run"},
		{"empty value", "A= run", "A=*** run"},
		{"equals in later arg is not env", "go test -run=Foo ./...", "go test -run=Foo ./..."},
		{"pane id untouched", "w1:p1", "w1:p1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := diag.RedactCommand(tt.in); got != tt.want {
				t.Errorf("RedactCommand(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
