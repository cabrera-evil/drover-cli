package orchestrator_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/cabrera-evil/drover-cli/internal/config"
	"github.com/cabrera-evil/drover-cli/internal/errs"
	"github.com/cabrera-evil/drover-cli/internal/orchestrator"
)

func deps(order []string, d map[string][]string) *config.Config {
	cfg := &config.Config{Order: order, Processes: map[string]config.Process{}}
	for _, n := range order {
		cfg.Processes[n] = config.Process{DependsOn: d[n]}
	}

	return cfg
}

// AC17 (R21): a depends_on b depends_on c => start order c, b, a; independent processes share a wave.
func TestLevels(t *testing.T) {
	tests := []struct {
		name  string
		order []string
		deps  map[string][]string
		names []string
		want  [][]string
	}{
		{"AC17 chain", []string{"a", "b", "c"}, map[string][]string{"a": {"b"}, "b": {"c"}}, []string{"a", "b", "c"},
			[][]string{{"c"}, {"b"}, {"a"}}},
		{"independent start together in declaration order", []string{"x", "y", "z"}, nil, []string{"x", "y", "z"},
			[][]string{{"x", "y", "z"}}},
		{"diamond", []string{"app", "l", "r", "base"},
			map[string][]string{"app": {"l", "r"}, "l": {"base"}, "r": {"base"}}, []string{"app", "l", "r", "base"},
			[][]string{{"base"}, {"l", "r"}, {"app"}}},
		{"dependency outside the selection is ignored", []string{"a", "b"}, map[string][]string{"a": {"b"}},
			[]string{"a"}, [][]string{{"a"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := orchestrator.Levels(deps(tc.order, tc.deps), tc.names)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.EqualFunc(got, tc.want, slices.Equal[[]string]) {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

// AC18 (R22): cycle => error listing the path, exit 4, before anything is created.
func TestLevelsCycle(t *testing.T) {
	cfg := deps([]string{"a", "b"}, map[string][]string{"a": {"b"}, "b": {"a"}})
	_, err := orchestrator.Levels(cfg, []string{"a", "b"})
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("want *errs.Error, got %v", err)
	}
	if e.Code != errs.CodeDepCycle || errs.ExitCode(err) != errs.ExitPreflight || errs.ExitPreflight != 4 {
		t.Errorf("code %s exit %d", e.Code, errs.ExitCode(err))
	}
	if !strings.Contains(err.Error(), "a -> b -> a") {
		t.Errorf("cycle path missing: %q", err)
	}
}

// R22: unknown dependency detected.
func TestLevelsUnknownDependency(t *testing.T) {
	_, err := orchestrator.Levels(deps([]string{"a"}, map[string][]string{"a": {"ghost"}}), []string{"a"})
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.CodeDepUnknown || errs.ExitCode(err) != errs.ExitPreflight {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "ghost") || !strings.Contains(err.Error(), "a") {
		t.Errorf("must name process and dependency: %q", err)
	}
}

type fakePorts map[int]bool

func (f fakePorts) InUse(p int) bool { return f[p] }

const secret = "s3cr3t-value"

func problems(t *testing.T, err error) []*errs.Error {
	t.Helper()
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		t.Fatalf("want joined problems, got %v", err)
	}
	out := make([]*errs.Error, 0, len(joined.Unwrap()))
	for _, p := range joined.Unwrap() {
		var e *errs.Error
		if !errors.As(p, &e) {
			t.Fatalf("problem is not *errs.Error: %v", p)
		}
		out = append(out, e)
	}

	return out
}

// AC19 (R23, R24): missing cwd, missing env file and busy port at once => all reported, nothing created.
func TestPreflightReportsAllProblems(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.env")
	if err := os.WriteFile(good, []byte("PORT=1\nTOKEN="+secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Order: []string{"nocwd", "noenv", "busy", "fine"},
		Processes: map[string]config.Process{
			"nocwd": {Cwd: filepath.Join(dir, "missing")},
			"noenv": {Cwd: dir, EnvFile: filepath.Join(dir, "missing.env"), Env: map[string]string{"K": secret}},
			"busy":  {Cwd: dir, EnvFile: good, Port: config.Port{Value: 8080}},
			"fine":  {Cwd: dir, EnvFile: good, Port: config.Port{Value: 9090}},
		},
	}
	before, _ := os.ReadDir(dir)
	err := orchestrator.Preflight(cfg, []string{"nocwd", "noenv", "busy", "fine"}, fakePorts{8080: true})
	if err == nil {
		t.Fatal("want preflight error")
	}
	if errs.ExitCode(err) != errs.ExitPreflight {
		t.Errorf("exit %d", errs.ExitCode(err))
	}
	want := map[string]string{
		"processes.nocwd": errs.CodeCwdMissing,
		"processes.noenv": errs.CodeEnvFileMissing,
		"processes.busy":  errs.CodePortInUse,
	}
	got := map[string]string{}
	for _, p := range problems(t, err) {
		got[p.Key] = p.Code
		if p.Fix == "" || p.Message == "" {
			t.Errorf("%s: message and fix required", p.Key)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for k, c := range want {
		if got[k] != c {
			t.Errorf("%s: got %q want %q", k, got[k], c)
		}
	}
	if strings.Contains(err.Error(), secret) {
		t.Error("env values must never be printed")
	}
	if after, _ := os.ReadDir(dir); len(after) != len(before) {
		t.Error("preflight must create nothing")
	}
}

// R23: port.from_env_file is checked too; a cycle is reported along with other problems.
func TestPreflightEnvFileForPortAndCycle(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Order: []string{"a", "b", "p"},
		Processes: map[string]config.Process{
			"a": {Cwd: dir, DependsOn: []string{"b"}},
			"b": {Cwd: dir, DependsOn: []string{"a"}},
			"p": {Cwd: dir, Port: config.Port{FromEnvFile: filepath.Join(dir, "none.env"), Key: "PORT"}},
		},
	}
	err := orchestrator.Preflight(cfg, []string{"a", "b", "p"}, fakePorts{})
	probs := problems(t, err)
	codes := make([]string, 0, len(probs))
	for _, p := range probs {
		codes = append(codes, p.Code)
	}
	slices.Sort(codes)
	want := []string{errs.CodeDepCycle, errs.CodeEnvFileMissing}
	slices.Sort(want)
	if !slices.Equal(codes, want) {
		t.Errorf("got %v want %v", codes, want)
	}
}

func TestPreflightOK(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Order:     []string{"a"},
		Processes: map[string]config.Process{"a": {Cwd: dir, Port: config.Port{Value: 7000}}},
	}
	if err := orchestrator.Preflight(cfg, []string{"a"}, fakePorts{}); err != nil {
		t.Fatal(err)
	}
}

// R73: new stable codes follow the DRV-AREA-NAME slug convention and are distinct.
func TestPreflightCodes(t *testing.T) {
	re := regexp.MustCompile(`^DRV(-[A-Z]+)+$`)
	seen := map[string]bool{}
	for _, c := range []string{
		errs.CodePortInUse, errs.CodeCwdMissing, errs.CodeEnvFileMissing, errs.CodeDepCycle, errs.CodeDepUnknown,
		errs.CodeUnknownKey, errs.CodeUnsupportedVersion, errs.CodeInvalidValue, errs.CodeNotFound, errs.CodeUsage,
	} {
		if !re.MatchString(c) || seen[c] {
			t.Errorf("bad or duplicate code %q", c)
		}
		seen[c] = true
	}
}
