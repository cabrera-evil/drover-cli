# Expected production API for slice S3 `internal/ui` readiness panel (derived from the tests)

Pure state machine and renderer; no goroutines, no terminal I/O, time is passed in. The bubbletea wrapper
(added later by the implementer) only calls these and writes the returned strings.

```go
type Proc struct {
	Name    string
	Timeout time.Duration // per-process readiness timeout
}

// NewReadiness starts every process in "starting" at `start`. tty selects the output mode.
func NewReadiness(procs []Proc, tty bool, start time.Time) *Readiness

// MarkReady records readiness at `at`; a process already timed out stays not ready; unknown names are ignored.
func (r *Readiness) MarkReady(name string, at time.Time)

// Render first settles timeouts (now - start >= Timeout and not ready => not ready), then returns text to write.
// Lines, one per process, in declaration order:
//   starting:  "◌ <name> starting 12s"          (whole seconds)
//   ready:     "● <name> ready"
//   not ready: "● <name> not ready — drover logs <name>"
// TTY mode: the first frame is the lines joined by "\n" (each ending "\n"); every later frame starts with
//   cursor-up "\x1b[<N>A" (N = number of processes) and rewrites all lines (each line prefixed "\r\x1b[2K").
// Non-TTY mode: append-only; a line is emitted once when the process first appears (starting), once when it
//   becomes ready, once when it times out; no "\x1b", no "\r"; an unchanged render returns "".
func (r *Readiness) Render(now time.Time) string

// Done reports whether every process is ready or timed out (as of the last Render/MarkReady).
func (r *Readiness) Done() bool

// NotReady returns the timed-out process names in declaration order (as of the last Render).
func (r *Readiness) NotReady() []string
```
