// Package config loads, validates and describes drover.yaml stack files.
package config

import (
	"cmp"
	"maps"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// YAML key and value names shared by validation, resolution and schema generation.
const (
	kindNone        = "none"
	kindDelay       = "delay"
	keyRestart      = "restart"
	keyTimeout      = "timeout"
	keyVersion      = "version"
	policyOnFailure = "on-failure"
	dirColumns      = "columns"
	dirRows         = "rows"
	jsObject        = "object"
	jsType          = "type"
	jsProperties    = "properties"
	jsOneOf         = "oneOf"
	jsAdditional    = "additionalProperties"
	jsInteger       = "integer"
	jsEnum          = "enum"
	jsString        = "string"
)

// SupportedVersion is the only config schema version this build reads.
const SupportedVersion = 1

// Config is a fully resolved stack: templates applied, defaults filled, paths absolute.
type Config struct {
	Version     int                `yaml:"version"     desc:"Config schema version; must be 1."`
	Name        string             `yaml:"name"        desc:"Stack name; used for the logs directory."`
	Description string             `yaml:"description" desc:"Free-text stack description."`
	Defaults    Process            `yaml:"defaults"    desc:"Process keys applied when a process leaves them unset."`
	Templates   map[string]Process `yaml:"templates"   desc:"Named process templates, referenced with use."`
	Processes   map[string]Process `yaml:"processes"   desc:"Processes to supervise, keyed by name."`
	Groups      map[string]Group   `yaml:"groups"      desc:"Named process selections shown as panes."`
	Presets     map[string]Preset  `yaml:"presets"     desc:"Named tag filters; the builtin all matches every process."`
	Layout      Layout             `yaml:"layout"      desc:"Pane layout: a named layout or an explicit split tree."`
	LogDir      string             `yaml:"logdir"      desc:"Logs directory; supports ~ and ${VAR}."`
	Source      string             `yaml:"-"`
	Order       []string           `yaml:"-"` // process names in declaration order.
}

// Process describes one supervised command.
type Process struct {
	Use        string            `yaml:"use"        desc:"Template to start from; process keys win over template keys."`
	Cmd        string            `yaml:"cmd"        desc:"Shell command to run."`
	Cwd        string            `yaml:"cwd"        desc:"Working directory; supports ~ and ${VAR}, relative to the config file."`
	EnvFile    string            `yaml:"env_file"   desc:"Dotenv file loaded into the process environment."`
	Env        map[string]string `yaml:"env"        desc:"Extra environment variables; merged by key over the template."`
	Tags       []string          `yaml:"tags"       desc:"Tags used by groups and presets."`
	DependsOn  []string          `yaml:"depends_on" desc:"Processes that must be ready first."`
	Ready      Ready             `yaml:"ready"      desc:"Readiness check: one of http, tcp, log, delay or none, plus timeout."`
	Port       Port              `yaml:"port"       desc:"Port number, or a key read from an env file."`
	Restart    string            `yaml:"restart"    desc:"never, on-failure, or {on-failure: {max: N}}."`
	RestartMax int               `yaml:"-"` // 0 means unlimited restarts.
	Stop       string            `yaml:"stop"       desc:"Shell command used to stop the process."`
}

// Ready is a readiness check. Kind is http, tcp, log, delay or none.
type Ready struct {
	Kind    string
	Value   string
	Timeout time.Duration
}

// Port is a literal port or one resolved from an env file key at Load.
type Port struct {
	Value       int
	FromEnvFile string
	Key         string
}

// Group selects processes by tags or by name.
type Group struct {
	Label     string   `yaml:"label"     desc:"Display label."`
	TagsAll   []string `yaml:"tags_all"  desc:"Select processes having all of these tags."`
	TagsAny   []string `yaml:"tags_any"  desc:"Select processes having any of these tags."`
	Processes []string `yaml:"processes" desc:"Select these processes explicitly."`
	Layout    Layout   `yaml:"layout"    desc:"Layout for the group."`
}

// Preset is a named tag filter.
type Preset struct {
	TagsAll []string `yaml:"tags_all" desc:"Match processes having all of these tags."`
	TagsAny []string `yaml:"tags_any" desc:"Match processes having any of these tags."`
}

// Layout is a named layout or an explicit split tree.
type Layout struct {
	Name string
	Tree *Split
}

// Split is a node of an explicit layout tree: a split with children, or a leaf naming a group.
type Split struct {
	Dir      string  `yaml:"split"`
	Children []Split `yaml:"children"`
	Group    string  `yaml:"group"`
}

// UnmarshalYAML reads a ready block; Load has already validated its shape.
func (r *Ready) UnmarshalYAML(n *yaml.Node) error {
	var m map[string]string
	if err := n.Decode(&m); err != nil {
		return err
	}
	r.Kind = kindNone
	for _, k := range readyKinds() {
		if v, ok := m[k]; ok && k != kindNone {
			r.Kind, r.Value = k, v
		}
	}
	if t, ok := m[keyTimeout]; ok {
		d, err := time.ParseDuration(t)
		if err != nil {
			return err
		}
		r.Timeout = d
	}

	return nil
}

// UnmarshalYAML reads an integer or a {from_env_file, key} mapping.
func (p *Port) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		v, err := strconv.Atoi(n.Value)
		p.Value = v

		return err
	}
	var m struct {
		FromEnvFile string `yaml:"from_env_file"`
		Key         string `yaml:"key"`
	}
	if err := n.Decode(&m); err != nil {
		return err
	}
	p.FromEnvFile, p.Key = m.FromEnvFile, m.Key

	return nil
}

