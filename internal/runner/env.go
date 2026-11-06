// Package runner builds a pane's environment and supervises its command (restart policy, log tee).
package runner

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cabrera-evil/drover-cli/internal/config"
	"github.com/cabrera-evil/drover-cli/internal/errs"
)

const (
	keyForceColor = "FORCE_COLOR"
	exportPrefix  = "export "
)

// BuildEnv merges, lowest to highest precedence: FORCE_COLOR=1, the env_file (relative paths resolve
// against p.Cwd), then p.Env. A FORCE_COLOR that ends up empty is removed. Errors name the file and
// line, never a value.
func BuildEnv(p config.Process) (map[string]string, error) {
	env := map[string]string{keyForceColor: "1"}
	if p.EnvFile != "" {
		path := p.EnvFile
		if !filepath.IsAbs(path) {
			path = filepath.Join(p.Cwd, path)
		}
		fileEnv, err := parseEnvFile(path, p.EnvFile)
		if err != nil {
			return nil, err
		}
		for k, v := range fileEnv {
			env[k] = v
		}
	}
	for k, v := range p.Env {
		env[k] = v
	}
	if env[keyForceColor] == "" {
		delete(env, keyForceColor)
	}

	return env, nil
}

// parseEnvFile reads KEY=VALUE lines (optional `export `, `#` comments, one pair of quotes stripped,
// no expansion). display is the name shown in errors.
func parseEnvFile(path, display string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, &errs.Error{Code: errs.CodeEnvFileMissing, File: display, Message: "env_file not found"}
	}
	if err != nil {
		return nil, &errs.Error{Code: errs.CodeRunFailed, File: display, Message: "cannot read env_file"}
	}
	env := map[string]string{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimPrefix(line, exportPrefix), "=")
		k = strings.TrimSpace(k)
		if !ok || !validKey(k) {
			return nil, &errs.Error{
				Code: errs.CodeEnvFileInvalid, File: display, Line: i + 1,
				Message: "expected KEY=VALUE", Fix: "Use one KEY=VALUE per line.",
			}
		}
		env[k] = unquote(strings.TrimSpace(v))
	}

	return env, nil
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}

	return v
}

// validKey reports whether k matches [A-Za-z_][A-Za-z0-9_]*.
func validKey(k string) bool {
	if k == "" {
		return false
	}
	for i := range len(k) {
		c := k[i]
		letter := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !letter && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}

	return true
}
