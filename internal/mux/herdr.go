package mux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cabrera-evil/drover-cli/internal/diag"
	"github.com/cabrera-evil/drover-cli/internal/errs"
)

const (
	defaultTimeout = 10 * time.Second
	waitDelay      = time.Second
)

// Herdr drives the herdr CLI. Every call uses exec.CommandContext with a timeout and no shell.
type Herdr struct {
	bin     string
	timeout time.Duration
	diag    *diag.Logger
}

// Option configures a Herdr.
type Option func(*Herdr)

// WithTimeout sets the per-call timeout.
func WithTimeout(d time.Duration) Option { return func(h *Herdr) { h.timeout = d } }

// WithDiag routes every exec through the logger.
func WithDiag(l *diag.Logger) Option { return func(h *Herdr) { h.diag = l } }

// NewHerdr returns an adapter for the herdr binary at bin; bin is never looked up on PATH.
func NewHerdr(bin string, opts ...Option) *Herdr {
	h := &Herdr{bin: bin, timeout: defaultTimeout}
	for _, o := range opts {
		o(h)
	}

	return h
}

// ListWorkspaces returns the live workspaces.
func (h *Herdr) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	var out struct {
		Result struct {
			Workspaces []struct {
				ID        string `json:"workspace_id"`
				Label     string `json:"label"`
				PaneCount int    `json:"pane_count"`
			} `json:"workspaces"`
		} `json:"result"`
	}
	if err := h.json(ctx, &out, "workspace", "list"); err != nil {
		return nil, err
	}
	ws := make([]Workspace, len(out.Result.Workspaces))
	for i, w := range out.Result.Workspaces {
		ws[i] = Workspace{ID: w.ID, Label: w.Label, PaneCount: w.PaneCount}
	}

	return ws, nil
}

// CreateWorkspace creates an unfocused workspace and returns its id and root pane.
func (h *Herdr) CreateWorkspace(ctx context.Context, cwd, label string) (Created, error) {
	var out struct {
		Result struct {
			Workspace struct {
				ID string `json:"workspace_id"`
			} `json:"workspace"`
			RootPane struct {
				ID string `json:"pane_id"`
			} `json:"root_pane"`
		} `json:"result"`
	}
	args := []string{"workspace", "create", "--cwd", cwd, "--label", label, "--no-focus"}
	if err := h.json(ctx, &out, args...); err != nil {
		return Created{}, err
	}
	c := Created{WorkspaceID: out.Result.Workspace.ID, RootPaneID: out.Result.RootPane.ID}
	if c.WorkspaceID == "" || c.RootPaneID == "" {
		return Created{}, badOutput(args)
	}

	return c, nil
}

// SplitPane splits a pane without focusing and returns the new pane id.
func (h *Herdr) SplitPane(ctx context.Context, paneID string, dir Direction, cwd string) (string, error) {
	var out struct {
		Result struct {
			Pane struct {
				ID string `json:"pane_id"`
			} `json:"pane"`
		} `json:"result"`
	}
	args := []string{"pane", "split", "--pane", paneID, "--direction", string(dir), "--cwd", cwd, "--no-focus"}
	if err := h.json(ctx, &out, args...); err != nil {
		return "", err
	}
	if out.Result.Pane.ID == "" {
		return "", badOutput(args)
	}

	return out.Result.Pane.ID, nil
}

// RenamePane sets a pane label.
func (h *Herdr) RenamePane(ctx context.Context, paneID, label string) error {
	_, err := h.run(ctx, "pane", "rename", paneID, label)

	return err
}

// RunInPane types cmd into a pane and runs it.
func (h *Herdr) RunInPane(ctx context.Context, paneID, cmd string) error {
	_, err := h.run(ctx, "pane", "run", paneID, cmd)

	return err
}

// ReadPane returns the raw stdout of `pane read`; its shape is not parsed.
func (h *Herdr) ReadPane(ctx context.Context, paneID string) (string, error) {
	return h.run(ctx, "pane", "read", paneID)
}

// FocusWorkspace focuses a workspace.
func (h *Herdr) FocusWorkspace(ctx context.Context, id string) error {
	_, err := h.run(ctx, "workspace", "focus", id)

	return err
}

// CloseWorkspace closes a workspace.
func (h *Herdr) CloseWorkspace(ctx context.Context, id string) error {
	_, err := h.run(ctx, "workspace", "close", id)

	return err
}

func (h *Herdr) json(ctx context.Context, into any, args ...string) error {
	out, err := h.run(ctx, args...)
	if err != nil {
		return err
	}
	if err = json.Unmarshal([]byte(out), into); err != nil {
		return badOutput(args)
	}

	return nil
}

func badOutput(args []string) error {
	return &errs.Error{
		Code:    errs.CodeHerdrFailed,
		Message: "herdr " + diag.FormatArgs(args) + " returned unexpected output",
		Fix:     "Check that the installed herdr is a supported version (herdr --version).",
	}
}

// run executes herdr and returns its stdout as-is. Errors are typed and secrets in args are redacted.
func (h *Herdr) run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	call := func() error {
		cmd := exec.CommandContext(ctx, h.bin, args...) //nolint:gosec // bin is the injected herdr path; no shell.
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		cmd.WaitDelay = waitDelay

		return cmd.Run()
	}
	var err error
	if h.diag != nil {
		err = h.diag.Exec(h.bin, args, call)
	} else {
		err = call()
	}
	if err != nil {
		return "", h.wrap(ctx, args, err, stderr.String())
	}

	return stdout.String(), nil
}

func (h *Herdr) wrap(ctx context.Context, args []string, err error, stderr string) error {
	var exit *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return &errs.Error{
			Code:    errs.CodeHerdrFailed,
			Message: "herdr " + diag.FormatArgs(args) + " aborted: " + ctx.Err().Error(),
			Fix:     "Check that herdr is running and responsive.",
		}
	case errors.As(err, &exit):
		msg := "herdr " + diag.FormatArgs(args) + " failed: exit code " + strconv.Itoa(exit.ExitCode())
		if s := scrub(stderr, args); s != "" {
			msg += ": " + s
		}

		return &errs.Error{Code: errs.CodeHerdrFailed, Message: msg, Fix: "Check that herdr is running."}
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, fs.ErrPermission), errors.Is(err, exec.ErrNotFound):
		return &errs.Error{
			Code:    errs.CodeHerdrNotFound,
			Message: "herdr binary " + h.bin + " was not found or is not executable",
			Fix:     "Install herdr and make sure it is executable.",
		}
	}

	return &errs.Error{
		Code:    errs.CodeHerdrFailed,
		Message: "herdr " + diag.FormatArgs(args) + " could not run: " + scrub(err.Error(), args),
	}
}

// scrub trims s and replaces every raw argument that RedactCommand would mask with its redacted form.
func scrub(s string, args []string) string {
	for _, a := range args {
		if r := diag.RedactCommand(a); r != a {
			s = strings.ReplaceAll(s, a, r)
		}
	}

	return strings.TrimSpace(s)
}
