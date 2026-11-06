// Package state persists which workspaces drover created, atomically and under an exclusive lock.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/cabrera-evil/drover-cli/internal/mux"
)

const (
	dirMode  = 0o700
	fileMode = 0o600
)

// Dir resolves the state dir: DROVER_STATE_DIR if set, else <home>/.local/state/drover.
func Dir(getenv func(string) string, home string) string {
	if d := getenv("DROVER_STATE_DIR"); d != "" {
		return d
	}

	return filepath.Join(home, ".local", "state", "drover")
}

// Process is one running process of a group.
type Process struct {
	Name      string    `json:"name"`
	PaneID    string    `json:"pane_id"`
	StartedAt time.Time `json:"started_at"`
	Restarts  int       `json:"restarts"`
}

// Group is a workspace drover tracks for a stack group.
type Group struct {
	WorkspaceID     string    `json:"workspace_id"`
	Label           string    `json:"label"`
	CreatedByDrover bool      `json:"created_by_drover"`
	Processes       []Process `json:"processes"`
}

// State maps stack name -> group name -> Group.
type State struct {
	Stacks map[string]map[string]Group `json:"stacks"`
}

// Reconcile drops every group whose workspace is not live, drops emptied stacks, and returns the dropped workspace
// ids sorted ascending. It never adds or adopts anything (R27).
func (s *State) Reconcile(live []mux.Workspace) []string {
	alive := make(map[string]bool, len(live))
	for _, w := range live {
		alive[w.ID] = true
	}
	var dropped []string
	for stack, groups := range s.Stacks {
		for name, g := range groups {
			if !alive[g.WorkspaceID] {
				dropped = append(dropped, g.WorkspaceID)
				delete(groups, name)
			}
		}
		if len(groups) == 0 {
			delete(s.Stacks, stack)
		}
	}
	slices.Sort(dropped)

	return slices.Compact(dropped)
}

// Store reads and writes <dir>/state.json.
//
// Locking is a single unix flock implementation (R50), tested on the current OS only.
type Store struct {
	dir    string
	rename func(oldpath, newpath string) error
}

// Option configures a Store.
type Option func(*Store)

// WithRename replaces os.Rename (test hook to simulate a kill between temp write and rename).
func WithRename(fn func(oldpath, newpath string) error) Option {
	return func(s *Store) { s.rename = fn }
}

// Open returns a Store for dir. It does no I/O; Update creates the dir.
func Open(dir string, opts ...Option) *Store {
	s := &Store{dir: dir, rename: os.Rename}
	for _, o := range opts {
		o(s)
	}

	return s
}

// Path is the state file path.
func (s *Store) Path() string { return filepath.Join(s.dir, "state.json") }

func (s *Store) lockPath() string { return filepath.Join(s.dir, "state.lock") }

// Load reads the state without locking (writes are atomic renames). A missing file is empty state.
func (s *Store) Load() (State, error) {
	var st State
	b, err := os.ReadFile(s.Path())
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("read state: %w", err)
	}
	if err = json.Unmarshal(b, &st); err != nil {
		return State{}, fmt.Errorf("parse state %s: %w", s.Path(), err)
	}

	return st, nil
}

// Update runs fn under an exclusive lock and atomically writes the result. If fn fails nothing is written.
func (s *Store) Update(fn func(*State) error) (err error) {
	if err = os.MkdirAll(s.dir, dirMode); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()

	st, err := s.Load()
	if err != nil {
		return err
	}
	if err = fn(&st); err != nil {
		return err
	}

	return s.write(st)
}

func (s *Store) lock() (func() error, error) {
	f, err := os.OpenFile(s.lockPath(), os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return nil, fmt.Errorf("open state lock: %w", err)
	}
	fd := int(f.Fd())
	for {
		if err = syscall.Flock(fd, syscall.LOCK_EX); !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()

		return nil, fmt.Errorf("lock state: %w", err)
	}

	// Closing the descriptor releases the flock.
	return f.Close, nil
}

// write stores st via a temp file in the same dir (fsync, then rename) so a crash never leaves a torn state.json.
func (s *Store) write(st State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, "state.json.tmp-*") // created 0600
	if err != nil {
		return fmt.Errorf("create temp state: %w", err)
	}
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = s.rename(tmp.Name(), s.Path())
	}
	if err != nil {
		_ = os.Remove(tmp.Name())

		return fmt.Errorf("write state: %w", err)
	}
	if d, derr := os.Open(s.dir); derr == nil { // best effort: persist the rename itself
		_ = d.Sync()
		_ = d.Close()
	}

	return nil
}
