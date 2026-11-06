package probe_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/cabrera-evil/drover-cli/internal/probe"
)

const (
	goosLinux  = "linux"
	goosDarwin = "darwin"
	portPG     = 5432
	portWeb    = 3000
	portAlt    = 8080
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

// AC38 (R40, R50): `ss -Hltn` output parsing; the port is matched exactly, never by substring.
func TestParseSS(t *testing.T) {
	out := fixture(t, "ss_listen.txt")
	tests := []struct {
		port int
		want bool
	}{{portPG, true}, {portWeb, true}, {portAlt, true}, {543, false}, {54320, false}, {22, false}}
	for _, tc := range tests {
		if got := probe.ParseSS(out, tc.port); got != tc.want {
			t.Errorf("ParseSS port %d = %v, want %v", tc.port, got, tc.want)
		}
	}
	if probe.ParseSS("", portPG) {
		t.Error("empty output must not be in use")
	}
}

// AC38 (R40, R50): /proc/net/tcp and tcp6, hex port, state 0A = LISTEN.
func TestParseProcNet(t *testing.T) {
	v4 := fixture(t, "proc_net_tcp.txt")
	v6 := fixture(t, "proc_net_tcp6.txt")
	tests := []struct {
		name    string
		content string
		port    int
		want    bool
	}{
		{"v4 listen 5432", v4, portPG, true},
		{"v4 established 9000 is not listening", v4, 9000, false},
		{"v4 other port", v4, portWeb, false},
		{"v6 listen 3000", v6, portWeb, true},
		{"v6 listen 8080", v6, portAlt, true},
		{"v6 other port", v6, portPG, false},
		{"empty", "", portPG, false},
		{"header only", strings.SplitN(v4, "\n", 2)[0], portPG, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := probe.ParseProcNet(tc.content, tc.port); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// AC38 (R51): macOS `lsof -nP -iTCP -sTCP:LISTEN` output parsing.
func TestParseLsof(t *testing.T) {
	out := fixture(t, "lsof_listen.txt")
	tests := []struct {
		port int
		want bool
	}{{portWeb, true}, {portPG, true}, {49152, true}, {portAlt, false}, {300, false}}
	for _, tc := range tests {
		if got := probe.ParseLsof(out, tc.port); got != tc.want {
			t.Errorf("ParseLsof port %d = %v, want %v", tc.port, got, tc.want)
		}
	}
}

type runRec struct {
	calls []string
	out   map[string]string // command name -> stdout
	err   map[string]error
}

func (r *runRec) run(name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	if err := r.err[name]; err != nil {
		return nil, err
	}

	return []byte(r.out[name]), nil
}

func readFrom(files map[string]string) probe.ReadFile {
	return func(p string) ([]byte, error) {
		if c, ok := files[p]; ok {
			return []byte(c), nil
		}

		return nil, os.ErrNotExist
	}
}

// AC38: Linux implementation prefers `ss -Hltn "sport = :PORT"` and never dials.
func TestLinuxTableUsesSS(t *testing.T) {
	rec := &runRec{out: map[string]string{"ss": fixture(t, "ss_listen.txt")}}
	tbl, err := probe.NewTable(goosLinux, rec.run, readFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if ok, listenErr := tbl.Listening(portPG); listenErr != nil || !ok {
		t.Fatalf("Listening(%d) = %v, %v; want true", portPG, ok, listenErr)
	}
	if len(rec.calls) != 1 || !strings.HasPrefix(rec.calls[0], "ss ") ||
		!strings.Contains(rec.calls[0], "-Hltn") || !strings.Contains(rec.calls[0], ":5432") {
		t.Errorf("unexpected ss invocation: %v", rec.calls)
	}
	if ok, _ := tbl.Listening(22); ok {
		t.Error("port 22 is not in the ss output")
	}
}

// AC38: when ss is unavailable the Linux implementation falls back to /proc/net/tcp and tcp6.
func TestLinuxTableFallsBackToProc(t *testing.T) {
	rec := &runRec{err: map[string]error{"ss": errors.New("not found")}}
	files := map[string]string{
		"/proc/net/tcp":  fixture(t, "proc_net_tcp.txt"),
		"/proc/net/tcp6": fixture(t, "proc_net_tcp6.txt"),
	}
	tbl, err := probe.NewTable(goosLinux, rec.run, readFrom(files))
	if err != nil {
		t.Fatal(err)
	}
	for port, want := range map[int]bool{portPG: true, portWeb: true, portAlt: true, 9000: false} {
		ok, listenErr := tbl.Listening(port)
		if listenErr != nil || ok != want {
			t.Errorf("Listening(%d) = %v, %v; want %v", port, ok, listenErr, want)
		}
	}
}

// AC38: with neither ss nor /proc readable the error surfaces; it is never reported as "free".
func TestLinuxTableNoSource(t *testing.T) {
	rec := &runRec{err: map[string]error{"ss": errors.New("not found")}}
	tbl, err := probe.NewTable(goosLinux, rec.run, readFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, listenErr := tbl.Listening(portPG); listenErr == nil {
		t.Error("expected an error when no socket table source works")
	}
}

// AC38 (R51): macOS implementation runs lsof and parses its output.
func TestDarwinTable(t *testing.T) {
	rec := &runRec{out: map[string]string{"lsof": fixture(t, "lsof_listen.txt")}}
	tbl, err := probe.NewTable(goosDarwin, rec.run, readFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if ok, listenErr := tbl.Listening(portWeb); listenErr != nil || !ok {
		t.Errorf("Listening(%d) = %v, %v; want true", portWeb, ok, listenErr)
	}
	if ok, _ := tbl.Listening(portAlt); ok {
		t.Errorf("port %d must be free", portAlt)
	}
	if len(rec.calls) == 0 || !strings.HasPrefix(rec.calls[0], "lsof ") ||
		!strings.Contains(rec.calls[0], "-sTCP:LISTEN") {
		t.Errorf("unexpected lsof invocation: %v", rec.calls)
	}
}

// AC38: selection by GOOS is injectable; unsupported platforms fail clearly.
func TestNewTableUnsupportedOS(t *testing.T) {
	if _, err := probe.NewTable("plan9", (&runRec{}).run, readFrom(nil)); err == nil {
		t.Error("expected an error for an unsupported GOOS")
	}
}

type fakeTable struct {
	ports map[int]bool
	err   error
	asked []int
}

func (f *fakeTable) Listening(port int) (bool, error) {
	f.asked = append(f.asked, port)

	return f.ports[port], f.err
}

// AC29 (R40): the PortChecker adapter used by orchestrator.Preflight reports from the table only.
func TestPortCheckerAdapter(t *testing.T) {
	ft := &fakeTable{ports: map[int]bool{portPG: true}}
	pc := probe.NewPortChecker(ft)
	if !pc.InUse(portPG) {
		t.Error("listed port must be in use")
	}
	if pc.InUse(portWeb) {
		t.Error("unlisted port must be free")
	}
	if len(ft.asked) != 2 {
		t.Errorf("table asked %v", ft.asked)
	}
	// ponytail: a table error is treated as "not in use" so preflight is not blocked by a broken probe.
	if probe.NewPortChecker(&fakeTable{err: errors.New("boom")}).InUse(portPG) {
		t.Error("table error must map to not in use")
	}
}
