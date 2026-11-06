package runner_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cabrera-evil/drover-cli/internal/config"
	"github.com/cabrera-evil/drover-cli/internal/runner"
)

const (
	policyNever     = "never"
	policyOnFailure = "on-failure"
	redText         = "\x1b[31mred\x1b[0m\n"
)

type harness struct {
	opts   runner.Options
	stdout *bytes.Buffer
	log    string
	count  string
	sleeps []time.Duration
}

// newHarness returns Options with a temp log, captured stdout and an instant recording Sleep.
func newHarness(t *testing.T, cmd string) *harness {
	t.Helper()
	dir := t.TempDir()
	h := &harness{stdout: &bytes.Buffer{}, log: filepath.Join(dir, "p.log"), count: filepath.Join(dir, "count")}
	h.opts = runner.Options{
		Name:    "p",
		Cmd:     strings.ReplaceAll(cmd, "$COUNT", "'"+h.count+"'"),
		LogPath: h.log,
		Restart: policyNever,
		Stdout:  h.stdout,
		Sleep:   func(_ context.Context, d time.Duration) { h.sleeps = append(h.sleeps, d) },
	}

	return h
}

func (h *harness) runs(t *testing.T) int {
	t.Helper()
	b, err := os.ReadFile(h.count)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}

	return strings.Count(string(b), "x")
}

