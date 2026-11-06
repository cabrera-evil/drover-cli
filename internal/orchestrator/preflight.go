package orchestrator

import (
	"errors"
	"fmt"
	"os"

	"github.com/cabrera-evil/drover-cli/internal/config"
	"github.com/cabrera-evil/drover-cli/internal/errs"
)

// PortChecker reports whether a TCP port is already bound.
type PortChecker interface{ InUse(port int) bool }

// Preflight checks, with no side effects, that every selected process can start: cwd exists, env_file and
// port.from_env_file exist, ports are free and dependencies are acyclic and known. All problems are reported
// together as errors.Join of *errs.Error (exit 4). Env values are never included. Returns nil when clean.
func Preflight(cfg *config.Config, names []string, ports PortChecker) error {
	var problems []error
	for _, n := range names {
		p := cfg.Processes[n]
		if !isDir(p.Cwd) {
			problems = append(problems, preflightErr(errs.CodeCwdMissing, n,
				fmt.Sprintf("cwd %q is not an existing directory", p.Cwd), "create the directory or fix cwd"))
		}
		for _, f := range []string{p.EnvFile, p.Port.FromEnvFile} {
			if f != "" && !isFile(f) {
				problems = append(problems, preflightErr(errs.CodeEnvFileMissing, n,
					fmt.Sprintf("env file %q does not exist", f), "create the file or fix the path"))
			}
		}
		if p.Port.Value != 0 && ports.InUse(p.Port.Value) {
			problems = append(problems, preflightErr(errs.CodePortInUse, n,
				fmt.Sprintf("port %d is already in use", p.Port.Value), "stop the process using it or change the port"))
		}
	}
	if err := unknownDeps(cfg, names); err != nil {
		problems = append(problems, err)
	}
	if err := findCycle(cfg, names); err != nil {
		problems = append(problems, err)
	}

	return errors.Join(problems...)
}

func isDir(p string) bool {
	st, err := os.Stat(p)

	return err == nil && st.IsDir()
}

func isFile(p string) bool {
	st, err := os.Stat(p)

	return err == nil && !st.IsDir()
}