// UnmarshalYAML reads a named layout or a split tree.
func (l *Layout) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		l.Name = n.Value

		return nil
	}
	l.Tree = &Split{}

	return n.Decode(l.Tree)
}

// UnmarshalYAML reads a process, folding the `restart: {on-failure: {max: N}}` form into Restart and RestartMax.
func (p *Process) UnmarshalYAML(n *yaml.Node) error {
	type plain Process
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value != keyRestart || n.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		var r struct {
			OnFailure struct {
				Max int `yaml:"max"`
			} `yaml:"on-failure"`
		}
		if err := n.Content[i+1].Decode(&r); err != nil {
			return err
		}
		p.RestartMax = r.OnFailure.Max
		n.Content[i+1] = &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: policyOnFailure,
			Line:  n.Content[i+1].Line,
		}
	}

	return n.Decode((*plain)(p))
}

func readyKinds() []string { return []string{"http", "tcp", "log", kindDelay, kindNone} }

func layoutNames() []string { return []string{"default", dirColumns, dirRows, "main-vertical"} }

// merge returns over with every unset key taken from base; env maps merge by key.
func merge(base, over Process) Process {
	out := over
	out.Cmd = cmp.Or(over.Cmd, base.Cmd)
	out.Cwd = cmp.Or(over.Cwd, base.Cwd)
	out.EnvFile = cmp.Or(over.EnvFile, base.EnvFile)
	out.Stop = cmp.Or(over.Stop, base.Stop)
	if over.Restart == "" {
		out.Restart, out.RestartMax = base.Restart, base.RestartMax
	}
	if over.Tags == nil {
		out.Tags = base.Tags
	}
	if over.DependsOn == nil {
		out.DependsOn = base.DependsOn
	}
	if over.Ready.Kind == "" {
		out.Ready = base.Ready
	}
	if over.Port == (Port{}) {
		out.Port = base.Port
	}
	if len(base.Env)+len(over.Env) > 0 {
		out.Env = make(map[string]string, len(base.Env)+len(over.Env))
		maps.Copy(out.Env, base.Env)
		maps.Copy(out.Env, over.Env)
	}

	return out
}
