package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cabrera-evil/drover-cli/internal/errs"
)

// Load reads, validates and resolves the stack file at path. Errors are *errs.Error with exit code 3;
// only the first problem is reported.
func Load(path string) (*Config, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, notFound(path, "invalid path")
	}
	root, err := parseFile(abs)
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, notFound(abs, "cannot determine the home directory")
	}
	w := &walker{file: abs, dir: filepath.Dir(abs), home: home}
	if err = checkVersion(w, root); err != nil {
		return nil, err
	}
	if err = w.top(root); err != nil {
		return nil, err
	}
	var cfg Config
	if err = root.Decode(&cfg); err != nil {
		return nil, decodeErr(abs, err)
	}
	cfg.Source = abs
	cfg.Order = declared(lookup(root, "processes"))
	if err = w.resolve(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// declared returns the keys of a mapping node in declaration order.
func declared(n *yaml.Node) []string {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	out := make([]string, 0, len(n.Content))
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, n.Content[i].Value)
	}

	return out
}

// ReadVersion returns the version declared by the file at path without validating anything else.
func ReadVersion(path string) (int, error) {
	root, err := parseFile(path)
	if err != nil {
		return 0, err
	}
	v := lookup(root, keyVersion)
	if v == nil {
		return 0, &errs.Error{
			Code: errs.CodeUnsupportedVersion, Message: "missing required key version", File: path, Key: keyVersion,
			Fix: "add `version: 1`", Exit: errs.ExitConfig,
		}
	}
	n, err := strconv.Atoi(v.Value)
	if err != nil {
		return 0, &errs.Error{
			Code: errs.CodeInvalidValue, Message: "version must be an integer", File: path, Line: v.Line,
			Key: keyVersion, Exit: errs.ExitConfig,
		}
	}

	return n, nil
}

func notFound(file, msg string) error {
	return &errs.Error{
		Code: errs.CodeNotFound, Message: msg, File: file, Exit: errs.ExitConfig,
		Fix: "check the path, or run from a directory with a drover.yaml, or pass --config",
	}
}

// parseFile reads path and returns the root mapping node.
func parseFile(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, notFound(path, "file not found")
		}

		return nil, notFound(path, "cannot read file")
	}
	var doc yaml.Node
	if err = yaml.Unmarshal(data, &doc); err != nil {
		return nil, decodeErr(path, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, &errs.Error{
			Code: errs.CodeInvalidValue, Message: "top level must be a mapping with version: 1", File: path, Line: 1,
			Fix: "start the file with `version: 1`", Exit: errs.ExitConfig,
		}
	}

	return doc.Content[0], nil
}

func checkVersion(w *walker, root *yaml.Node) error {
	v := lookup(root, keyVersion)
	if v == nil {
		return w.fail(errs.CodeUnsupportedVersion, root, keyVersion, "missing required key version",
			"add `version: 1` at the top of the file")
	}
	n, err := strconv.Atoi(v.Value)
	if err != nil {
		return w.bad(v, keyVersion, "version must be an integer", "use `version: 1`")
	}
	if n != SupportedVersion {
		return w.fail(errs.CodeUnsupportedVersion, v, keyVersion,
			fmt.Sprintf("unsupported version %d (supported: %d); see `drover config migrate`", n, SupportedVersion),
			"run `drover config migrate`")
	}

	return nil
}

// decodeErr converts a yaml error into an *errs.Error, dropping any quoted values yaml may echo.
func decodeErr(file string, err error) error {
	msg := strings.TrimPrefix(err.Error(), "yaml: ")
	var te *yaml.TypeError
	if errors.As(err, &te) && len(te.Errors) > 0 {
		msg = te.Errors[0]
	}
	line := 0
	if m := regexp.MustCompile(`^(?:unmarshal errors:\s*)?line (\d+): `).FindStringSubmatch(msg); m != nil {
		line, _ = strconv.Atoi(m[1])
		msg = strings.TrimPrefix(msg, m[0])
	}
	msg = regexp.MustCompile("`[^`]*`").ReplaceAllString(msg, "value")

	return &errs.Error{
		Code: errs.CodeInvalidValue, Message: msg, File: file, Line: line, Exit: errs.ExitConfig,
		Fix: "compare the file with schema/drover.schema.json",
	}
}

// expandPath expands ~ and ${VAR}, then makes the result absolute relative to base.
func expandPath(p, base, home string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = home + p[1:]
	}
	p = os.ExpandEnv(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}

	return filepath.Clean(p)
}

// resolve applies templates and defaults, makes paths absolute and resolves ports, logdir and layout.
func (w *walker) resolve(cfg *Config) error {
	procs := make(map[string]Process, len(cfg.Processes))
	for _, name := range slices.Sorted(maps.Keys(cfg.Processes)) {
		p := cfg.Processes[name]
		if p.Use != "" {
			p = merge(cfg.Templates[p.Use], p)
		}
		p = merge(cfg.Defaults, p)
		if p.Cmd == "" {
			return &errs.Error{
				Code: errs.CodeInvalidValue, Message: "cmd is required", File: w.file, Line: w.lines[name],
				Key: "processes." + name + ".cmd", Fix: "set cmd, or use a template that defines it",
				Exit: errs.ExitConfig,
			}
		}
		if p.Ready.Kind == "" {
			p.Ready.Kind = kindNone
		}
		p.Cwd = expandPath(p.Cwd, w.dir, w.home)
		if p.EnvFile != "" {
			p.EnvFile = expandPath(p.EnvFile, w.dir, w.home)
		}
		if p.Port.FromEnvFile != "" {
			p.Port.FromEnvFile = expandPath(p.Port.FromEnvFile, w.dir, w.home)
			port, _ := portFromEnvFile(p.Port.FromEnvFile, p.Port.Key) // validated in the node walk
			p.Port.Value = port
		}
		procs[name] = p
	}
	cfg.Processes = procs
	if cfg.Presets == nil {
		cfg.Presets = map[string]Preset{}
	}
	cfg.Presets["all"] = Preset{TagsAny: []string{"*"}}
	if cfg.Layout.Name == "" && cfg.Layout.Tree == nil {
		cfg.Layout.Name = "default"
	}
	if cfg.LogDir == "" {
		cfg.LogDir = filepath.Join(w.home, ".local", "state", "drover", "logs", cfg.Name)
	} else {
		cfg.LogDir = expandPath(cfg.LogDir, w.dir, w.home)
	}

	return nil
}
