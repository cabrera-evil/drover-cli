package state_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cabrera-evil/drover-cli/internal/mux"
	"github.com/cabrera-evil/drover-cli/internal/state"
)

func started() time.Time { return time.Date(2026, 5, 1, 10, 30, 0, 0, time.UTC) }

func group(wsID, label string) state.Group {
	return state.Group{
		WorkspaceID:     wsID,
		Label:           label,
		CreatedByDrover: true,
		Processes:       []state.Process{{Name: "api", PaneID: wsID + ":p1", StartedAt: started(), Restarts: 2}},
	}
}

func put(t *testing.T, s *state.Store, stack, name string, g state.Group) {
	t.Helper()
	err := s.Update(func(st *state.State) error {
		if st.Stacks == nil {
			st.Stacks = map[string]map[string]state.Group{}
		}
		if st.Stacks[stack] == nil {
			st.Stacks[stack] = map[string]state.Group{}
		}
		st.Stacks[stack][name] = g

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// R46: DROVER_STATE_DIR wins, otherwise ~/.local/state/drover.
func TestDir(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }

	if got, want := state.Dir(getenv, "/home/u"), "/home/u/.local/state/drover"; got != want {
		t.Errorf("default Dir = %q, want %q", got, want)
	}
	env["DROVER_STATE_DIR"] = "/custom/state"
	if got := state.Dir(getenv, "/home/u"); got != "/custom/state" {
		t.Errorf("override Dir = %q, want /custom/state", got)
	}
}

// R46: a missing state file loads as empty state, not an error.
func TestLoadMissingIsEmpty(t *testing.T) {
	s := state.Open(filepath.Join(t.TempDir(), "absent"))
	st, err := s.Load()
	if err != nil || len(st.Stacks) != 0 {
		t.Fatalf("Load = %+v, %v; want empty, nil", st, err)
	}
}

// R46: records survive a write/read cycle with every documented field.
func TestUpdateRoundTrip(t *testing.T) {
	s := state.Open(filepath.Join(t.TempDir(), "nested", "state"))
	put(t, s, "dev", "backend", group("w1", "dev-backend"))

	st, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	g, ok := st.Stacks["dev"]["backend"]
	if !ok {
		t.Fatalf("group missing: %+v", st)
	}
	p := g.Processes[0]
	if g.WorkspaceID != "w1" || g.Label != "dev-backend" || !g.CreatedByDrover ||
		p.Name != "api" || p.PaneID != "w1:p1" || !p.StartedAt.Equal(started()) || p.Restarts != 2 {
		t.Errorf("round trip lost data: %+v", g)
	}
}

// R46: when the callback fails nothing is written.
func TestUpdateCallbackErrorKeepsFile(t *testing.T) {
	s := state.Open(t.TempDir())
	put(t, s, "dev", "backend", group("w1", "dev-backend"))

	boom := errors.New("boom")
	err := s.Update(func(st *state.State) error {
		delete(st.Stacks, "dev")

		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Update error = %v, want boom", err)
	}
	st, err := s.Load()
	if err != nil || len(st.Stacks["dev"]) != 1 {
		t.Fatalf("state changed after failed update: %+v, %v", st, err)
	}
}

// AC34 (R46): concurrent writers never tear the file and none of their updates is lost.
func TestConcurrentWritersDoNotTear(t *testing.T) {
	const writers = 16
	s := state.Open(t.TempDir())

	var wg sync.WaitGroup
	errc := make(chan error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errc <- s.Update(func(st *state.State) error {
				if st.Stacks == nil {
					st.Stacks = map[string]map[string]state.Group{}
				}
				st.Stacks[fmt.Sprintf("s%d", i)] = map[string]state.Group{"g": group(fmt.Sprintf("w%d", i), "l")}

				return nil
			})
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		if err != nil {
			t.Fatal(err)
		}
	}

	raw, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	var st state.State
	if err = json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("state file is torn: %v\n%s", err, raw)
	}
	if len(st.Stacks) != writers {
		t.Errorf("got %d stacks, want %d (lost update)", len(st.Stacks), writers)
	}
}

// AC34 (R46): a write that dies before the rename leaves the previous content intact.
func TestFailedRenameKeepsPreviousState(t *testing.T) {
	dir := t.TempDir()
	good := state.Open(dir)
	put(t, good, "dev", "backend", group("w1", "dev-backend"))
	before, err := os.ReadFile(good.Path())
	if err != nil {
		t.Fatal(err)
	}

	killed := errors.New("killed before rename")
	crashing := state.Open(dir, state.WithRename(func(string, string) error { return killed }))
	err = crashing.Update(func(st *state.State) error {
		st.Stacks["dev"]["backend"] = group("w9", "clobbered")

		return nil
	})
	if !errors.Is(err, killed) {
		t.Fatalf("Update error = %v, want the rename failure", err)
	}

	after, err := os.ReadFile(good.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("state file changed after a failed rename:\nbefore %s\nafter  %s", before, after)
	}
}

// AC34 (R46): a stale half-written temp file from a killed writer is ignored and the next write succeeds.
func TestStaleTempFileIgnored(t *testing.T) {
	dir := t.TempDir()
	s := state.Open(dir)
	put(t, s, "dev", "backend", group("w1", "dev-backend"))
	if err := os.WriteFile(filepath.Join(dir, "state.json.tmp-stale"), []byte(`{"stacks": {"torn`), 0o600); err != nil {
		t.Fatal(err)
	}

	st, err := s.Load()
	if err != nil || len(st.Stacks["dev"]) != 1 {
		t.Fatalf("Load with stale temp = %+v, %v", st, err)
	}
	put(t, s, "dev", "web", group("w2", "dev-web"))
	if st, err = s.Load(); err != nil || len(st.Stacks["dev"]) != 2 {
		t.Fatalf("Update after stale temp = %+v, %v", st, err)
	}
}

// AC35 (R47): entries whose workspace herdr no longer has are dropped; live ones stay.
func TestReconcileDropsGoneWorkspaces(t *testing.T) {
	st := state.State{Stacks: map[string]map[string]state.Group{
		"dev": {"backend": group("w1", "dev-backend"), "web": group("w2", "dev-web")},
		"ops": {"jobs": group("w3", "ops-jobs")},
	}}

	dropped := st.Reconcile([]mux.Workspace{{ID: "w1", Label: "dev-backend", PaneCount: 1}})

	if len(dropped) != 2 || dropped[0] != "w2" || dropped[1] != "w3" {
		t.Errorf("dropped = %v, want [w2 w3]", dropped)
	}
	if _, ok := st.Stacks["dev"]["backend"]; !ok {
		t.Error("live group w1 was dropped")
	}
	if _, ok := st.Stacks["dev"]["web"]; ok {
		t.Error("gone group w2 was kept")
	}
	if _, ok := st.Stacks["ops"]; ok {
		t.Error("stack with no remaining groups should be removed")
	}
}

// R47 + R27: a foreign workspace with an identical label is never adopted; the stale entry is dropped.
func TestReconcileNeverAdoptsForeignWorkspace(t *testing.T) {
	st := state.State{Stacks: map[string]map[string]state.Group{"dev": {"backend": group("w1", "dev-backend")}}}

	dropped := st.Reconcile([]mux.Workspace{{ID: "w9", Label: "dev-backend", PaneCount: 3}})

	if len(dropped) != 1 || dropped[0] != "w1" {
		t.Errorf("dropped = %v, want [w1]", dropped)
	}
	for stack, groups := range st.Stacks {
		for name, g := range groups {
			if g.WorkspaceID == "w9" {
				t.Errorf("foreign workspace adopted as %s/%s", stack, name)
			}
		}
	}
}

// AC35 (R47): reconcile applied through Update persists the cleanup and leaves foreign ids out.
func TestReconcilePersists(t *testing.T) {
	s := state.Open(t.TempDir())
	put(t, s, "ops", "backend", group("w1", "ops-backend"))

	err := s.Update(func(st *state.State) error {
		st.Reconcile(nil)

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.Load()
	if err != nil || len(st.Stacks) != 0 {
		t.Fatalf("after reconcile Load = %+v, %v; want empty", st, err)
	}
}
