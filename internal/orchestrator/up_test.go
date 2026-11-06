package orchestrator_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cabrera-evil/drover-cli/internal/config"
	"github.com/cabrera-evil/drover-cli/internal/errs"
	"github.com/cabrera-evil/drover-cli/internal/orchestrator"
)

func mustUp(t *testing.T, h *harness, req orchestrator.Request) orchestrator.Result {
	t.Helper()
	res, err := orchestrator.Up(t.Context(), h.deps(), req)
	if err != nil {
		t.Fatalf("Up: %v", err)
	}

	return res
}

func twoProcs(h *harness) *config.Config {
	return h.config(procDef{procDB, nil}, procDef{procAPI, []string{procDB}})
}

// AC19 (R20-R24): any preflight problem => nothing is created, the mux sees zero calls, exit 4.
func TestUpPreflightFailureCreatesNothing(t *testing.T) {
	tests := map[string]struct {
		mutate func(h *harness, cfg *config.Config)
		code   string
	}{
		"cwd missing": {func(h *harness, cfg *config.Config) {
			setProc(cfg, procAPI, func(p *config.Process) { p.Cwd = filepath.Join(h.cwd, "missing") })
		}, errs.CodeCwdMissing},
		"port in use": {func(h *harness, _ *config.Config) { h.ports[apiPort] = true }, errs.CodePortInUse},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			cfg := twoProcs(h)
			tc.mutate(h, cfg)
			_, err := orchestrator.Up(t.Context(), h.deps(), h.request(cfg, false, procDB, procAPI))
			var e *errs.Error
			if !errors.As(err, &e) || e.Code != tc.code || errs.ExitCode(err) != errs.ExitPreflight {
				t.Fatalf("want %s exit 4, got %v", tc.code, err)
			}
			if calls := h.rec.snapshot(); len(calls) != 0 {
				t.Errorf("mux saw calls: %v", calls)
			}
			if _, statErr := os.Stat(h.store.Path()); statErr == nil {
				t.Error("state file written despite preflight failure")
			}
		})
	}
}

// R25-R28, R31: one workspace per group, labelled, panes renamed and started through the supervisor command.
func TestUpCreatesWorkspaceAndPanes(t *testing.T) {
	h := newHarness(t)
	cfg := twoProcs(h)
	res := mustUp(t, h, h.request(cfg, false, procDB, procAPI))

	if !slices.Equal(res.Created, []string{groupWeb}) {
		t.Errorf("Created = %v", res.Created)
	}
	if got := h.rec.ofOp(opCreate); len(got) != 1 || got[0][0] != h.cwd || got[0][1] != labelWeb {
		t.Fatalf("create calls = %v", got)
	}
	if got := h.rec.ofOp(opSplit); len(got) != 1 || got[0][1] != "right" || got[0][2] != h.cwd {
		t.Errorf("split calls = %v", got)
	}
	logPath := filepath.Join(h.logDir, procAPI+".log")
	envPath := filepath.Join(h.envDir, procAPI+".env.sh")
	pane := h.rec.paneOf(procAPI)
	idx := h.rec.runIdx(procAPI)
	if pane == "" || idx < 0 {
		t.Fatalf("api pane not renamed and started: %v", h.rec.snapshot())
	}
	cmd := h.rec.snapshot()[idx].Args[1]
	for _, want := range []string{selfBin, logPath, envPath} {
		if !strings.Contains(cmd, want) {
			t.Errorf("pane command %q lacks %q", cmd, want)
		}
	}
	if h.rec.find(opRename, pane, procAPI) > idx {
		t.Error("RenamePane must precede RunInPane")
	}
	assertGroupRecorded(t, h)
	fi, err := os.Stat(h.logDir)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("log dir: %v %v", fi, err)
	}
}

func assertGroupRecorded(t *testing.T, h *harness) {
	t.Helper()
	g, ok := h.group(groupWeb)
	if !ok || g.WorkspaceID != "w1" || g.Label != labelWeb || !g.CreatedByDrover {
		t.Fatalf("state group = %+v ok=%v", g, ok)
	}
	names := []string{procDB, procAPI}
	if len(g.Processes) != len(names) {
		t.Fatalf("state processes = %+v", g.Processes)
	}
	for i, p := range g.Processes {
		if p.Name != names[i] || p.PaneID != h.rec.paneOf(p.Name) || p.StartedAt.IsZero() {
			t.Errorf("state process %d = %+v", i, p)
		}
	}
}

