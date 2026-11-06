# Expected production API for slice S3 `internal/probe` (derived from the tests)

## package `errs` additions
```go
const ExitReadiness = 5
const CodeReadyTimeout = "DRV-READY-TIMEOUT"
```

## package `probe`
```go
type PortTable interface{ Listening(port int) (bool, error) }   // reads the socket table; never dials
type Exec func(name string, args ...string) ([]byte, error)      // injected command runner
type ReadFile func(path string) ([]byte, error)                  // injected file reader

func ParseSS(out string, port int) bool        // `ss -Hltn` lines; exact local port match (v4, [::], *)
func ParseProcNet(content string, port int) bool // /proc/net/tcp{,6}: hex port, st == 0A; skips header
func ParseLsof(out string, port int) bool      // `lsof -nP -iTCP -sTCP:LISTEN`: NAME ends host:port (LISTEN)

// NewTable picks the implementation by goos ("linux", "darwin"); other values return an error.
// Linux: runs `ss -Hltn "sport = :PORT"` (one call per Listening); if the command errors, falls back to
// ReadFile of /proc/net/tcp and /proc/net/tcp6; if neither works Listening returns an error (never "free").
// Darwin: runs `lsof -nP -iTCP -sTCP:LISTEN` and parses it.
func NewTable(goos string, run Exec, read ReadFile) (PortTable, error)

// PortChecker adapts a PortTable to orchestrator.PortChecker (InUse(port int) bool); a table error => false.
// probe must not import orchestrator; *probe.PortChecker satisfies the orchestrator interface structurally.
type PortChecker struct{ /* ... */ }
func NewPortChecker(t PortTable) *PortChecker
func (*PortChecker) InUse(port int) bool

type Checker interface{ Check(ctx context.Context) (bool, error) }
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}
type RealClock struct{}                          // implements Clock with the time package

type Deps struct {
	Table  PortTable          // tcp
	Lines  <-chan string      // log: lines drained non-blockingly on each Check
	Clock  Clock              // delay (defaults to RealClock when nil)
	Status int                // http expected status; 0 means any 2xx
	Client *http.Client       // optional; default has a bounded timeout. Each request also honours ctx.
}
// NewChecker builds a checker from config.Ready: http (Value = URL, or a path starting with "/" combined with
// http://127.0.0.1:<port>), tcp (Value "host:port", Table.Listening(port)), log (Value = regexp, sticky once
// matched), delay (Value = duration, ready when Clock.Now() - construction >= delay), none (always ready).
// Invalid regexp / duration / address / unknown Kind => error.
func NewChecker(r config.Ready, port int, d Deps) (Checker, error)

type Result struct {
	Ready   bool
	Elapsed time.Duration // clock time from start to ready, or until the timeout expired
}
// Wait loops: Check (ctx bounded by the remaining timeout); ready => return; elapsed >= timeout => not ready;
// otherwise <-clock.After(interval). A Check error counts as not ready. Honours ctx cancellation.
func Wait(ctx context.Context, c Checker, timeout, interval time.Duration, clk Clock) Result

// NotReadyError returns *errs.Error{Code: CodeReadyTimeout, Exit: ExitReadiness} whose Message/Fix contain
// "drover logs <proc>" and the timeout.
func NotReadyError(proc string, timeout time.Duration) *errs.Error
```
