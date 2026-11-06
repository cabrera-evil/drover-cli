package mux_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cabrera-evil/drover-cli/internal/diag"
	"github.com/cabrera-evil/drover-cli/internal/mux"
)

const fakePackage = "github.com/cabrera-evil/drover-cli/internal/testutil/fakeherdr"

//nolint:gochecknoglobals // the fake herdr binary is built once per test binary in TestMain.
var builtBin string

// TestMain builds internal/testutil/fakeherdr once into a temp dir as a binary named "herdr" and removes it after.
func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	dir, err := os.MkdirTemp("", "fakeherdr")
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake herdr temp dir:", err)

		return 1
	}
	defer os.RemoveAll(dir)

	out := filepath.Join(dir, "herdr")
	if b, berr := exec.Command("go", "build", "-o", out, fakePackage).CombinedOutput(); berr != nil {
		fmt.Fprintf(os.Stderr, "build fake herdr: %v\n%s", berr, b)

		return 1
	}
	builtBin = out

	return m.Run()
}

// fakeBin returns the path of the fake herdr binary built by TestMain.
func fakeBin(t *testing.T) string {
	t.Helper()

	return builtBin
}

// newHerdr returns an adapter wired to the fake binary with a private state file, plus that file's path.
func newHerdr(t *testing.T, opts ...mux.Option) (*mux.Herdr, string) {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "fake.json")
	t.Setenv("FAKE_HERDR_STATE", statePath)
	t.Setenv("FAKE_HERDR_FAIL", "")
	t.Setenv("FAKE_HERDR_SLEEP_MS", "")

	return mux.NewHerdr(fakeBin(t), opts...), statePath
}

// recordedCalls returns the argv of every call the fake received.
func recordedCalls(t *testing.T, statePath string) [][]string {
	t.Helper()
	b, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var w struct {
		Calls [][]string `json:"calls"`
	}
	if err = json.Unmarshal(b, &w); err != nil {
		t.Fatal(err)
	}

	return w.Calls
}

func lastCall(t *testing.T, statePath string) []string {
	t.Helper()
	calls := recordedCalls(t, statePath)
	if len(calls) == 0 {
		t.Fatal("fake herdr received no calls")
	}

	return calls[len(calls)-1]
}

// flagOf returns the value following name in args, or "" when absent.
func flagOf(args []string, name string) string {
	for i := range len(args) - 1 {
		if args[i] == name {
			return args[i+1]
		}
	}

	return ""
}

func has(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}

	return false
}

// stepClock advances 12ms on every Now call, so each timed call takes exactly 12ms.
func stepClock() func() time.Time {
	now := time.Unix(1_700_000_000, 0)

	return func() time.Time {
		t := now
		now = now.Add(12 * time.Millisecond)

		return t
	}
}

// verboseDiag returns a -v logger writing to the returned buffer.
func verboseDiag(t *testing.T) (*diag.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	l, err := diag.New(diag.Options{
		Verbose:  true,
		Stderr:   &buf,
		StateDir: t.TempDir(),
		Getenv:   func(string) string { return "" },
		Now:      stepClock(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })

	return l, &buf
}
