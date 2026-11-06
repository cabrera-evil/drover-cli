package orchestrator_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cabrera-evil/drover-cli/internal/config"
	"github.com/cabrera-evil/drover-cli/internal/mux"
	"github.com/cabrera-evil/drover-cli/internal/orchestrator"
	"github.com/cabrera-evil/drover-cli/internal/probe"
	"github.com/cabrera-evil/drover-cli/internal/state"
)

const (
	stackName    = "demo"
	groupWeb     = "web"
	labelWeb     = "Web"
	procAPI      = "api"
	procDB       = "db"
	procCache    = "cache"
	selfBin      = "/opt/drover/bin/drover"
	secretEnv    = "s3cr3t-env-AAA"
	secretFile   = "s3cr3t-file-BBB"
	apiPort      = 8080
	readyTimeout = 5 * time.Second
	foreignID    = "w9"
	seededID     = "w50"
	staleID      = "w60"
	opCreate     = "create"
	opSplit      = "split"
	opRename     = "rename"
	opRun        = "run"
	opFocus      = "focus"
	opClose      = "close"
	opCheck      = "check"
	opStop       = "stop"
)

// Compile-time contracts for the fakes.
var (
	_ mux.Multiplexer            = (*fakeMux)(nil)
	_ probe.Clock                = (*fakeClock)(nil)
	_ probe.Checker              = (*fakeChecker)(nil)
	_ orchestrator.CommandRunner = (*fakeRunner)(nil)
	_ orchestrator.PortChecker   = fakePorts(nil)
)

type call struct {
	Op   string
	Args []string
}

// recorder keeps one ordered log of every fake interaction so tests can assert ordering across fakes.
type recorder struct {
	mu    sync.Mutex
	calls []call
}

func (r *recorder) add(op string, args ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call{op, args})
}

func (r *recorder) snapshot() []call {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.calls)
}

// find returns the index of the first call with op whose leading args equal args, or -1.
func (r *recorder) find(op string, args ...string) int {
	return slices.IndexFunc(r.snapshot(), func(c call) bool {
		return c.Op == op && len(c.Args) >= len(args) && slices.Equal(c.Args[:len(args)], args)
	})
}

// all returns every call argument joined, for secret scans.
func (r *recorder) all() string {
	var b strings.Builder
	for _, c := range r.snapshot() {
		b.WriteString(c.Op + " " + strings.Join(c.Args, " ") + "\n")
	}

	return b.String()
}

// ofOp returns the args of every call with the given op, in order.
func (r *recorder) ofOp(op string) [][]string {
	var out [][]string
	for _, c := range r.snapshot() {
		if c.Op == op {
			out = append(out, c.Args)
		}
	}

	return out
}

// paneOf returns the pane renamed to name, or "".
func (r *recorder) paneOf(name string) string {
	for _, a := range r.ofOp(opRename) {
		if a[1] == name {
			return a[0]
		}
	}

	return ""
}

// runIdx returns the index of the RunInPane call for the pane named name, or -1.
func (r *recorder) runIdx(name string) int {
	pane := r.paneOf(name)
	if pane == "" {
		return -1
	}

	return r.find(opRun, pane)
}

// fakeMux records every call, honours a cancelled context like the real adapter, and tracks live workspaces.
type fakeMux struct {
	rec     *recorder
	mu      sync.Mutex
	live    []mux.Workspace
	next    int
	panes   map[string]int
	created []string
}

func (f *fakeMux) ListWorkspaces(ctx context.Context) ([]mux.Workspace, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.rec.add("list")
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.live), nil
}

func (f *fakeMux) CreateWorkspace(ctx context.Context, cwd, label string) (mux.Created, error) {
	if err := ctx.Err(); err != nil {
		return mux.Created{}, err
	}
	f.rec.add(opCreate, cwd, label)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := "w" + strconv.Itoa(f.next)
	f.live = append(f.live, mux.Workspace{ID: id, Label: label, PaneCount: 1})
	f.created = append(f.created, id)
	f.panes[id] = 1

	return mux.Created{WorkspaceID: id, RootPaneID: id + ":p1"}, nil
}

func (f *fakeMux) SplitPane(ctx context.Context, paneID string, dir mux.Direction, cwd string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.rec.add(opSplit, paneID, string(dir), cwd)
	f.mu.Lock()
	defer f.mu.Unlock()
	ws, _, _ := strings.Cut(paneID, ":")
	f.panes[ws]++

	return ws + ":p" + strconv.Itoa(f.panes[ws]), nil
}

func (f *fakeMux) RenamePane(ctx context.Context, paneID, label string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.rec.add(opRename, paneID, label)

	return nil
}

func (f *fakeMux) RunInPane(ctx context.Context, paneID, cmd string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.rec.add(opRun, paneID, cmd)

	return nil
}

func (f *fakeMux) ReadPane(context.Context, string) (string, error) { return "", nil }

func (f *fakeMux) FocusWorkspace(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.rec.add(opFocus, id)

	return nil
}

func (f *fakeMux) CloseWorkspace(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.rec.add(opClose, id)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.live = slices.DeleteFunc(f.live, func(w mux.Workspace) bool { return w.ID == id })

	return nil
}

func (f *fakeMux) isLive(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.ContainsFunc(f.live, func(w mux.Workspace) bool { return w.ID == id })
}

