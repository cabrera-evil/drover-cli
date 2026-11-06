# Expected production API for slice S4b `internal/runner` (derived from the tests)

```go
package runner

// BuildEnv merges, lowest to highest precedence: base defaults (FORCE_COLOR=1), the env_file
// (resolved against p.Cwd when relative), then p.Env (config defaults/template env are already
// merged into p.Env by internal/config). An entry FORCE_COLOR="" removes the variable.
// .env syntax: KEY=VALUE, optional `export `, `#` comment lines, blank lines, one pair of
// single/double quotes stripped, no expansion. Errors name the file (and line) and never a value.
func BuildEnv(p config.Process) (map[string]string, error)

// WriteEnvScript writes <dir>/<name>.env.sh (file 0600, dir created 0700) with
// `export KEY='value'` lines (POSIX single-quote escaping), atomically replacing any existing
// file, and returns its path. Keys must match [A-Za-z_][A-Za-z0-9_]*; the error names the key only.
func WriteEnvScript(dir, name string, env map[string]string) (string, error)

// PaneSpec describes the supervisor invocation for one pane. It carries no env values.
type PaneSpec struct {
	Name, LogPath, EnvScript, Restart, Cmd string
	Max                                    int
}

// PaneCommand returns `<selfBin> internal-run --name N --log PATH --env-script PATH
// --restart POLICY --max N -- <cmd>` with every argument shell-quoted; deterministic.
func PaneCommand(selfBin string, spec PaneSpec) string

// Options configure the in-pane supervisor.
type Options struct {
	Name      string
	Cmd       string // run via `sh -c`
	LogPath   string // truncated on start; ANSI-stripped copy of output; restart markers appended
	EnvScript string // optional; sourced before Cmd ("" = none)
	Restart   string // "never" or "on-failure"
	Max       int    // on-failure restarts; 0 = unlimited
	Stdout    io.Writer
	Sleep     func(ctx context.Context, d time.Duration) // backoff between restarts; nil = real sleep
}

// Run executes Cmd and applies the restart policy. Child stdout+stderr go unchanged to Stdout
// and ANSI-stripped to LogPath. Before restart N of Max it appends `--- restart N/Max ---\n`
// to the log (no truncation). Returns the child's last exit code; err is for supervisor failures
// (log unopenable, start failure), never contains env values. Cancelling ctx kills the child's
// whole process group (unix; tiny interface, unix impl tested on the current OS) and Run returns;
// ctx is checked before every restart.
func Run(ctx context.Context, o Options) (int, error)
```
