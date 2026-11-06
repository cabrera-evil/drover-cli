# Expected production API for slice S2 orchestrator (derived from the tests)

## package `errs` additions
```go
const ExitPreflight = 4
const (CodePortInUse, CodeCwdMissing, CodeEnvFileMissing, CodeDepCycle, CodeDepUnknown string)
// Slug codes ^DRV(-[A-Z]+)+$, distinct, never reused:
//   CodePortInUse="DRV-PORT-IN-USE" CodeCwdMissing="DRV-CWD-MISSING" CodeEnvFileMissing="DRV-ENVFILE-MISSING"
//   CodeDepCycle="DRV-DEP-CYCLE" CodeDepUnknown="DRV-DEP-UNKNOWN"
```

## package `config` additions
`Config.Order []string` (`yaml:"-"`): process names in YAML declaration order, filled by `Load`.
Orchestrator and selection use it for every deterministic ordering.

## package `orchestrator` (internal/orchestrator)
```go
// Levels returns start waves for the selected names: a process is in the wave after all of its
// (selected) dependencies; within a wave, names follow cfg.Order. Dependencies outside `names` are ignored.
// Errors are *errs.Error with Exit=ExitPreflight: CodeDepCycle (message lists the path, "a -> b -> a")
// and CodeDepUnknown (names process and missing dependency; checked against cfg.Processes).
func Levels(cfg *config.Config, names []string) ([][]string, error)

type PortChecker interface{ InUse(port int) bool }

// Preflight checks, without any side effects: cwd exists (dir), env_file and port.from_env_file exist,
// non-zero ports are free per ports, dependency cycles/unknowns. It reports ALL problems in one pass as
// errors.Join of *errs.Error (each Exit=ExitPreflight, Key "processes.<name>", Message, Fix, Code);
// errs.ExitCode(joined) == 4. Returns nil when clean. Env values are never included in messages.
func Preflight(cfg *config.Config, names []string, ports PortChecker) error
```