// fakeClock fires After at once and moves time forward, so readiness waits run in no real time.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	ch := make(chan time.Time, 1)
	ch <- c.now

	return ch
}

type fakeChecker struct {
	h    *harness
	name string
}

func (c *fakeChecker) Check(context.Context) (bool, error) {
	c.h.rec.add(opCheck, c.name)
	if fn := c.h.hook[c.name]; fn != nil {
		fn()
	}

	return !c.h.never[c.name], nil
}

type stopRun struct {
	Dir string
	Env map[string]string
	Cmd string
}

type fakeRunner struct {
	rec  *recorder
	mu   sync.Mutex
	fail map[string]bool
	runs []stopRun
}

func (f *fakeRunner) Run(_ context.Context, dir string, env map[string]string, cmd string) error {
	f.rec.add(opStop, cmd)
	f.mu.Lock()
	f.runs = append(f.runs, stopRun{dir, env, cmd})
	f.mu.Unlock()
	if f.fail[cmd] {
		return errors.New("stop failed")
	}

	return nil
}

// safeBuf is a goroutine-safe Reporter.
type safeBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.b.String()
}

// harness wires every fake around a real state.Store in a temp dir.
type harness struct {
	t      *testing.T
	rec    *recorder
	mux    *fakeMux
	store  *state.Store
	clock  *fakeClock
	runner *fakeRunner
	out    *safeBuf
	ports  fakePorts
	never  map[string]bool
	hook   map[string]func()
	cwd    string
	logDir string
	envDir string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	rec := &recorder{}
	root := t.TempDir()

	return &harness{
		t:      t,
		rec:    rec,
		mux:    &fakeMux{rec: rec, panes: map[string]int{}},
		store:  state.Open(filepath.Join(root, "state")),
		clock:  &fakeClock{now: time.Unix(1_700_000_000, 0)},
		runner: &fakeRunner{rec: rec, fail: map[string]bool{}},
		out:    &safeBuf{},
		ports:  fakePorts{},
		never:  map[string]bool{},
		hook:   map[string]func(){},
		cwd:    t.TempDir(),
		logDir: filepath.Join(root, "logs", stackName),
		envDir: filepath.Join(root, "state", "env"),
	}
}

func (h *harness) deps() orchestrator.Deps {
	return orchestrator.Deps{
		Mux:   h.mux,
		Store: h.store,
		Ports: h.ports,
		NewChecker: func(p config.Process, _ int) (probe.Checker, error) {
			return &fakeChecker{h: h, name: strings.TrimPrefix(p.Cmd, "serve ")}, nil
		},
		Clock:          h.clock,
		Runner:         h.runner,
		Reporter:       h.out,
		SelfBin:        selfBin,
		LogDir:         h.logDir,
		EnvDir:         h.envDir,
		PollInterval:   time.Second,
		DefaultTimeout: readyTimeout,
	}
}

type procDef struct {
	name string
	deps []string
}

// config builds a resolved stack with one group "web"; processes follow defs order.
func (h *harness) config(defs ...procDef) *config.Config {
	cfg := &config.Config{
		Name:      stackName,
		Layout:    config.Layout{Name: "default"},
		Processes: map[string]config.Process{},
		Groups:    map[string]config.Group{groupWeb: {Label: labelWeb}},
		Order:     make([]string, 0, len(defs)),
	}
	for _, d := range defs {
		cfg.Order = append(cfg.Order, d.name)
		p := config.Process{
			Cmd: "serve " + d.name, Cwd: h.cwd, Stop: "stop " + d.name, DependsOn: d.deps, Restart: "never",
			Ready: config.Ready{Kind: "none", Timeout: readyTimeout},
		}
		if d.name == procAPI {
			p.Port.Value = apiPort
		}
		cfg.Processes[d.name] = p
	}

	return cfg
}

func (h *harness) request(cfg *config.Config, restart bool, names ...string) orchestrator.Request {
	return orchestrator.Request{
		Config: cfg, Stack: stackName, Groups: map[string][]string{groupWeb: names}, Restart: restart,
	}
}

// seed records an owned group in the state store.
func (h *harness) seed(group, ws string) {
	h.t.Helper()
	err := h.store.Update(func(s *state.State) error {
		if s.Stacks == nil {
			s.Stacks = map[string]map[string]state.Group{}
		}
		if s.Stacks[stackName] == nil {
			s.Stacks[stackName] = map[string]state.Group{}
		}
		s.Stacks[stackName][group] = state.Group{
			WorkspaceID: ws, Label: group, CreatedByDrover: true,
			Processes: []state.Process{{Name: procDB, PaneID: ws + ":p1"}},
		}

		return nil
	})
	if err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) addLive(id, label string) {
	h.mux.mu.Lock()
	defer h.mux.mu.Unlock()
	h.mux.live = append(h.mux.live, mux.Workspace{ID: id, Label: label, PaneCount: 1})
}

func (h *harness) group(group string) (state.Group, bool) {
	h.t.Helper()
	st, err := h.store.Load()
	if err != nil {
		h.t.Fatal(err)
	}
	g, ok := st.Stacks[stackName][group]

	return g, ok
}

func setProc(cfg *config.Config, name string, fn func(*config.Process)) {
	p := cfg.Processes[name]
	fn(&p)
	cfg.Processes[name] = p
}