// Layout wiring: group layout wins over the stack layout; both feed PlanLayout.
func TestUpAppliesLayout(t *testing.T) {
	tests := map[string]struct {
		group config.Layout
		want  []string // direction per split
	}{
		"stack default":       {config.Layout{}, []string{"right", "down"}},
		"group columns":       {config.Layout{Name: "columns"}, []string{"right", "right"}},
		"group rows":          {config.Layout{Name: "rows"}, []string{"down", "down"}},
		"group main-vertical": {config.Layout{Name: "main-vertical"}, []string{"right", "down"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			cfg := h.config(procDef{procDB, nil}, procDef{procCache, nil}, procDef{procAPI, nil})
			cfg.Groups[groupWeb] = config.Group{Label: labelWeb, Layout: tc.group}
			mustUp(t, h, h.request(cfg, false, procDB, procCache, procAPI))
			splits := h.rec.ofOp(opSplit)
			dirs := make([]string, 0, len(splits))
			for _, a := range splits {
				dirs = append(dirs, a[1])
			}
			if !slices.Equal(dirs, tc.want) {
				t.Errorf("split directions %v want %v", dirs, tc.want)
			}
		})
	}
}

// R21/R28: independent processes start together; a wave starts only after earlier waves are READY.
func TestUpStartsInReadinessOrderedWaves(t *testing.T) {
	h := newHarness(t)
	cfg := h.config(procDef{procDB, nil}, procDef{procCache, nil}, procDef{procAPI, []string{procDB, procCache}})
	mustUp(t, h, h.request(cfg, false, procDB, procCache, procAPI))

	runDB, runCache, runAPI := h.rec.runIdx(procDB), h.rec.runIdx(procCache), h.rec.runIdx(procAPI)
	checkDB, checkCache := h.rec.find(opCheck, procDB), h.rec.find(opCheck, procCache)
	if runDB < 0 || runCache < 0 || runAPI < 0 || checkDB < 0 || checkCache < 0 {
		t.Fatalf("missing calls: %v", h.rec.snapshot())
	}
	if max(runDB, runCache) > min(checkDB, checkCache) {
		t.Error("independent processes must both start before the wave is awaited")
	}
	if runAPI < max(checkDB, checkCache) {
		t.Error("api started before its dependencies were ready")
	}
}

// AC20 (R25): a group already running and owned is focused, nothing is created, and it is reported.
func TestUpIsIdempotent(t *testing.T) {
	h := newHarness(t)
	h.seed(groupWeb, seededID)
	h.addLive(seededID, labelWeb)
	res := mustUp(t, h, h.request(twoProcs(h), false, procDB, procAPI))

	if h.rec.find(opFocus, seededID) < 0 {
		t.Error("existing workspace not focused")
	}
	for _, o := range []string{opCreate, opSplit, opRename, opRun, opClose} {
		if got := h.rec.ofOp(o); len(got) != 0 {
			t.Errorf("%s called: %v", o, got)
		}
	}
	if !slices.Equal(res.Reused, []string{groupWeb}) || len(res.Created) != 0 {
		t.Errorf("result = %+v", res)
	}
	if out := h.out.String(); !strings.Contains(out, "already running") || !strings.Contains(out, groupWeb) {
		t.Errorf("report = %q", out)
	}
	if g, _ := h.group(groupWeb); g.WorkspaceID != seededID {
		t.Errorf("state workspace = %q", g.WorkspaceID)
	}
}

// AC21 (R26): --restart closes the owned workspace, clears its entry and recreates it.
func TestUpRestart(t *testing.T) {
	h := newHarness(t)
	h.seed(groupWeb, seededID)
	h.addLive(seededID, labelWeb)
	mustUp(t, h, h.request(twoProcs(h), true, procDB, procAPI))

	closeIdx, createIdx := h.rec.find(opClose, seededID), h.rec.find(opCreate)
	if closeIdx < 0 || createIdx < 0 || closeIdx > createIdx {
		t.Errorf("want close before create, got %v", h.rec.snapshot())
	}
	if h.mux.isLive(seededID) {
		t.Error("old workspace still live")
	}
	if g, ok := h.group(groupWeb); !ok || g.WorkspaceID == seededID {
		t.Errorf("state not replaced: %+v", g)
	}
}

// AC22 (R27): a live workspace with the same label that is not in state is foreign and never touched.
func TestUpLeavesForeignWorkspaceAlone(t *testing.T) {
	for _, restart := range []bool{false, true} {
		h := newHarness(t)
		h.addLive(foreignID, labelWeb)
		mustUp(t, h, h.request(twoProcs(h), restart, procDB, procAPI))

		if h.rec.find(opClose, foreignID) >= 0 || h.rec.find(opFocus, foreignID) >= 0 {
			t.Errorf("restart=%v: foreign workspace touched: %v", restart, h.rec.snapshot())
		}
		if !h.mux.isLive(foreignID) {
			t.Errorf("restart=%v: foreign workspace gone", restart)
		}
		if g, ok := h.group(groupWeb); !ok || g.WorkspaceID == foreignID {
			t.Errorf("restart=%v: state adopted foreign workspace: %+v", restart, g)
		}
		if len(h.rec.ofOp(opCreate)) != 1 {
			t.Errorf("restart=%v: own workspace not created", restart)
		}
	}
}

// AC35 (R27): state entries whose workspace is gone are dropped at the start of Up.
func TestUpReconcilesStaleState(t *testing.T) {
	h := newHarness(t)
	h.seed("ghost", staleID)
	mustUp(t, h, h.request(twoProcs(h), false, procDB, procAPI))

	if _, ok := h.group("ghost"); ok {
		t.Error("stale group survived reconcile")
	}
	if _, ok := h.group(groupWeb); !ok {
		t.Error("new group missing")
	}
}

