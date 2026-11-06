package runner

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cabrera-evil/drover-cli/internal/errs"
)

const (
	dirMode    = 0o700
	scriptMode = 0o600
)

// WriteEnvScript writes <dir>/<name>.env.sh (0600, dir 0700) with `export KEY='value'` lines and
// atomically replaces any existing file. Errors name the key, never a value.
func WriteEnvScript(dir, name string, env map[string]string) (string, error) {
	if name == "" || name != filepath.Base(name) {
		return "", &errs.Error{Code: errs.CodeRunFailed, Message: "invalid env script name"}
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		if !validKey(k) {
			return "", &errs.Error{
				Code: errs.CodeInvalidValue, Key: k, Message: "invalid environment variable name",
				Fix: "Use letters, digits and underscores, not starting with a digit.",
			}
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString("export " + k + "=" + shellQuote(env[k]) + "\n")
	}

	if err := os.MkdirAll(dir, dirMode); err != nil {
		return "", &errs.Error{Code: errs.CodeRunFailed, File: dir, Message: "cannot create env script dir"}
	}
	if err := os.Chmod(dir, dirMode); err != nil {
		return "", &errs.Error{Code: errs.CodeRunFailed, File: dir, Message: "cannot secure env script dir"}
	}
	path := filepath.Join(dir, name+".env.sh")
	if err := writeAtomic(dir, path, b.String()); err != nil {
		return "", &errs.Error{Code: errs.CodeRunFailed, File: path, Message: "cannot write env script"}
	}

	return path, nil
}

// writeAtomic writes content to a 0600 temp file in dir, then renames it over path.
func writeAtomic(dir, path, content string) error {
	f, err := os.CreateTemp(dir, ".env-*.tmp")
	if err != nil {
		return err //nolint:wrapcheck // only a path, never a value; the caller replaces it.
	}
	tmp := f.Name()
	_, werr := f.WriteString(content)
	cerr := f.Close()
	if err = firstErr(werr, cerr, os.Chmod(tmp, scriptMode)); err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}

	return err
}

func firstErr(errList ...error) error {
	for _, e := range errList {
		if e != nil {
			return e
		}
	}

	return nil
}

// shellQuote wraps s in single quotes for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
