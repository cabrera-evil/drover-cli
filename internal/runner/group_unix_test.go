//go:build unix

package runner_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cabrera-evil/drover-cli/internal/runner"
)

const (
	pollEvery = 20 * time.Millisecond
	pollFor   = 5 * time.Second
)

func readPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(pollFor)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && strings.HasSuffix(string(b), "\n") {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(b)))
			if convErr != nil {
				t.Fatal(convErr)
			}

			return pid
		}
		time.Sleep(pollEvery)
	}
	t.Fatalf("pid file %s never appeared", path)

	return 0
}

// alive reports whether pid is a live (non-zombie) process.
func alive(ctx context.Context, pid int) bool {
	out, err := exec.CommandContext(ctx, "ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	s := strings.TrimSpace(string(out))

	return err == nil && s != "" && !strings.HasPrefix(s, "Z")
}

// R50: cancelling the context kills the child and its grandchildren (process group, unix).
func TestRunCancelKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	child, grand := filepath.Join(dir, "child"), filepath.Join(dir, "grand")
	cmd := "sleep 30 & echo $! > '" + grand + "'; echo $$ > '" + child + "'; sleep 31"
	h := newHarness(t, cmd)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = runner.Run(ctx, h.opts)
	}()
	childPID, grandPID := readPID(t, child), readPID(t, grand)
	cancel()
	select {
	case <-done:
	case <-time.After(pollFor):
		t.Fatal("Run did not return after cancel")
	}
	deadline := time.Now().Add(pollFor)
	for time.Now().Before(deadline) && (alive(t.Context(), childPID) || alive(t.Context(), grandPID)) {
		time.Sleep(pollEvery)
	}
	if alive(t.Context(), childPID) || alive(t.Context(), grandPID) {
		t.Fatalf("child %d or grandchild %d still alive", childPID, grandPID)
	}
}
