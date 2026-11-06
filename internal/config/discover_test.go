package config_test

import (
	"path/filepath"
	"testing"

	"github.com/cabrera-evil/drover-cli/internal/config"
)

// AC3 (R2): --config beats a parent drover.yaml; the parent file is found without the flag.
func TestSourcesFlagBeatsParent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	parent := write(t, root, "drover.yaml", "version: 1\nname: parent\n")
	explicit := write(t, t.TempDir(), "other.yaml", "version: 1\nname: other\n")
	deep := filepath.Join(root, "a", "b")
	write(t, deep, ".keep", "")

	got, err := config.Sources(deep, "")
	if err != nil || len(got) == 0 || got[0].Path != parent || got[0].Name != "parent" {
		t.Fatalf("no flag: %+v err=%v", got, err)
	}
	got, err = config.Sources(deep, explicit)
	if err != nil || len(got) == 0 || got[0].Path != explicit || got[0].Name != "other" {
		t.Fatalf("flag: %+v err=%v", got, err)
	}
}

// R2: cwd file wins over parent; user stacks dir follows project file.
func TestSourcesOrderAndStacksDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	write(t, root, "drover.yaml", "version: 1\nname: parent\n")
	child := filepath.Join(root, "child")
	cwdFile := write(t, child, "drover.yaml", "version: 1\nname: child\n")
	s1 := write(t, home, ".config/drover/stacks/a.yaml", "version: 1\nname: sa\n")
	s2 := write(t, home, ".config/drover/stacks/b.yaml", "version: 1\nname: sb\n")

	got, err := config.Sources(child, "")
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(got))
	for _, s := range got {
		paths = append(paths, s.Path)
	}
	want := []string{cwdFile, s1, s2}
	if len(paths) != len(want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("paths[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
}

func TestSourcesOnlyStacksDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s := write(t, home, ".config/drover/stacks/only.yaml", "version: 1\nname: only\n")
	got, err := config.Sources(t.TempDir(), "")
	if err != nil || len(got) != 1 || got[0].Path != s || got[0].Name != "only" {
		t.Fatalf("got %+v err=%v", got, err)
	}
}

func TestSourcesNone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, err := config.Sources(t.TempDir(), "")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v err=%v", got, err)
	}
}