func (h *harness) logText(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(h.log)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

// AC26: on-failure with max 2 restarts a failing child at most twice (3 runs); returns last code.
func TestRunOnFailureMax(t *testing.T) {
	h := newHarness(t, "printf x >> $COUNT; exit 7")
	h.opts.Restart, h.opts.Max = policyOnFailure, 2
	code, err := runner.Run(t.Context(), h.opts)
	if err != nil {
		t.Fatal(err)
	}
	if code != 7 || h.runs(t) != 3 {
		t.Fatalf("code=%d runs=%d, want 7 and 3", code, h.runs(t))
	}
	if len(h.sleeps) != 2 {
		t.Errorf("backoff sleeps = %d, want 2", len(h.sleeps))
	}
}

// AC26: exit 0 is not restarted under on-failure.
func TestRunOnFailureSuccessNotRestarted(t *testing.T) {
	h := newHarness(t, "printf x >> $COUNT")
	h.opts.Restart, h.opts.Max = policyOnFailure, 5
	code, err := runner.Run(t.Context(), h.opts)
	if err != nil || code != 0 || h.runs(t) != 1 {
		t.Fatalf("code=%d err=%v runs=%d, want 0 nil 1", code, err, h.runs(t))
	}
}

// AC26: restart never runs once and returns the child's code.
func TestRunNeverRunsOnce(t *testing.T) {
	h := newHarness(t, "printf x >> $COUNT; exit 3")
	code, err := runner.Run(t.Context(), h.opts)
	if err != nil || code != 3 || h.runs(t) != 1 {
		t.Fatalf("code=%d err=%v runs=%d, want 3 nil 1", code, err, h.runs(t))
	}
}

// AC26: max 0 is unlimited; the test bounds it by cancelling the context from Sleep.
func TestRunUnlimitedBoundedByCancel(t *testing.T) {
	const stopAfter = 3
	h := newHarness(t, "printf x >> $COUNT; exit 1")
	h.opts.Restart, h.opts.Max = policyOnFailure, 0
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	h.opts.Sleep = func(_ context.Context, d time.Duration) {
		h.sleeps = append(h.sleeps, d)
		if len(h.sleeps) == stopAfter {
			cancel()
		}
	}
	_, _ = runner.Run(ctx, h.opts)
	if got := h.runs(t); got != stopAfter {
		t.Fatalf("runs = %d, want %d", got, stopAfter)
	}
}

// AC28: colour reaches stdout unchanged.
func TestRunStdoutKeepsColour(t *testing.T) {
	h := newHarness(t, `printf '\033[31mred\033[0m\n'`)
	if _, err := runner.Run(t.Context(), h.opts); err != nil {
		t.Fatal(err)
	}
	if h.stdout.String() != redText {
		t.Fatalf("stdout = %q, want %q", h.stdout.String(), redText)
	}
}

// AC36: log copy is ANSI-stripped.
func TestRunLogStripsANSI(t *testing.T) {
	h := newHarness(t, `printf '\033[31mred\033[0m\n'`)
	if _, err := runner.Run(t.Context(), h.opts); err != nil {
		t.Fatal(err)
	}
	if got := h.logText(t); got != "red\n" {
		t.Fatalf("log = %q, want %q", got, "red\n")
	}
}

// AC36: a fresh run truncates an existing log.
func TestRunTruncatesLogOnStart(t *testing.T) {
	h := newHarness(t, "echo new")
	if err := os.WriteFile(h.log, []byte("OLD CONTENT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(t.Context(), h.opts); err != nil {
		t.Fatal(err)
	}
	if got := h.logText(t); got != "new\n" {
		t.Fatalf("log = %q, want only new output", got)
	}
}

// AC36: restarts append a plain marker instead of truncating; colour still reaches stdout.
func TestRunRestartMarkerInLog(t *testing.T) {
	h := newHarness(t, `printf x >> $COUNT; printf '\033[32mrun\033[0m\n'; exit 1`)
	h.opts.Restart, h.opts.Max = policyOnFailure, 2
	if _, err := runner.Run(t.Context(), h.opts); err != nil {
		t.Fatal(err)
	}
	want := "run\n--- restart 1/2 ---\nrun\n--- restart 2/2 ---\nrun\n"
	if got := h.logText(t); got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
	if got := strings.Count(h.stdout.String(), "\x1b[32mrun\x1b[0m\n"); got != 3 {
		t.Errorf("coloured stdout lines = %d, want 3", got)
	}
}

// AC28: the env script is sourced; FORCE_COLOR=1 by default, removed by an empty override.
func TestRunEnvScriptAndForceColor(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want string
	}{
		"default":  {map[string]string{"GREETING": "hi there"}, "1|hi there\n"},
		"removed":  {map[string]string{"GREETING": "hi there", keyColor: ""}, "unset|hi there\n"},
		"override": {map[string]string{"GREETING": "hi there", keyColor: "0"}, "0|hi there\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(keyColor, "")
			if err := os.Unsetenv(keyColor); err != nil {
				t.Fatal(err)
			}
			h := newHarness(t, `printf '%s|%s\n' "${FORCE_COLOR-unset}" "$GREETING"`)
			env, err := runner.BuildEnv(config.Process{Env: tc.env})
			if err != nil {
				t.Fatal(err)
			}
			h.opts.EnvScript, err = runner.WriteEnvScript(t.TempDir(), "p", env)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = runner.Run(t.Context(), h.opts); err != nil {
				t.Fatal(err)
			}
			if h.stdout.String() != tc.want {
				t.Fatalf("stdout = %q, want %q", h.stdout.String(), tc.want)
			}
		})
	}
}

// R49: a supervisor failure never prints env values (unwritable log path, secret in the script).
func TestRunErrorOmitsEnvValues(t *testing.T) {
	h := newHarness(t, "true")
	h.opts.LogPath = filepath.Join(t.TempDir(), "missing-dir", "p.log")
	script, err := runner.WriteEnvScript(t.TempDir(), "p", map[string]string{"TOKEN": secretValue})
	if err != nil {
		t.Fatal(err)
	}
	h.opts.EnvScript = script
	_, err = runner.Run(t.Context(), h.opts)
	if err == nil {
		t.Fatal("want error for unwritable log path")
	}
	if strings.Contains(err.Error(), secretValue) {
		t.Fatalf("error leaks value: %v", err)
	}
}

func TestRunReturnsChildExitCode(t *testing.T) {
	h := newHarness(t, "exit 42")
	code, err := runner.Run(t.Context(), h.opts)
	if err != nil || code != 42 {
		t.Fatalf("code=%d err=%v, want 42 nil", code, err)
	}
}
