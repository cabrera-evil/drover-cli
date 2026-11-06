package config

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/cabrera-evil/drover-cli/internal/errs"
)

const maxPort = 65535

// walker validates the YAML node tree and reports the first problem in document order,
// with file, line and key path. It never echoes env or env_file values.
type walker struct {
	file, dir, home string
	templates       map[string]bool
	procs           map[string]bool
	groups          map[string]bool
	lines           map[string]int // process name -> line of its key
}

func (w *walker) fail(code string, n *yaml.Node, key, msg, fix string) error {
	return &errs.Error{
		Code: code, Message: msg, File: w.file, Line: n.Line, Key: key, Fix: fix, Exit: errs.ExitConfig,
	}
}

func (w *walker) bad(n *yaml.Node, key, msg, fix string) error {
	return w.fail(errs.CodeInvalidValue, n, key, msg, fix)
}

func join(path, key string) string {
	if path == "" {
		return key
	}

	return path + "." + key
}

// lookup returns the value node for key in a mapping node, or nil.
func lookup(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}

	return nil
}

func names(n *yaml.Node) map[string]bool {
	out := map[string]bool{}
	if n != nil && n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			out[n.Content[i].Value] = true
		}
	}

	return out
}

// validName allows letters, digits, dot, underscore and dash, starting with a letter or digit.
func validName(s string) bool {
	for i, r := range s {
		alnum := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if !alnum && (i == 0 || r != '.' && r != '_' && r != '-') {
			return false
		}
	}

	return s != ""
}

// keys checks n is a mapping whose keys are all in allowed.
func (w *walker) keys(n *yaml.Node, path string, allowed ...string) error {
	if n.Kind != yaml.MappingNode {
		return w.bad(n, path, "must be a mapping", "see schema/drover.schema.json")
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i]
		if !slices.Contains(allowed, k.Value) {
			return w.fail(errs.CodeUnknownKey, k, join(path, k.Value), "unknown key",
				"remove it or fix the spelling; allowed keys: "+strings.Join(allowed, ", "))
		}
	}

	return nil
}

// named walks a mapping of user-chosen names to values.
func (w *walker) named(n *yaml.Node, path string, fn func(name, val *yaml.Node, path string) error) error {
	if n.Kind != yaml.MappingNode {
		return w.bad(n, path, "must be a mapping", "see schema/drover.schema.json")
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		p := join(path, k.Value)
		if !validName(k.Value) {
			return w.bad(k, p, "invalid name", "use letters, digits, '.', '_' or '-', starting with a letter or digit")
		}
		if err := fn(k, v, p); err != nil {
			return err
		}
	}

	return nil
}

func (w *walker) top(root *yaml.Node) error {
	w.templates = names(lookup(root, "templates"))
	w.procs = names(lookup(root, "processes"))
	w.groups = names(lookup(root, "groups"))
	w.lines = map[string]int{}
	err := w.keys(root, "", keyVersion, "name", "description", "logdir", "defaults", "templates", "processes",
		"groups", "presets", "layout")
	if err != nil {
		return err
	}
	if nm := lookup(root, "name"); nm == nil || !validName(nm.Value) {
		return w.bad(cmpNode(nm, root), "name", "name is required: letters, digits, '.', '_' or '-'",
			"set `name: my-stack`")
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if err = w.section(root.Content[i].Value, root.Content[i+1]); err != nil {
			return err
		}
	}

	return nil
}

func cmpNode(n, fallback *yaml.Node) *yaml.Node {
	if n != nil {
		return n
	}

	return fallback
}

func (w *walker) section(key string, v *yaml.Node) error {
	switch key {
	case "defaults":
		return w.process(v, key, false)
	case "templates":
		return w.named(v, key, func(_, val *yaml.Node, p string) error { return w.process(val, p, false) })
	case "processes":
		return w.named(v, key, func(k, val *yaml.Node, p string) error {
			w.lines[k.Value] = k.Line

			return w.process(val, p, true)
		})
	case "groups":
		return w.named(v, key, func(_, val *yaml.Node, p string) error { return w.group(val, p) })
	case "presets":
		return w.named(v, key, func(k, val *yaml.Node, p string) error {
			if k.Value == "all" {
				return w.bad(k, p, "all is a builtin preset and cannot be redefined", "pick another name")
			}

			return w.keys(val, p, "tags_all", "tags_any")
		})
	case "layout":
		return w.layout(v, key)
	}

	return nil
}

