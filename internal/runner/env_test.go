package runner_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cabrera-evil/drover-cli/internal/config"
	"github.com/cabrera-evil/drover-cli/internal/runner"
)

const (
	envFileName = "app.env"
	keyColor    = "FORCE_COLOR"
	secretValue = "s3cr3t-value-xyz"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func buildWith(t *testing.T, envFile string, env map[string]string) (map[string]string, error) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, envFileName, envFile)

	return runner.BuildEnv(config.Process{Cwd: dir, EnvFile: envFileName, Env: env})
}

// AC28: default env has FORCE_COLOR=1.
func TestBuildEnvDefaultForceColor(t *testing.T) {
	got, err := runner.BuildEnv(config.Process{})
	if err != nil {
		t.Fatal(err)
	}
	if got[keyColor] != "1" {
		t.Fatalf("FORCE_COLOR = %q, want 1", got[keyColor])
	}
}

// AC28: an empty FORCE_COLOR removes it; any other value overrides it.
func TestBuildEnvForceColorOverride(t *testing.T) {
	got, err := runner.BuildEnv(config.Process{Env: map[string]string{keyColor: ""}})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := got[keyColor]; ok {
		t.Fatalf("FORCE_COLOR present (%q), want removed", v)
	}

	got, err = runner.BuildEnv(config.Process{Env: map[string]string{keyColor: "0"}})
	if err != nil {
		t.Fatal(err)
	}
	if got[keyColor] != "0" {
		t.Fatalf("FORCE_COLOR = %q, want 0", got[keyColor])
	}
}

// R31/R33: env_file syntax.
func TestBuildEnvFileSyntax(t *testing.T) {
	content := strings.Join([]string{
		"# a comment",
		"",
		"PLAIN=one",
		"export EXPORTED=two",
		"SINGLE='three four'",
		`DOUBLE="five six"`,
		"LITERAL=$HOME/${X}",
		"EQUALS=a=b",
		"EMPTY=",
		"",
	}, "\n")
	got, err := buildWith(t, content, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"PLAIN": "one", "EXPORTED": "two", "SINGLE": "three four", "DOUBLE": "five six",
		"LITERAL": "$HOME/${X}", "EQUALS": "a=b", "EMPTY": "",
	}
	for k, v := range want {
		if g, ok := got[k]; !ok || g != v {
			t.Errorf("%s = %q (present=%v), want %q", k, g, ok, v)
		}
	}
}

// Precedence: base defaults < env_file < process env.
func TestBuildEnvPrecedence(t *testing.T) {
	got, err := buildWith(t, "FORCE_COLOR=2\nA=file\nB=file\n", map[string]string{"B": "proc"})
	if err != nil {
		t.Fatal(err)
	}
	if got[keyColor] != "2" || got["A"] != "file" || got["B"] != "proc" {
		t.Fatalf("got %v", got)
	}
}

// R49: a malformed line names file and line number, never the value.
func TestBuildEnvMalformedLine(t *testing.T) {
	_, err := buildWith(t, "OK=1\n# c\nthis is not valid "+secretValue+"\n", nil)
	if err == nil {
		t.Fatal("want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, envFileName) || !strings.Contains(msg, "3") {
		t.Errorf("error %q must name file and line 3", msg)
	}
	if strings.Contains(msg, secretValue) {
		t.Errorf("error leaks value: %q", msg)
	}
}

// R49: a missing env_file names the file.
func TestBuildEnvMissingFile(t *testing.T) {
	_, err := runner.BuildEnv(config.Process{Cwd: t.TempDir(), EnvFile: "nope.env"})
	if err == nil || !strings.Contains(err.Error(), "nope.env") {
		t.Fatalf("err = %v, want it to name nope.env", err)
	}
}
