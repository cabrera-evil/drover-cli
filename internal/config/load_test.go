package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cabrera-evil/drover-cli/internal/config"
	"github.com/cabrera-evil/drover-cli/internal/errs"
)

var drvCode = regexp.MustCompile(`^DRV(-[A-Z]+)+$`)

// write creates dir/name with content and returns its path.
func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return p
}

// loadErr loads content and returns the *errs.Error (fails if none).
func loadErr(t *testing.T, content string) (*errs.Error, string) {
	t.Helper()
	path := write(t, t.TempDir(), "drover.yaml", content)
	_, err := config.Load(path)
	var de *errs.Error
	if !errors.As(err, &de) {
		t.Fatalf("want *errs.Error, got %v", err)
	}
	if errs.ExitCode(err) != errs.ExitConfig {
		t.Errorf("exit code = %d, want %d", errs.ExitCode(err), errs.ExitConfig)
	}
	return de, path
}

func loadOK(t *testing.T, home, content string) (*config.Config, string) {
	t.Helper()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	path := write(t, dir, "drover.yaml", content)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	return cfg, dir
}

func TestLoadFullFixture(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Load("testdata/full.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Version != 1 || cfg.Name != "demo" || cfg.Description != "Demo stack" {
		t.Errorf("header = %+v", cfg)
	}
	if len(cfg.Processes) != 3 {
		t.Errorf("processes = %d", len(cfg.Processes))
	}
	// R4: defaults apply when the process does not set the key.
	if got := cfg.Processes["db"].Restart; got != "on-failure" {
		t.Errorf("defaults.restart not applied: %q", got)
	}
	api := cfg.Processes["api"]
	if api.Cmd != "npm run api" || len(api.DependsOn) != 1 || api.DependsOn[0] != "db" || api.Port.Value != 3000 {
		t.Errorf("api = %+v", api)
	}
	if api.Ready.Kind != "http" || api.Ready.Value != "http://localhost:3000/health" ||
		api.Ready.Timeout != 30*time.Second {
		t.Errorf("api.ready = %+v", api.Ready)
	}
	if cfg.Source == "" || !filepath.IsAbs(cfg.Source) {
		t.Errorf("Source should be absolute: %q", cfg.Source)
	}
}

// AC1 (R1): unknown top-level key.
func TestUnknownKeyTopLevel(t *testing.T) {
	de, path := loadErr(t, "version: 1\nname: demo\nfoo: bar\n")
	if de.File != path || de.Line != 3 || de.Key != "foo" || !drvCode.MatchString(de.Code) {
		t.Errorf("err = %+v", de)
	}
	if want := path + ":3 foo"; !strings.Contains(de.Error(), want) {
		t.Errorf("Error() = %q, want it to contain %q", de.Error(), want)
	}
}

func TestUnknownKeyNested(t *testing.T) {
	de, _ := loadErr(t, "version: 1\nname: demo\nprocesses:\n  api:\n    cmd: x\n    bogus: 1\n")
	if de.Key != "processes.api.bogus" || de.Line != 6 {
		t.Errorf("key=%q line=%d", de.Key, de.Line)
	}
}

// AC2 (R1): unsupported version points to `config migrate`.
func TestUnsupportedVersion(t *testing.T) {
	de, _ := loadErr(t, "version: 2\nname: demo\n")
	if de.Key != "version" || !drvCode.MatchString(de.Code) {
		t.Errorf("err = %+v", de)
	}
	if !strings.Contains(de.Error()+de.Fix, "config migrate") {
		t.Errorf("should mention `config migrate`: %q / %q", de.Error(), de.Fix)
	}
}

func TestMissingVersionRejected(t *testing.T) {
	de, _ := loadErr(t, "name: demo\n")
	if de.Key != "version" {
		t.Errorf("key = %q", de.Key)
	}
}

// AC5 (R5): cwd expansion.
func TestCwdExpansion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DROVER_TEST_BASE", "/opt/base")
	cfg, dir := loadOK(t, home, `version: 1
name: demo
processes:
  tilde: {cwd: "~/x", cmd: a}
  envhome: {cwd: "${HOME}/x", cmd: a}
  envvar: {cwd: "${DROVER_TEST_BASE}/y", cmd: a}
  rel: {cwd: ./sub/dir, cmd: a}
  bare: {cmd: a}
`)
	want := map[string]string{
		"tilde":   filepath.Join(home, "x"),
		"envhome": filepath.Join(home, "x"),
		"envvar":  "/opt/base/y",
		"rel":     filepath.Join(dir, "sub", "dir"),
		"bare":    dir,
	}
	for name, w := range want {
		got := cfg.Processes[name].Cwd
		if !filepath.IsAbs(got) || got != w {
			t.Errorf("%s cwd = %q, want %q", name, got, w)
		}
	}
}

