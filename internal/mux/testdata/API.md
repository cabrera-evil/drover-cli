# internal/mux expected API (S4a)

Tests are in package `mux_test`. They build `internal/testutil/fakeherdr` once per test binary
(sync.Once) into a temp dir as a binary named `herdr`, and inject its path; real herdr is never run.

```go
package mux

type Workspace struct {
	ID        string // workspace_id
	Label     string
	PaneCount int    // pane_count
}
type Created struct{ WorkspaceID, RootPaneID string }
type Direction string
const (Right Direction = "right"; Down Direction = "down")

type Multiplexer interface { // 8 methods
	ListWorkspaces(ctx context.Context) ([]Workspace, error)
	CreateWorkspace(ctx context.Context, cwd, label string) (Created, error)      // workspace create --cwd --label --no-focus
	SplitPane(ctx context.Context, paneID string, dir Direction, cwd string) (string, error) // pane split --pane --direction --cwd --no-focus
	RenamePane(ctx context.Context, paneID, label string) error                   // pane rename <id> <label>
	RunInPane(ctx context.Context, paneID, cmd string) error                      // pane run <id> <cmd>
	ReadPane(ctx context.Context, paneID string) (string, error)                  // pane read <id>; returns stdout as-is
	FocusWorkspace(ctx context.Context, id string) error
	CloseWorkspace(ctx context.Context, id string) error
}

type Herdr struct{ /* ... */ }              // satisfies Multiplexer
type Option func(*Herdr)
func NewHerdr(bin string, opts ...Option) *Herdr   // bin is the injected binary path; never looked up on PATH
func WithTimeout(d time.Duration) Option    // per-call timeout (default e.g. 10s)
func WithDiag(l *diag.Logger) Option        // every exec goes through l.Exec(bin, args, fn)
```

Errors (all `*errs.Error`, exit 1, new slug codes to add in internal/errs):
- `DRV-HERDR-FAILED` (nonzero exit, timeout, cancelled ctx). Message contains `herdr <args>`, `exit code <n>` (when it exited) and
  trimmed stderr. Args in the message pass through diag.RedactCommand (secrets never shown, only `KEY=***`).
- `DRV-HERDR-NOT-FOUND` (binary missing / not executable).

The fake herdr (`internal/testutil/fakeherdr`) env: FAKE_HERDR_STATE (required, JSON file incl. a `calls` list of argv),
FAKE_HERDR_FAIL=<argv prefix, e.g. "pane run">, FAKE_HERDR_SLEEP_MS. Fake ids: workspaces w1,w2,..; panes <ws>:p1,p2,..
`pane read` prints the commands run in the pane, one per line, as plain text (real herdr's shape unverified).
