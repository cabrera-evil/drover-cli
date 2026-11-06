package runner

import (
	"strconv"
	"strings"
)

// PaneSpec describes the supervisor invocation for one pane. It carries no env values.
type PaneSpec struct {
	Name, LogPath, EnvScript, Restart, Cmd string
	Max                                    int
}

// PaneCommand returns the shell line a pane runs: `<selfBin> internal-run ... -- <cmd>`, every argument
// shell-quoted and the layout deterministic.
func PaneCommand(selfBin string, spec PaneSpec) string {
	args := []string{
		selfBin, "internal-run",
		"--name", spec.Name,
		"--log", spec.LogPath,
		"--env-script", spec.EnvScript,
		"--restart", spec.Restart,
		"--max", strconv.Itoa(spec.Max),
		"--", spec.Cmd,
	}
	for i, a := range args {
		args[i] = shellQuote(a)
	}

	return strings.Join(args, " ")
}
