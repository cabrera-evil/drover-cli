package runner_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cabrera-evil/drover-cli/internal/runner"
)

// echoArgs writes a fake selfBin (in a dir with a space) that prints one argument per line.
func echoArgs(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "my bin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "drover")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	return bin
}

func paneSpec() runner.PaneSpec {
	return runner.PaneSpec{
		Name:      "api server",
		LogPath:   "/tmp/logs dir/api.log",
		EnvScript: "/tmp/env dir/api.env.sh",
		Restart:   "on-failure",
		Max:       2,
		Cmd:       `echo "it's" $HOME; npm run dev -- --port 3000`,
	}
}

// AC37: every argument is shell-quoted; the flag layout is exact and carries no env values.
func TestPaneCommandArgs(t *testing.T) {
	bin := echoArgs(t)
	spec := paneSpec()
	line := runner.PaneCommand(bin, spec)

	out, err := exec.CommandContext(t.Context(), "sh", "-c", line).Output()
	if err != nil {
		t.Fatalf("run %q: %v", line, err)
	}
	want := []string{
		"internal-run", "--name", spec.Name, "--log", spec.LogPath, "--env-script", spec.EnvScript,
		"--restart", spec.Restart, "--max", "2", "--", spec.Cmd,
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if !slices.Equal(got, want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestPaneCommandStable(t *testing.T) {
	a := runner.PaneCommand("/usr/bin/drover", paneSpec())
	b := runner.PaneCommand("/usr/bin/drover", paneSpec())
	if a != b {
		t.Fatalf("not stable:\n%s\n%s", a, b)
	}
	if !strings.HasPrefix(a, "'/usr/bin/drover'") && !strings.HasPrefix(a, "/usr/bin/drover") {
		t.Errorf("unexpected prefix: %s", a)
	}
}