// AC6 (R6): ready checkers.
func TestReadyTwoCheckersRejected(t *testing.T) {
	de, _ := loadErr(t, `version: 1
name: demo
processes:
  api:
    cmd: a
    ready:
      http: http://localhost:1/h
      tcp: localhost:1
      timeout: 5s
`)
	if de.Key != "processes.api.ready" || !drvCode.MatchString(de.Code) {
		t.Errorf("err = %+v", de)
	}
}

func TestReadyKinds(t *testing.T) {
	cfg, _ := loadOK(t, t.TempDir(), `version: 1
name: demo
processes:
  none1: {cmd: a}
  http1: {cmd: a, ready: {http: "http://x/h", timeout: 5s}}
  tcp1: {cmd: a, ready: {tcp: "localhost:80", timeout: 5s}}
  log1: {cmd: a, ready: {log: "listening on", timeout: 5s}}
  delay1: {cmd: a, ready: {delay: 2s}}
  explicit: {cmd: a, ready: {none: true}}
`)
	want := map[string]string{
		"none1": "none", "http1": "http", "tcp1": "tcp", "log1": "log", "delay1": "delay", "explicit": "none",
	}
	for name, kind := range want {
		if got := cfg.Processes[name].Ready.Kind; got != kind {
			t.Errorf("%s ready kind = %q, want %q", name, got, kind)
		}
	}
}

