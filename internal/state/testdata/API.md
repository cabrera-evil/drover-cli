# internal/state expected API (S4a)

Tests are in package `state_test`. State dir is a constructor argument (not an env var inside the package).

```go
package state

// Dir resolves the state dir: getenv("DROVER_STATE_DIR") if non-empty, else home + "/.local/state/drover".
func Dir(getenv func(string) string, home string) string

type Process struct {
	Name      string    // json tags are the implementer's choice
	PaneID    string
	StartedAt time.Time
	Restarts  int
}

type Group struct {
	WorkspaceID     string
	Label           string
	CreatedByDrover bool
	Processes       []Process
}

// State maps stack name -> group name -> Group. Nil/empty is valid. Must JSON-roundtrip.
type State struct{ Stacks map[string]map[string]Group }

// Reconcile removes every group whose WorkspaceID is not in live, removes stacks left with no groups,
// and returns the dropped workspace ids sorted ascending. It never adds or adopts anything (R27).
func (s *State) Reconcile(live []mux.Workspace) []string

type Store struct{ /* dir, rename hook */ }
type Option func(*Store)

// WithRename replaces os.Rename (test hook to simulate a kill between temp write and rename).
func WithRename(fn func(oldpath, newpath string) error) Option

// Open does no I/O; the dir is created (MkdirAll) by Update.
func Open(dir string, opts ...Option) *Store

// Path is <dir>/state.json. The lock file is <dir>/state.lock.
func (s *Store) Path() string

// Load reads the file without locking is acceptable; a missing file returns empty State, nil.
// A stale temp file such as state.json.tmp-* in dir must be ignored.
func (s *Store) Load() (State, error)

// Update takes an exclusive flock on the lock file, loads, calls fn, and on nil writes atomically
// (temp file in the same dir + rename). If fn errors, nothing is written and its error is returned (errors.Is).
// If rename fails, that error is returned (errors.Is) and the previous file is untouched.
func (s *Store) Update(fn func(*State) error) error
```

R50: flock is a single unix implementation (syscall.Flock) tested on the current OS only.
