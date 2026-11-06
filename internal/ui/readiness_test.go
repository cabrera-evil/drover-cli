package ui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cabrera-evil/drover-cli/internal/ui"
)

const (
	esc    = "\x1b"
	procDB = "db"
	procAP = "api"
)

func at(sec int) time.Time { return time.Unix(1_700_000_000, 0).Add(time.Duration(sec) * time.Second) }

func procs() []ui.Proc {
	return []ui.Proc{{Name: procDB, Timeout: 30 * time.Second}, {Name: procAP, Timeout: 10 * time.Second}}
}

func line(frame, name string) string {
	for _, l := range strings.Split(frame, "\n") {
		if strings.Contains(l, name) {
			return l
		}
	}

	return ""
}

// AC23 (R28): TTY mode shows one live line per process, starting -> ready.
func TestReadinessTTYTransition(t *testing.T) {
	long := []ui.Proc{{Name: procDB, Timeout: time.Minute}, {Name: procAP, Timeout: time.Minute}}
	r := ui.NewReadiness(long, true, at(0))
	f1 := r.Render(at(12))
	for _, n := range []string{procDB, procAP} {
		l := line(f1, n)
		if !strings.Contains(l, "◌") || !strings.Contains(l, "starting 12s") {
			t.Errorf("%s line = %q, want '◌ ... starting 12s'", n, l)
		}
	}
	r.MarkReady(procDB, at(13))
	f2 := r.Render(at(13))
	l := line(f2, procDB)
	if !strings.Contains(l, "●") || !strings.Contains(l, "ready") || strings.Contains(l, "not ready") {
		t.Errorf("db line = %q, want '● ... ready'", l)
	}
	if la := line(f2, procAP); !strings.Contains(la, "starting 13s") {
		t.Errorf("api line = %q, want still starting 13s", la)
	}
	if !strings.HasPrefix(f2, esc+"[2A") {
		t.Errorf("second TTY frame must move the cursor up 2 lines, got %q", f2)
	}
	if r.Done() {
		t.Error("Done before every process settled")
	}
	r.MarkReady(procAP, at(14))
	if !r.Done() {
		t.Error("Done once every process is ready")
	}
}

// AC24 (R28, R29): after the per-process timeout the line reads not ready with the logs hint.
func TestReadinessTimeout(t *testing.T) {
	r := ui.NewReadiness(procs(), true, at(0))
	r.Render(at(9))
	f := r.Render(at(10))
	if l := line(f, procAP); !strings.Contains(l, "not ready — drover logs api") {
		t.Errorf("api line = %q, want not ready hint", l)
	}
	if l := line(f, procDB); !strings.Contains(l, "starting 10s") {
		t.Errorf("db has a 30s timeout and must still be starting: %q", l)
	}
	if got := r.NotReady(); len(got) != 1 || got[0] != procAP {
		t.Errorf("NotReady() = %v, want [api]", got)
	}
	if r.Done() {
		t.Error("db is still starting, not Done")
	}
	r.Render(at(31))
	if !r.Done() {
		t.Error("Done once every process is ready or timed out")
	}
	if got := r.NotReady(); len(got) != 2 || got[0] != procDB || got[1] != procAP {
		t.Errorf("NotReady() = %v, want declaration order [db api]", got)
	}
}

// A process marked ready before its timeout is never reported not ready afterwards.
func TestReadinessReadyBeatsTimeout(t *testing.T) {
	r := ui.NewReadiness(procs(), true, at(0))
	r.MarkReady(procAP, at(9))
	f := r.Render(at(60))
	if strings.Contains(line(f, procAP), "not ready") {
		t.Errorf("ready process rendered as not ready: %q", line(f, procAP))
	}
	if got := r.NotReady(); len(got) != 1 || got[0] != procDB {
		t.Errorf("NotReady() = %v, want [db]", got)
	}
}

// AC23 (R28): non-TTY output is append-only, contains no escape sequences, and never repeats a state.
func TestReadinessNonTTY(t *testing.T) {
	r := ui.NewReadiness(procs(), false, at(0))
	var out strings.Builder
	out.WriteString(r.Render(at(0)))
	out.WriteString(r.Render(at(3)))
	r.MarkReady(procDB, at(4))
	out.WriteString(r.Render(at(4)))
	out.WriteString(r.Render(at(11)))
	out.WriteString(r.Render(at(12)))
	all := out.String()
	if strings.Contains(all, esc) || strings.Contains(all, "\r") {
		t.Errorf("non-TTY output has control sequences: %q", all)
	}
	if !strings.HasSuffix(all, "\n") {
		t.Errorf("non-TTY output must be line oriented: %q", all)
	}
	for needle, want := range map[string]int{"db starting": 1, "db ready": 1, "api starting": 1, "drover logs api": 1} {
		if got := strings.Count(all, needle); got != want {
			t.Errorf("%q appears %d times, want %d in:\n%s", needle, got, want, all)
		}
	}
}

// R28: rendering with nothing changed in non-TTY mode writes nothing.
func TestReadinessNonTTYNoChangeNoOutput(t *testing.T) {
	r := ui.NewReadiness(procs(), false, at(0))
	r.Render(at(0))
	if got := r.Render(at(1)); got != "" {
		t.Errorf("unchanged non-TTY render = %q, want empty", got)
	}
}
