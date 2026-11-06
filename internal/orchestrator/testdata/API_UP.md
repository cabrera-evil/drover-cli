# Expected production API for slice S4c orchestrator Up/Down (derived from the tests)

## package `errs` additions
```go
const ExitInterrupted = 130
const CodeInterrupted = "DRV-INTERRUPTED"   // Up cancelled by ctx (Ctrl-C): *errs.Error{Exit: ExitInterrupted}
```

## package `orchestrator` (new files up.go, down.go, layout.go)
```go
type CommandRunner interface {
	Run(ctx context.Context, dir string, env map[string]string, cmd string) error // stop commands; env via parameter only
}

type SplitOp struct {
	Pane int           // index of the pane to split (0 = workspace root pane)
	Dir  mux.Direction // mux.Right | mux.Down
}

// PlanLayout returns n-1 ops; op i creates pane i+1 by splitting pane op.Pane. Names: ""/default, columns, rows,
// main-vertical (first pane left, rest stacked down). default and main-vertical: {0,right} then {i-1,down}.
// columns: {i-1,right}; rows: {i-1,down}. A Tree plans from its leaves in order (n is ignored): within a
// split node the first leaf is the anchor; columns => right, rows => down; nested nodes split from the pane
// created by their parent op (columns[a, rows[b,c]] => {0,right},{1,down}).
func PlanLayout(l config.Layout, n int) []SplitOp

type Deps struct {
	Mux            mux.Multiplexer
	Store          *state.Store
	Ports          PortChecker
	NewChecker     func(p config.Process, port int) (probe.Checker, error)
	Clock          probe.Clock
	Runner         CommandRunner
	Reporter       io.Writer      // readiness panel, "already running", "not running", failed stop reports
	SelfBin        string         // path embedded in the pane command (runner.PaneCommand)
	LogDir         string         // <LogDir>/<process>.log; Up creates it 0700
	EnvDir         string         // runner.WriteEnvScript dir (0700), scripts 0600
	PollInterval   time.Duration  // probe.Wait interval
	DefaultTimeout time.Duration  // used when Process.Ready.Timeout == 0
}

type Request struct {
	Config  *config.Config
	Stack   string
	Groups  map[string][]string // group -> selected process names, in pane order
	Restart bool
}

// Result lists group names (sorted).
type Result struct{ Created, Reused, Closed, NotRunning []string }

// Up: Preflight over the union of selected processes first (zero mux calls on failure, state untouched);
// then ListWorkspaces + state Reconcile (persisted); per group (sorted): owned+live => FocusWorkspace, report
// "already running" (Reused) unless Restart (close, drop entry, recreate); else CreateWorkspace(cwd of first
// process, group label or name), splits per PlanLayout (cwd = process cwd), then per Levels wave:
// RenamePane + RunInPane(runner.PaneCommand) for the wave, then probe.Wait for each. State is recorded via
// Store.Update right after the workspace is created (workspace id, label, CreatedByDrover, one state.Process
// per selected process in pane order with PaneID and StartedAt). Not ready => probe.NotReadyError, dependants
// not started, nothing closed. ctx cancelled => close workspaces created in this run (with a non-cancelled
// context), drop them from state, return DRV-INTERRUPTED (exit 130).
func Up(ctx context.Context, d Deps, r Request) (Result, error)

// Down: Reconcile; for each selected group owned in state and live: Runner.Run(ctx, process cwd, runner.BuildEnv,
// Stop) for each process with a Stop (failure reported to Reporter, never returned), CloseWorkspace, drop state.
// Groups not owned/live are listed in NotRunning and reported "not running". Never errors for "nothing running".
func Down(ctx context.Context, d Deps, r Request) (Result, error)
```
