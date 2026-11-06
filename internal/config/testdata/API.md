# Expected production API for slice S1 (derived from the tests)

Module `github.com/cabrera-evil/drover-cli`. Replace the legacy env-only `internal/config/config.go`
(`Load() Config`) and update `internal/app/root.go` / `app_test.go` accordingly (greeting goes away).
Add `gopkg.in/yaml.v3` to go.mod. YAML parsing must reject unknown keys and report line numbers.

## package `errs` (internal/errs)

```go
const ExitConfig = 3
const (CodeUnknownKey, CodeUnsupportedVersion, CodeInvalidValue, CodeNotFound string) // slug ^DRV(-[A-Z]+)+$, distinct:
//   CodeUnknownKey="DRV-CONFIG-UNKNOWN-KEY" CodeUnsupportedVersion="DRV-CONFIG-UNSUPPORTED-VERSION"
//   CodeInvalidValue="DRV-CONFIG-INVALID-VALUE" CodeNotFound="DRV-CONFIG-NOT-FOUND"; CodeUsage="DRV-USAGE"

type Error struct {
    Code, Message, File string
    Line int
    Key, Fix string
    Exit int            // 0 => ExitCode() 1
}
func (e *Error) Error() string  // location = File[+":"+Line] [+" "+Key]; if location != "" => "<location>: <Message> (<Code>)" else "<Message> (<Code>)"
                                //   "drover.yaml:12 processes.api.ready: msg (DRV-CONFIG-INVALID-VALUE)"
                                //   "/x/drover.yaml version: msg (DRV-CONFIG-UNSUPPORTED-VERSION)"   (Line 0 => no ":line")
                                //   "msg (DRV-SOMETHING-FAILED)"
func (e *Error) ExitCode() int
func ExitCode(err error) int    // nil=>0; errors.As *Error => its Exit (or 1 if 0); else 1
```
See TestErrorRendering for the three exact goldens. Errors must never contain env / env_file values.

## package `config` (internal/config)

```go
type Config struct {
    Version int; Name, Description string
    Defaults Process               // yaml `defaults`: process keys applied when a process leaves them unset
    Templates map[string]Process   // yaml `templates`
    Processes map[string]Process   // templates resolved (use), defaults applied, cwd absolute, port resolved
    Groups    map[string]Group
    Presets   map[string]Preset    // builtin "all" = Preset{TagsAny: ["*"]} always injected
    Layout    Layout               // default Name "default"
    LogDir    string               // yaml `logdir` (~/${VAR} expanded); default $HOME/.local/state/drover/logs/<name>
    Source    string               // absolute path of the loaded file
}
type Process struct {
    Use, Cmd, Cwd, EnvFile, Restart, Stop string
    Env map[string]string; Tags, DependsOn []string
    Ready Ready; Port Port
}
type Ready struct{ Kind, Value string; Timeout time.Duration } // Kind http|tcp|log|delay|none (default none); Value = url/addr/regex/duration string
type Port struct{ Value int; FromEnvFile, Key string }          // yaml: int | {from_env_file, key}; Value resolved at Load
type Group struct{ Label string; TagsAll, TagsAny, Processes []string; Layout Layout }
type Preset struct{ TagsAll, TagsAny []string }
type Layout struct{ Name string; Tree *Split } // yaml: scalar default|columns|rows|main-vertical, or split tree map => Tree
type Split struct{ Dir string; Children []Split; Group string } // yaml {split: columns|rows, children: [...]} / leaf {group: name}

func Load(path string) (*Config, error)           // errors are *errs.Error with Exit=ExitConfig; first error only
type Source struct{ Path, Name string }
func Sources(cwd, flag string) ([]Source, error)  // order: flag (if set), drover.yaml in cwd or nearest parent, ~/.config/drover/stacks/*.yaml sorted; HOME via env
func Schema() ([]byte, error)                     // deterministic JSON Schema from the structs; top-level properties version,name,processes,groups,presets,layout
```
Process YAML keys: cwd, cmd, env, env_file, tags, depends_on, ready (http|tcp|log|delay|none + timeout), port, restart, stop, use.
Validation errors (Key path, e.g. `processes.api.ready`, `processes.api.use`, `layout...`, `version`, unknown key full path).
Relative paths (cwd, env_file, from_env_file) resolve against the config file's directory. Missing port key error
names env file and key, never the value.
Golden: `schema/drover.schema.json` at repo root is written via `go test ./internal/config -run SchemaGolden -update`.
Examples validation (AC11 second half) is out of S1 (delivered in S6).

## package `app` (internal/app)

Root persistent flag `--config <path>`. Commands: `config check`, `config show`, `config migrate`, `stacks`.
Write output with cmd.OutOrStdout(); return errors (do not os.Exit); main maps via errs.ExitCode.
Config is discovered via config.Sources(cwd, flag)[0]; none found => *errs.Error (Exit=3, DRV code).
- check: prints a line containing "ok" on success.
- show: stack name, source path, process names, env KEY NAMES only (values and env_file contents never printed).
- migrate: v1 prints "already current", leaves file untouched, exit 0.
- stacks: one line per source with path and stack name; no sources => no error.
