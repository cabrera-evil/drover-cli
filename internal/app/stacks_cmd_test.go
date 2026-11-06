//nolint:testpackage // exercises unexported wiring
package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AC4 (R3): two sources => `drover stacks` prints both paths and names.
func TestStacksListsEverySource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	proj := filepath.Join(dir, "drover.yaml")
	userStack := filepath.Join(home, ".config", "drover", "stacks", "wallet.yaml")
	for p, c := range map[string]string{
		proj:      "version: 1\nname: alpha\n",
		userStack: "version: 1\nname: beta\n",
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)

	out, _, err := run(t, "stacks")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{proj, "alpha", userStack, "beta"} {
		if !strings.Contains(out, want) {
			t.Errorf("stacks output missing %q:\n%s", want, out)
		}
	}
}

func TestStacksEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	if _, _, err := run(t, "stacks"); err != nil {
		t.Errorf("empty stacks should not fail: %v", err)
	}
}
