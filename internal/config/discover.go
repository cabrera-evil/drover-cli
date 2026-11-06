package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Source is a discovered stack file and the stack name it declares ("" when unreadable).
type Source struct {
	Path string
	Name string
}

// Sources lists stack files in precedence order: the explicit flag path, drover.yaml in cwd or its nearest
// parent, then ~/.config/drover/stacks/*.yaml sorted by file name.
func Sources(cwd, flag string) ([]Source, error) {
	var paths []string
	if flag != "" {
		abs, err := filepath.Abs(flag)
		if err != nil {
			return nil, notFound(flag, "invalid path")
		}
		if _, err = os.Stat(abs); err != nil {
			return nil, notFound(abs, "file not found")
		}
		paths = append(paths, abs)
	}
	if p := projectFile(cwd); p != "" {
		paths = append(paths, p)
	}
	paths = append(paths, userStacks()...)

	seen := map[string]bool{}
	out := make([]Source, 0, len(paths))
	for _, p := range paths {
		if !seen[p] {
			seen[p] = true
			out = append(out, Source{Path: p, Name: peekName(p)})
		}
	}

	return out, nil
}

// projectFile returns the drover.yaml in dir or its nearest parent, or "".
func projectFile(dir string) string {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if p := filepath.Join(dir, "drover.yaml"); isFile(p) {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// userStacks returns ~/.config/drover/stacks/*.yaml sorted by file name.
func userStacks() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(home, ".config", "drover", "stacks")
	entries, _ := os.ReadDir(dir) // a missing directory simply means no user stacks
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}

	return out
}

func isFile(p string) bool {
	fi, err := os.Stat(p)

	return err == nil && !fi.IsDir()
}

func peekName(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var v struct {
		Name string `yaml:"name"`
	}
	_ = yaml.Unmarshal(data, &v) // an invalid file lists with an empty name; Load reports the error

	return v.Name
}