// AC7 (R7): port from env file.
func TestPortFromEnvFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	write(t, dir, "api/.env", "PORT=4123\nAPI_TOKEN=hunter2\n")
	path := write(t, dir, "drover.yaml", `version: 1
name: demo
processes:
  api:
    cmd: a
    port: {from_env_file: ./api/.env, key: PORT}
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Processes["api"].Port.Value; got != 4123 {
		t.Errorf("port = %d", got)
	}
}

func TestPortMissingKeyNeverLeaksValue(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	envFile := write(t, dir, "api.env", "API_TOKEN=hunter2\n")
	path := write(t, dir, "drover.yaml", `version: 1
name: demo
processes:
  api:
    cmd: a
    port: {from_env_file: ./api.env, key: PORT}
`)
	_, err := config.Load(path)
	var de *errs.Error
	if !errors.As(err, &de) {
		t.Fatalf("want *errs.Error, got %v", err)
	}
	msg := de.Error() + " " + de.Fix
	if !strings.Contains(msg, "PORT") || !strings.Contains(msg, filepath.Base(envFile)) {
		t.Errorf("should name file and key: %q", msg)
	}
	if strings.Contains(msg, "hunter2") {
		t.Errorf("secret leaked: %q", msg)
	}
	if errs.ExitCode(err) != errs.ExitConfig {
		t.Errorf("exit = %d", errs.ExitCode(err))
	}
}

// AC8 (R8): templates, shallow merge, maps merge by key, process wins.
func TestTemplateMerge(t *testing.T) {
	cfg, _ := loadOK(t, t.TempDir(), `version: 1
name: demo
templates:
  node:
    cmd: npm run dev
    tags: [js]
    env: {NODE_ENV: development, SHARED: from-template}
processes:
  api:
    use: node
    cmd: npm run api
    env: {SHARED: from-process, EXTRA: "1"}
  plain:
    use: node
`)
	api := cfg.Processes["api"]
	if api.Cmd != "npm run api" {
		t.Errorf("process cmd should win: %q", api.Cmd)
	}
	wantEnv := map[string]string{"NODE_ENV": "development", "SHARED": "from-process", "EXTRA": "1"}
	if len(api.Env) != len(wantEnv) {
		t.Errorf("env = %v", api.Env)
	}
	for k, v := range wantEnv {
		if api.Env[k] != v {
			t.Errorf("env[%s] = %q, want %q", k, api.Env[k], v)
		}
	}
	plain := cfg.Processes["plain"]
	if plain.Cmd != "npm run dev" || len(plain.Tags) != 1 || plain.Tags[0] != "js" {
		t.Errorf("plain = %+v", plain)
	}
}

func TestUnknownTemplateRejected(t *testing.T) {
	de, _ := loadErr(t, "version: 1\nname: demo\nprocesses:\n  api:\n    use: nope\n    cmd: a\n")
	if de.Key != "processes.api.use" {
		t.Errorf("key = %q", de.Key)
	}
}

// AC9 (R9,R10): groups and presets parse; selection itself is S2.
func TestGroupsAndPresets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Load("testdata/full.yaml")
	if err != nil {
		t.Fatal(err)
	}
	be := cfg.Groups["backend"]
	if be.Label != "Backend" || len(be.TagsAny) != 1 || be.TagsAny[0] != "backend" || be.Layout.Name != "columns" {
		t.Errorf("backend = %+v", be)
	}
	if fe := cfg.Groups["frontend"]; len(fe.Processes) != 1 || fe.Processes[0] != "web" {
		t.Errorf("frontend = %+v", fe)
	}
	if st := cfg.Groups["strict"]; len(st.TagsAll) != 2 {
		t.Errorf("strict = %+v", st)
	}
	if p := cfg.Presets["be"]; len(p.TagsAny) != 1 || p.TagsAny[0] != "backend" {
		t.Errorf("preset be = %+v", p)
	}
	// R10: builtin `all` preset means every process (tag filter "*").
	if p, ok := cfg.Presets["all"]; !ok || len(p.TagsAny) != 1 || p.TagsAny[0] != "*" {
		t.Errorf("builtin all preset = %+v ok=%v", p, ok)
	}
}

// AC10 (R11): layouts.
func TestLayoutNamed(t *testing.T) {
	for _, name := range []string{"default", "columns", "rows", "main-vertical"} {
		t.Run(name, func(t *testing.T) {
			cfg, _ := loadOK(t, t.TempDir(), "version: 1\nname: demo\nlayout: "+name+"\n")
			if cfg.Layout.Name != name || cfg.Layout.Tree != nil {
				t.Errorf("layout = %+v", cfg.Layout)
			}
		})
	}
}

func TestLayoutUnspecifiedIsDefault(t *testing.T) {
	cfg, _ := loadOK(t, t.TempDir(), "version: 1\nname: demo\n")
	if cfg.Layout.Name != "default" {
		t.Errorf("layout = %+v", cfg.Layout)
	}
}

func TestLayoutSplitTree(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Load("testdata/full.yaml")
	if err != nil {
		t.Fatal(err)
	}
	tree := cfg.Layout.Tree
	if tree == nil || tree.Dir != "columns" || len(tree.Children) != 2 {
		t.Fatalf("tree = %+v", tree)
	}
	if tree.Children[0].Group != "backend" {
		t.Errorf("leaf = %+v", tree.Children[0])
	}
	if r := tree.Children[1]; r.Dir != "rows" || len(r.Children) != 2 || r.Children[1].Group != "strict" {
		t.Errorf("nested = %+v", r)
	}
}

func TestLayoutInvalid(t *testing.T) {
	head := "version: 1\nname: demo\ngroups:\n  g: {tags_any: [a]}\n"
	tests := map[string]string{
		"unknown name":     "layout: diagonal\n",
		"bad split dir":    "layout:\n  split: diagonal\n  children:\n    - group: g\n",
		"empty children":   "layout:\n  split: rows\n  children: []\n",
		"unknown group":    "layout:\n  split: rows\n  children:\n    - group: nope\n",
		"leaf and split":   "layout:\n  split: rows\n  group: g\n  children:\n    - group: g\n",
		"nested bad split": "layout:\n  split: rows\n  children:\n    - split: zig\n      children:\n        - group: g\n",
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			de, _ := loadErr(t, head+body)
			if !strings.HasPrefix(de.Key, "layout") || !drvCode.MatchString(de.Code) {
				t.Errorf("err = %+v", de)
			}
		})
	}
}

// AC14 (R12): logdir default.
func TestLogDirDefault(t *testing.T) {
	home := t.TempDir()
	cfg, _ := loadOK(t, home, "version: 1\nname: demo\n")
	want := filepath.Join(home, ".local", "state", "drover", "logs", "demo")
	if cfg.LogDir != want {
		t.Errorf("LogDir = %q, want %q", cfg.LogDir, want)
	}
}

func TestLogDirExplicit(t *testing.T) {
	home := t.TempDir()
	cfg, _ := loadOK(t, home, "version: 1\nname: demo\nlogdir: ~/mylogs\n")
	if want := filepath.Join(home, "mylogs"); cfg.LogDir != want {
		t.Errorf("LogDir = %q, want %q", cfg.LogDir, want)
	}
}

// Secrets: env / env_file values never appear in errors.
func TestEnvValuesNeverInErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	write(t, dir, "x.env", "TOKEN=hunter2\n")
	path := write(t, dir, "drover.yaml", `version: 1
name: demo
processes:
  api:
    cmd: a
    env: {SECRET: s3cr3t-value}
    env_file: ./x.env
    bogus: 1
`)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("want error")
	}
	for _, leak := range []string{"s3cr3t-value", "hunter2"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error leaks %q: %v", leak, err)
		}
	}
}
