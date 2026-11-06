//nolint:testpackage // exercises unexported wiring
package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cabrera-evil/drover-cli/internal/errs"
)

// run executes the root command with args and returns stdout, stderr and the error.
func run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(args)
	err := root.Execute()

	return out.String(), errb.String(), err
}

// project writes drover.yaml in a fresh dir, chdirs there with a fresh HOME, returns the path.
func project(t *testing.T, content string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	p := filepath.Join(dir, "drover.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	return p
}

const validCfg = `version: 1
name: demo
processes:
  api:
    cmd: run-api
    env: {API_TOKEN: s3cr3t-token}
    tags: [backend]
`

func TestConfigCheckOK(t *testing.T) {
	project(t, validCfg)
	out, _, err := run(t, "config", "check")
	if err != nil || !strings.Contains(strings.ToLower(out), "ok") {
		t.Errorf("err=%v out=%q", err, out)
	}
}

// AC1: unknown key => exit 3, file:line, key path, DRV code.
func TestConfigCheckUnknownKey(t *testing.T) {
	p := project(t, "version: 1\nname: demo\nfoo: bar\n")
	_, _, err := run(t, "config", "check")
	if err == nil || errs.ExitCode(err) != 3 {
		t.Fatalf("err=%v exit=%d", err, errs.ExitCode(err))
	}
	msg := err.Error()
	if !strings.Contains(msg, p+":3 foo") || !strings.Contains(msg, "(DRV-") {
		t.Errorf("msg = %q", msg)
	}
}

// AC2: version 2 => exit 3 pointing at `config migrate`.
func TestConfigCheckVersion2(t *testing.T) {
	project(t, "version: 2\nname: demo\n")
	_, _, err := run(t, "config", "check")
	if err == nil || errs.ExitCode(err) != 3 || !strings.Contains(err.Error(), "config migrate") {
		t.Errorf("err=%v exit=%d", err, errs.ExitCode(err))
	}
}

// AC3 through the CLI: --config wins over the cwd file.
func TestConfigCheckFlagBeatsCwd(t *testing.T) {
	project(t, "version: 1\nname: cwdone\n")
	other := filepath.Join(t.TempDir(), "other.yaml")
	if err := os.WriteFile(other, []byte("version: 1\nname: other\nfoo: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := run(t, "--config", other, "config", "check")
	if err == nil || !strings.Contains(err.Error(), other) {
		t.Errorf("flag file should have been used: %v", err)
	}
}

func TestConfigCheckNoConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	_, _, err := run(t, "config", "check")
	if err == nil || errs.ExitCode(err) != 3 || !strings.Contains(err.Error(), "(DRV-") {
		t.Errorf("err=%v exit=%d", err, errs.ExitCode(err))
	}
}

// Secrets: show prints keys and sources only, never values.
func TestConfigShowRedactsEnvValues(t *testing.T) {
	p := project(t, validCfg)
	out, errOut, err := run(t, "config", "show")
	if err != nil {
		t.Fatal(err)
	}
	all := out + errOut
	for _, want := range []string{"demo", "api", "API_TOKEN", p} {
		if !strings.Contains(all, want) {
			t.Errorf("show output missing %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "s3cr3t-token") {
		t.Errorf("show leaks env value:\n%s", all)
	}
}

func TestConfigShowRedactsEnvFileValues(t *testing.T) {
	p := project(t, `version: 1
name: demo
processes:
  api: {cmd: a, env_file: ./api.env}
`)
	if err := os.WriteFile(filepath.Join(filepath.Dir(p), "api.env"), []byte("DB_PASS=hunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut, err := run(t, "config", "show")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out+errOut, "hunter2") {
		t.Errorf("show leaks env_file value:\n%s", out)
	}
}

// AC13: migrate on v1 => "already current", exit 0, file unchanged.
func TestConfigMigrateNoop(t *testing.T) {
	p := project(t, validCfg)
	before, _ := os.ReadFile(p)
	out, _, err := run(t, "config", "migrate")
	if err != nil || !strings.Contains(out, "already current") {
		t.Errorf("err=%v out=%q", err, out)
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Error("migrate modified a v1 file")
	}
}

// R73: --help works for the new commands.
func TestConfigHelp(t *testing.T) {
	for _, args := range [][]string{
		{"config", "--help"}, {"config", "check", "--help"}, {"config", "show", "--help"},
		{"config", "migrate", "--help"}, {"stacks", "--help"},
	} {
		if _, _, err := run(t, args...); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
}
