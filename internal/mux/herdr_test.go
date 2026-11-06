package mux_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cabrera-evil/drover-cli/internal/errs"
	"github.com/cabrera-evil/drover-cli/internal/mux"
)

const (
	codeFailed   = "DRV-HERDR-FAILED"
	codeNotFound = "DRV-HERDR-NOT-FOUND"
	secret       = "hunter2"
)

// AC32 (R44): workspace list/create/split parse ids, labels and pane counts from the herdr JSON shapes.
func TestHerdrParsesJSONShapes(t *testing.T) {
	h, _ := newHerdr(t)
	ctx := context.Background()

	created, err := h.CreateWorkspace(ctx, "/work/api", "api")
	if err != nil {
		t.Fatal(err)
	}
	if created.WorkspaceID != "w1" || created.RootPaneID != "w1:p1" {
		t.Fatalf("CreateWorkspace = %+v, want w1 / w1:p1", created)
	}
	second, err := h.SplitPane(ctx, created.RootPaneID, mux.Right, "/work/api")
	if err != nil || second != "w1:p2" {
		t.Fatalf("SplitPane = %q, %v", second, err)
	}
	if _, err = h.CreateWorkspace(ctx, "/work/web", "web"); err != nil {
		t.Fatal(err)
	}

	got, err := h.ListWorkspaces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []mux.Workspace{{ID: "w1", Label: "api", PaneCount: 2}, {ID: "w2", Label: "web", PaneCount: 1}}
	if len(got) != len(want) {
		t.Fatalf("ListWorkspaces = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("workspace %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// AC32 (R44): the adapter issues exactly the documented subcommands and flags.
func TestHerdrSubcommands(t *testing.T) {
	h, statePath := newHerdr(t)
	ctx := context.Background()

	created, err := h.CreateWorkspace(ctx, "/work/api", "api")
	if err != nil {
		t.Fatal(err)
	}
	args := lastCall(t, statePath)
	if args[0] != "workspace" || args[1] != "create" || flagOf(args, "--cwd") != "/work/api" ||
		flagOf(args, "--label") != "api" || !has(args, "--no-focus") {
		t.Errorf("create call = %v", args)
	}

	if _, err = h.SplitPane(ctx, created.RootPaneID, mux.Down, "/work/api/sub"); err != nil {
		t.Fatal(err)
	}
	args = lastCall(t, statePath)
	if args[0] != "pane" || args[1] != "split" || flagOf(args, "--pane") != created.RootPaneID ||
		flagOf(args, "--direction") != "down" || flagOf(args, "--cwd") != "/work/api/sub" || !has(args, "--no-focus") {
		t.Errorf("split call = %v", args)
	}

	steps := []struct {
		name string
		do   func() error
		want string
	}{
		{"rename", func() error { return h.RenamePane(ctx, "w1:p1", "api-1") }, "pane rename w1:p1 api-1"},
		{"run", func() error { return h.RunInPane(ctx, "w1:p1", "make dev") }, "pane run w1:p1 make dev"},
		{"focus", func() error { return h.FocusWorkspace(ctx, "w1") }, "workspace focus w1"},
	}
	for _, s := range steps {
		if err = s.do(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if got := strings.Join(lastCall(t, statePath), " "); got != s.want {
			t.Errorf("%s call = %q, want %q", s.name, got, s.want)
		}
	}
}

// AC32 (R44): ReadPane returns pane output, CloseWorkspace removes the workspace.
func TestHerdrReadAndClose(t *testing.T) {
	h, statePath := newHerdr(t)
	ctx := context.Background()
	created, err := h.CreateWorkspace(ctx, "/work/api", "api")
	if err != nil {
		t.Fatal(err)
	}
	if err = h.RunInPane(ctx, created.RootPaneID, "make dev"); err != nil {
		t.Fatal(err)
	}
	out, err := h.ReadPane(ctx, created.RootPaneID)
	if err != nil || !strings.Contains(out, "make dev") {
		t.Fatalf("ReadPane = %q, %v", out, err)
	}

	if err = h.CloseWorkspace(ctx, created.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(lastCall(t, statePath), " "); got != "workspace close w1" {
		t.Errorf("close call = %q", got)
	}
	list, err := h.ListWorkspaces(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("after close ListWorkspaces = %v, %v", list, err)
	}
}

// AC33 (R45): a failing herdr call yields a typed error with command, exit code and stderr.
func TestHerdrFailureIsTyped(t *testing.T) {
	ctx := context.Background()
	calls := []struct {
		fail string
		do   func(h *mux.Herdr) error
	}{
		{"workspace list", func(h *mux.Herdr) error { _, err := h.ListWorkspaces(ctx); return err }},
		{"workspace create", func(h *mux.Herdr) error { _, err := h.CreateWorkspace(ctx, "/x", "l"); return err }},
		{"pane split", func(h *mux.Herdr) error { _, err := h.SplitPane(ctx, "w1:p1", mux.Right, "/x"); return err }},
		{"pane rename", func(h *mux.Herdr) error { return h.RenamePane(ctx, "w1:p1", "l") }},
		{"pane run", func(h *mux.Herdr) error { return h.RunInPane(ctx, "w1:p1", "ls") }},
		{"pane read", func(h *mux.Herdr) error { _, err := h.ReadPane(ctx, "w1:p1"); return err }},
		{"workspace focus", func(h *mux.Herdr) error { return h.FocusWorkspace(ctx, "w1") }},
		{"workspace close", func(h *mux.Herdr) error { return h.CloseWorkspace(ctx, "w1") }},
	}
	for _, c := range calls {
		t.Run(c.fail, func(t *testing.T) {
			h, _ := newHerdr(t)
			t.Setenv("FAKE_HERDR_FAIL", c.fail)

			err := c.do(h)
			var e *errs.Error
			if !errors.As(err, &e) {
				t.Fatalf("error %v is not *errs.Error", err)
			}
			if e.Code != codeFailed || e.ExitCode() != 1 {
				t.Errorf("code/exit = %s/%d, want %s/1", e.Code, e.ExitCode(), codeFailed)
			}
			for _, want := range []string{"herdr " + c.fail, "exit code 1", "simulated failure"} {
				if !strings.Contains(e.Message, want) {
					t.Errorf("message %q lacks %q", e.Message, want)
				}
			}
		})
	}
}

// R45: a missing binary is DRV-HERDR-NOT-FOUND, exit 1.
func TestHerdrBinaryMissing(t *testing.T) {
	h := mux.NewHerdr(t.TempDir() + "/no-such-herdr")
	_, err := h.ListWorkspaces(context.Background())
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != codeNotFound || e.ExitCode() != 1 {
		t.Fatalf("error = %v, want %s exit 1", err, codeNotFound)
	}
}

// R45: every call has a timeout; a hung herdr is cut off with a typed error.
func TestHerdrTimeout(t *testing.T) {
	h, _ := newHerdr(t, mux.WithTimeout(100*time.Millisecond))
	t.Setenv("FAKE_HERDR_SLEEP_MS", "5000")

	start := time.Now()
	_, err := h.ListWorkspaces(context.Background())
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != codeFailed {
		t.Fatalf("error = %v, want %s", err, codeFailed)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("call took %s, want it cut off near the 100ms timeout", elapsed)
	}
}

// R45: a cancelled context aborts the call with a typed error.
func TestHerdrContextCancelled(t *testing.T) {
	h, _ := newHerdr(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.ListWorkspaces(ctx)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != codeFailed {
		t.Fatalf("error = %v, want %s", err, codeFailed)
	}
}

// AC37 (R49): a secret in the pane command never appears in the error, only the key.
func TestHerdrErrorRedactsSecrets(t *testing.T) {
	h, _ := newHerdr(t)
	t.Setenv("FAKE_HERDR_FAIL", "pane run")

	err := h.RunInPane(context.Background(), "w1:p1", "API_TOKEN="+secret+" ./serve")
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("error %v is not *errs.Error", err)
	}
	for _, text := range []string{err.Error(), e.Message, e.Fix} {
		if strings.Contains(text, secret) {
			t.Errorf("secret leaked in %q", text)
		}
	}
	if !strings.Contains(e.Message, "API_TOKEN=***") {
		t.Errorf("message %q should show API_TOKEN=***", e.Message)
	}
}

// AC37 (R49) + AC39 (R52): -v output shows each call with its duration and never a secret.
func TestHerdrVerboseLogsCallsWithoutSecrets(t *testing.T) {
	l, buf := verboseDiag(t)
	h, _ := newHerdr(t, mux.WithDiag(l))
	ctx := context.Background()

	if _, err := h.ListWorkspaces(ctx); err != nil {
		t.Fatal(err)
	}
	created, err := h.CreateWorkspace(ctx, "/work/api", "api")
	if err != nil {
		t.Fatal(err)
	}
	if err = h.RunInPane(ctx, created.RootPaneID, "API_TOKEN="+secret+" ./serve"); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "herdr workspace list (12ms)\n") {
		t.Errorf("-v output lacks timed list call:\n%s", out)
	}
	if !strings.Contains(out, "herdr workspace create") || !strings.Contains(out, "herdr pane run") {
		t.Errorf("-v output lacks create/run calls:\n%s", out)
	}
	if strings.Contains(out, secret) {
		t.Errorf("secret leaked in -v output:\n%s", out)
	}
	if !strings.Contains(out, "API_TOKEN=***") {
		t.Errorf("-v output should show API_TOKEN=***:\n%s", out)
	}
}

// AC37 (R49): a failing call's -v output and error also stay secret-free.
func TestHerdrVerboseFailureRedacts(t *testing.T) {
	l, buf := verboseDiag(t)
	h, _ := newHerdr(t, mux.WithDiag(l))
	t.Setenv("FAKE_HERDR_FAIL", "pane run")

	err := h.RunInPane(context.Background(), "w1:p1", "API_TOKEN="+secret+" ./serve")
	if err == nil {
		t.Fatal("expected failure")
	}
	if strings.Contains(buf.String(), secret) || strings.Contains(err.Error(), secret) {
		t.Errorf("secret leaked: out=%q err=%q", buf.String(), err)
	}
}