// AC24 unit (R28, R29): a never-ready process => NotReadyError (exit 5), dependants not started,
// created workspaces stay open and recorded, the readiness panel goes to the injected writer.
func TestUpNotReady(t *testing.T) {
	h := newHarness(t)
	h.never[procDB] = true
	_, err := orchestrator.Up(t.Context(), h.deps(), h.request(twoProcs(h), false, procDB, procAPI))

	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.CodeReadyTimeout || errs.ExitCode(err) != errs.ExitReadiness {
		t.Fatalf("want DRV-READY-TIMEOUT exit 5, got %v", err)
	}
	if text := e.Message + e.Fix; !strings.Contains(text, procDB) || !strings.Contains(text, "drover logs db") {
		t.Errorf("error text %q", text)
	}
	if h.rec.runIdx(procAPI) >= 0 {
		t.Error("dependant api was started")
	}
	if len(h.rec.ofOp(opClose)) != 0 {
		t.Error("workspace closed on readiness failure")
	}
	if g, ok := h.group(groupWeb); !ok || g.WorkspaceID != "w1" {
		t.Errorf("state = %+v", g)
	}
	if !strings.Contains(h.out.String(), "not ready") {
		t.Errorf("readiness panel missing from reporter: %q", h.out.String())
	}
}

// AC25 (R30): cancelling during Up closes only the workspaces created in this run, drops them
// from state, keeps pre-existing ones, and exits 130 (DRV-INTERRUPTED).
func TestUpInterruptClosesOnlyOwnRunWorkspaces(t *testing.T) {
	h := newHarness(t)
	cfg := h.config(procDef{"pa", nil}, procDef{"pb", nil}, procDef{"po", nil})
	for _, g := range []string{"alpha", "beta", "old"} {
		cfg.Groups[g] = config.Group{Label: g}
	}
	h.seed("old", seededID)
	h.addLive(seededID, "old")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	h.hook["pb"] = cancel
	h.never["pb"] = true

	req := orchestrator.Request{Config: cfg, Stack: stackName, Groups: map[string][]string{
		"alpha": {"pa"}, "beta": {"pb"}, "old": {"po"},
	}}
	_, err := orchestrator.Up(ctx, h.deps(), req)

	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.CodeInterrupted {
		t.Fatalf("want DRV-INTERRUPTED, got %v", err)
	}
	if got, want := errs.ExitCode(err), errs.ExitInterrupted; got != 130 || got != want {
		t.Errorf("exit = %d, ExitInterrupted = %d, want 130", got, want)
	}
	closed := make([]string, 0, len(h.mux.created))
	for _, a := range h.rec.ofOp(opClose) {
		closed = append(closed, a[0])
	}
	slices.Sort(closed)
	created := slices.Sorted(slices.Values(h.mux.created))
	if len(created) != 2 || !slices.Equal(closed, created) {
		t.Errorf("closed %v, created this run %v", closed, created)
	}
	if h.rec.find(opClose, seededID) >= 0 || !h.mux.isLive(seededID) {
		t.Error("pre-existing workspace was closed")
	}
	st, loadErr := h.store.Load()
	if loadErr != nil || len(st.Stacks[stackName]) != 1 || st.Stacks[stackName]["old"].WorkspaceID != seededID {
		t.Errorf("state after interrupt = %+v %v", st, loadErr)
	}
}

// AC37 unit (R35): env and env_file values reach only the 0600 env script, never a mux call,
// the reporter or an error.
func TestUpNeverLeaksEnvValues(t *testing.T) {
	for _, notReady := range []bool{false, true} {
		h := newHarness(t)
		envFile := filepath.Join(h.cwd, ".env")
		if err := os.WriteFile(envFile, []byte("API_KEY="+secretFile+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := twoProcs(h)
		setProc(cfg, procAPI, func(p *config.Process) {
			p.Env = map[string]string{"TOKEN": secretEnv}
			p.EnvFile = envFile
		})
		h.never[procAPI] = notReady
		_, err := orchestrator.Up(t.Context(), h.deps(), h.request(cfg, false, procDB, procAPI))
		if (err != nil) != notReady {
			t.Fatalf("notReady=%v err=%v", notReady, err)
		}
		visible := h.rec.all() + h.out.String()
		if err != nil {
			visible += err.Error()
		}
		for _, secret := range []string{secretEnv, secretFile} {
			if strings.Contains(visible, secret) {
				t.Errorf("notReady=%v: %q leaked into mux calls, report or error", notReady, secret)
			}
		}
		assertEnvScript(t, filepath.Join(h.envDir, procAPI+".env.sh"))
	}
}

func assertEnvScript(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("env script mode %v", fi.Mode().Perm())
	}
	body, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(body), secretEnv) || !strings.Contains(string(body), secretFile) {
		t.Errorf("env script lacks the values: %v", err)
	}
}
