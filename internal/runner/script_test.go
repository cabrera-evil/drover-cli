package runner_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cabrera-evil/drover-cli/internal/runner"
)

const scriptSuffix = ".env.sh"

func source(t *testing.T, path, key string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "sh", "-c", `. "$1"; printf %s "$`+key+`"`, "sh", path).Output()
	if err != nil {
		t.Fatalf("source %s: %v", path, err)
	}

	return string(out)
}

// AC37: file 0600 in a 0700 dir, named <name>.env.sh.
func TestWriteEnvScriptModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "envs")
	path, err := runner.WriteEnvScript(dir, "api", map[string]string{"A": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "api"+scriptSuffix) {
		t.Fatalf("path = %q", path)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", fi.Mode().Perm())
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %v, want 0700", di.Mode().Perm())
	}
}

// AC37: values with quotes, spaces, $, backticks and newlines round-trip through sh.
func TestWriteEnvScriptRoundTrip(t *testing.T) {
	values := []string{
		"plain", "with space", "it's", `say "hi"`, "$HOME and ${X}", "`id`", "line1\nline2", "", `back\slash`, "$(id)",
	}
	for _, v := range values {
		path, err := runner.WriteEnvScript(t.TempDir(), "p", map[string]string{"KEY": v})
		if err != nil {
			t.Fatal(err)
		}
		if got := source(t, path, "KEY"); got != v {
			t.Errorf("round-trip %q = %q", v, got)
		}
	}
}

// Rewriting replaces the file and leaves no temp files behind.
func TestWriteEnvScriptRewrite(t *testing.T) {
	dir := t.TempDir()
	if _, err := runner.WriteEnvScript(dir, "p", map[string]string{"A": "old"}); err != nil {
		t.Fatal(err)
	}
	path, err := runner.WriteEnvScript(dir, "p", map[string]string{"A": "new"})
	if err != nil {
		t.Fatal(err)
	}
	if got := source(t, path, "A"); got != "new" {
		t.Errorf("A = %q, want new", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want 1", len(entries))
	}
}

// R49: invalid keys are rejected; the error names the key and never the value.
func TestWriteEnvScriptInvalidKey(t *testing.T) {
	for _, key := range []string{"1BAD", "A-B", "A B", "A=B", ""} {
		_, err := runner.WriteEnvScript(t.TempDir(), "p", map[string]string{key: secretValue})
		if err == nil {
			t.Errorf("key %q: want error", key)

			continue
		}
		if strings.Contains(err.Error(), secretValue) {
			t.Errorf("key %q: error leaks value: %v", key, err)
		}
		if key != "" && !strings.Contains(err.Error(), key) {
			t.Errorf("key %q: error %q must name the key", key, err)
		}
	}
}
