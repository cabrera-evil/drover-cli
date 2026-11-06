package orchestrator_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/cabrera-evil/drover-cli/internal/config"
	"github.com/cabrera-evil/drover-cli/internal/orchestrator"
)

func mustDown(t *testing.T, h *harness, req orchestrator.Request) orchestrator.Result {
	t.Helper()
	res, err := orchestrator.Down(t.Context(), h.deps(), req)
	if err != nil {
		t.Fatalf("Down: %v", err)
	}

	return res
}

// AC27 (R32): stop commands run (cwd + env via parameters), then the workspace is closed and dropped.
func TestDownStopsThenCloses(t *testing.T) {
	h := newHarness(t)
	cfg := twoProcs(h)
	setProc(cfg, procAPI, func(p *config.Process) { p.Env = map[string]string{"TOKEN": secretEnv} })
	h.seed(groupWeb, seededID)
	h.addLive(seededID, labelWeb)
	res := mustDown(t, h, h.request(cfg, false, procDB, procAPI))

	if !slices.Equal(res.Closed, []string{groupWeb}) {
		t.Errorf("Closed = %v", res.Closed)
	}
	closeIdx := h.rec.find(opClose, seededID)
	for _, name := range []string{procDB, procAPI} {
		if idx := h.rec.find(opStop, "stop "+name); idx < 0 || idx > closeIdx {
			t.Errorf("stop %s must run before close (stop=%d close=%d)", name, idx, closeIdx)
		}
	}
	for _, r := range h.runner.runs {
		if r.Dir != h.cwd {
			t.Errorf("stop %q ran in %q, want %q", r.Cmd, r.Dir, h.cwd)
		}
		if strings.Contains(r.Cmd, secretEnv) {
			t.Errorf("env value leaked into command %q", r.Cmd)
		}
		if r.Cmd == "stop "+procAPI && r.Env["TOKEN"] != secretEnv {
			t.Errorf("env not passed through the env parameter: %v", r.Env)
		}
	}
	if _, ok := h.group(groupWeb); ok {
		t.Error("state entry not removed")
	}
	if strings.Contains(h.rec.all()+h.out.String(), secretEnv) {
		t.Error("env value leaked into mux calls or report")
	}
}

// R32: a failing stop command is reported but the workspace is still closed and dropped.
func TestDownStopFailureStillCloses(t *testing.T) {
	h := newHarness(t)
	cfg := twoProcs(h)
	h.seed(groupWeb, seededID)
	h.addLive(seededID, labelWeb)
	h.runner.fail["stop "+procAPI] = true
	mustDown(t, h, h.request(cfg, false, procDB, procAPI))

	if h.rec.find(opClose, seededID) < 0 {
		t.Error("workspace not closed after a failed stop")
	}
	if _, ok := h.group(groupWeb); ok {
		t.Error("state entry not removed")
	}
	if !strings.Contains(h.out.String(), procAPI) {
		t.Errorf("failed stop not reported: %q", h.out.String())
	}
}

// R32/R27: foreign workspaces and workspaces absent from state are never stopped or closed.
func TestDownIgnoresForeignWorkspace(t *testing.T) {
	h := newHarness(t)
	h.addLive(foreignID, labelWeb)
	res := mustDown(t, h, h.request(twoProcs(h), false, procDB, procAPI))

	if len(h.rec.ofOp(opClose)) != 0 || len(h.rec.ofOp(opStop)) != 0 || !h.mux.isLive(foreignID) {
		t.Errorf("foreign workspace touched: %v", h.rec.snapshot())
	}
	if !slices.Equal(res.NotRunning, []string{groupWeb}) || !strings.Contains(h.out.String(), "not running") {
		t.Errorf("result %+v report %q", res, h.out.String())
	}
}

// Down with nothing running is not an error.
func TestDownNothingRunning(t *testing.T) {
	h := newHarness(t)
	res := mustDown(t, h, h.request(twoProcs(h), false, procDB, procAPI))

	if len(h.rec.ofOp(opClose)) != 0 || len(h.rec.ofOp(opStop)) != 0 || len(res.Closed) != 0 {
		t.Errorf("unexpected work: %v %+v", h.rec.snapshot(), res)
	}
}

// AC35: a state entry whose workspace is gone is reconciled away; it is not stopped or closed.
func TestDownReconcilesStaleState(t *testing.T) {
	h := newHarness(t)
	h.seed(groupWeb, staleID)
	h.seed("ghost", "w61")
	res := mustDown(t, h, h.request(twoProcs(h), false, procDB, procAPI))

	if len(h.rec.ofOp(opClose)) != 0 || len(h.rec.ofOp(opStop)) != 0 {
		t.Errorf("stale workspace acted on: %v", h.rec.snapshot())
	}
	for _, g := range []string{groupWeb, "ghost"} {
		if _, ok := h.group(g); ok {
			t.Errorf("stale group %q survived", g)
		}
	}
	if !slices.Equal(res.NotRunning, []string{groupWeb}) {
		t.Errorf("NotRunning = %v", res.NotRunning)
	}
}
