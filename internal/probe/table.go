// Package probe checks whether ports are in use and whether processes are ready. Port checks read the
// kernel socket table and never connect to the port.
package probe

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	procTCP         = "/proc/net/tcp"
	procTCP6        = "/proc/net/tcp6"
	procListenState = "0A"
	hexBase         = 16
	procMinFields   = 4
	ssFields        = 4
	goosLinux       = "linux"
	goosDarwin      = "darwin"
)

// PortTable reports whether a TCP port is listening by reading the socket table; it never dials.
type PortTable interface{ Listening(port int) (bool, error) }

// Exec runs a command and returns its stdout.
type Exec func(name string, args ...string) ([]byte, error)

// ReadFile reads a file.
type ReadFile func(path string) ([]byte, error)

const execTimeout = 5 * time.Second

// OSExec runs the binary directly (no shell, so no alias) with a bounded timeout.
func OSExec(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	return exec.CommandContext(ctx, name, args...).Output()
}

// OSReadFile is os.ReadFile, for use as a ReadFile.
func OSReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

// portSuffix reports whether addr ends with ":<port>".
func portSuffix(addr string, port int) bool {
	return strings.HasSuffix(addr, ":"+strconv.Itoa(port))
}

// ParseSS reports whether `ss -Hltn` output lists a listener on the exact local port.
func ParseSS(out string, port int) bool {
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= ssFields && portSuffix(f[3], port) {
			return true
		}
	}

	return false
}

// ParseProcNet reports whether /proc/net/tcp or tcp6 content has a LISTEN socket on port.
func ParseProcNet(content string, port int) bool {
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < procMinFields || f[0] == "sl" || f[3] != procListenState {
			continue
		}
		_, hexPort, ok := strings.Cut(f[1], ":")
		if !ok {
			continue
		}
		if n, err := strconv.ParseInt(hexPort, hexBase, 32); err == nil && int(n) == port {
			return true
		}
	}

	return false
}

// ParseLsof reports whether `lsof -nP -iTCP -sTCP:LISTEN` output lists a listener on port.
func ParseLsof(out string, port int) bool {
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasSuffix(line, "(LISTEN)") {
			continue
		}
		f := strings.Fields(strings.TrimSuffix(line, "(LISTEN)"))
		if len(f) > 0 && portSuffix(f[len(f)-1], port) {
			return true
		}
	}

	return false
}

type linuxTable struct {
	run  Exec
	read ReadFile
}

func (t linuxTable) Listening(port int) (bool, error) {
	out, err := t.run("ss", "-Hltn", fmt.Sprintf("sport = :%d", port))
	if err == nil {
		return ParseSS(string(out), port), nil
	}
	var found, readAny bool
	for _, p := range []string{procTCP, procTCP6} {
		b, rerr := t.read(p)
		if rerr != nil {
			continue
		}
		readAny = true
		found = found || ParseProcNet(string(b), port)
	}
	if !readAny {
		return false, fmt.Errorf("no socket table source: ss failed (%w) and %s is unreadable", err, procTCP)
	}

	return found, nil
}

type darwinTable struct{ run Exec }

func (t darwinTable) Listening(port int) (bool, error) {
	out, err := t.run("lsof", "-nP", "-iTCP", "-sTCP:LISTEN")
	if err != nil {
		return false, fmt.Errorf("lsof: %w", err)
	}

	return ParseLsof(string(out), port), nil
}

// NewTable picks the socket table implementation for goos ("linux" or "darwin").
func NewTable(goos string, run Exec, read ReadFile) (PortTable, error) { //nolint:ireturn // the interface is the API
	switch goos {
	case goosLinux:
		return linuxTable{run: run, read: read}, nil
	case goosDarwin:
		return darwinTable{run: run}, nil
	}

	return nil, errors.New("unsupported platform for port checks: " + goos)
}

// PortChecker adapts a PortTable to the orchestrator's PortChecker (InUse); a table error means not in use.
type PortChecker struct{ t PortTable }

// NewPortChecker wraps t.
func NewPortChecker(t PortTable) *PortChecker { return &PortChecker{t: t} }

// InUse reports whether port is listening.
func (p *PortChecker) InUse(port int) bool {
	ok, err := p.t.Listening(port)

	return err == nil && ok
}