func (w *walker) process(n *yaml.Node, path string, allowUse bool) error {
	allowed := []string{"cwd", "cmd", "env", "env_file", "tags", "depends_on", "ready", "port", keyRestart, "stop"}
	if allowUse {
		allowed = append(allowed, "use")
	}
	if err := w.keys(n, path, allowed...); err != nil {
		return err
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		p := join(path, k.Value)
		var err error
		switch k.Value {
		case "use":
			if !w.templates[v.Value] {
				err = w.bad(v, p, fmt.Sprintf("unknown template %q", v.Value), "define it under templates")
			}
		case "depends_on":
			err = w.dependsOn(v, p)
		case "ready":
			err = w.ready(v, p)
		case "port":
			err = w.port(v, p)
		case keyRestart:
			err = w.restart(v, p)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func (w *walker) dependsOn(n *yaml.Node, path string) error {
	if n.Kind != yaml.SequenceNode {
		return w.bad(n, path, "must be a list of process names", "e.g. depends_on: [db]")
	}
	for _, item := range n.Content {
		if !w.procs[item.Value] {
			return w.bad(item, path, fmt.Sprintf("unknown process %q", item.Value),
				"list names defined under processes")
		}
	}

	return nil
}

func (w *walker) ready(n *yaml.Node, path string) error {
	if err := w.keys(n, path, "http", "tcp", "log", kindDelay, kindNone, keyTimeout); err != nil {
		return err
	}
	checkers := 0
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Value != keyTimeout {
			checkers++
		}
		if k.Value == keyTimeout || k.Value == kindDelay {
			if _, err := time.ParseDuration(v.Value); err != nil {
				return w.bad(v, join(path, k.Value), "invalid duration", "use a Go duration such as 500ms, 5s or 2m")
			}
		}
		if k.Value == kindNone && v.Value != "true" {
			return w.bad(v, join(path, k.Value), "none must be true", "use `none: true` or drop the key")
		}
	}
	if checkers > 1 {
		return w.bad(n, path, "only one ready checker is allowed", "keep one of http, tcp, log, delay or none")
	}

	return nil
}

func (w *walker) port(n *yaml.Node, path string) error {
	const fix = "use a number or {from_env_file: ./.env, key: PORT}"
	if n.Kind == yaml.ScalarNode {
		if v, err := strconv.Atoi(n.Value); err != nil || v < 1 || v > maxPort {
			return w.bad(n, path, "port must be an integer between 1 and 65535", fix)
		}

		return nil
	}
	if err := w.keys(n, path, "from_env_file", "key"); err != nil {
		return err
	}
	file, key := lookup(n, "from_env_file"), lookup(n, "key")
	if file == nil || key == nil {
		return w.bad(n, path, "port needs both from_env_file and key", fix)
	}
	if _, why := portFromEnvFile(expandPath(file.Value, w.dir, w.home), key.Value); why != "" {
		return w.bad(n, path, why, "add the key to the env file or fix from_env_file/key")
	}

	return nil
}

func (w *walker) restart(n *yaml.Node, path string) error {
	const fix = "use never, on-failure or {on-failure: {max: N}}"
	if n.Kind == yaml.ScalarNode {
		if n.Value != "never" && n.Value != policyOnFailure {
			return w.bad(n, path, "restart must be never or on-failure", fix)
		}

		return nil
	}
	if err := w.keys(n, path, policyOnFailure); err != nil {
		return err
	}
	of := lookup(n, policyOnFailure)
	if of == nil {
		return w.bad(n, path, "restart mapping needs on-failure", fix)
	}
	if err := w.keys(of, join(path, policyOnFailure), "max"); err != nil {
		return err
	}
	if m := lookup(of, "max"); m != nil {
		if v, err := strconv.Atoi(m.Value); err != nil || v < 0 {
			return w.bad(m, join(path, "on-failure.max"), "max must be a non-negative integer", fix)
		}
	}

	return nil
}

func (w *walker) group(n *yaml.Node, path string) error {
	if err := w.keys(n, path, "label", "tags_all", "tags_any", "processes", "layout"); err != nil {
		return err
	}
	if ps := lookup(n, "processes"); ps != nil {
		if ps.Kind != yaml.SequenceNode {
			return w.bad(ps, join(path, "processes"), "must be a list of process names", "e.g. processes: [web]")
		}
		for _, item := range ps.Content {
			if !w.procs[item.Value] {
				return w.bad(item, join(path, "processes"), fmt.Sprintf("unknown process %q", item.Value),
					"list names defined under processes")
			}
		}
	}
	if l := lookup(n, "layout"); l != nil {
		return w.layout(l, join(path, "layout"))
	}

	return nil
}

func (w *walker) layout(n *yaml.Node, path string) error {
	if n.Kind == yaml.ScalarNode {
		if !slices.Contains(layoutNames(), n.Value) {
			return w.bad(n, path, fmt.Sprintf("unknown layout %q", n.Value),
				"use one of "+strings.Join(layoutNames(), ", ")+" or a split tree")
		}

		return nil
	}

	return w.split(n, path)
}

func (w *walker) split(n *yaml.Node, path string) error {
	if err := w.keys(n, path, "split", "children", "group"); err != nil {
		return err
	}
	dir, children, group := lookup(n, "split"), lookup(n, "children"), lookup(n, "group")
	if group != nil {
		if dir != nil || children != nil {
			return w.bad(n, path, "a leaf has only group; a split has only split and children",
				"remove group, or remove split and children")
		}
		if !w.groups[group.Value] {
			return w.bad(group, join(path, "group"), fmt.Sprintf("unknown group %q", group.Value),
				"define it under groups")
		}

		return nil
	}
	if dir == nil || dir.Value != dirColumns && dir.Value != dirRows {
		return w.bad(cmpNode(dir, n), join(path, "split"), "split must be columns or rows", "set split: columns")
	}
	if children == nil || children.Kind != yaml.SequenceNode || len(children.Content) == 0 {
		return w.bad(cmpNode(children, n), join(path, "children"), "a split needs at least one child",
			"add children: [{group: name}]")
	}
	for i, c := range children.Content {
		if err := w.split(c, fmt.Sprintf("%s.children[%d]", path, i)); err != nil {
			return err
		}
	}

	return nil
}

// portFromEnvFile reads key from a dotenv file. The reason string never contains a value.
func portFromEnvFile(file, key string) (int, string) {
	f, err := os.Open(file)
	if err != nil {
		return 0, "cannot read env file " + file
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimPrefix(strings.TrimSpace(sc.Text()), "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		port, perr := strconv.Atoi(strings.Trim(strings.TrimSpace(v), `"'`))
		if perr != nil || port < 1 || port > maxPort {
			return 0, fmt.Sprintf("key %s in env file %s is not a valid port", key, file)
		}

		return port, ""
	}

	return 0, fmt.Sprintf("key %s not found in env file %s", key, file)
}
