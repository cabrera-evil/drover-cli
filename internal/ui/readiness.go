// Package ui renders terminal output. The readiness panel is a pure state machine: no goroutines, no
// terminal I/O, time is passed in, and the caller writes the returned strings.
package ui

import (
	"fmt"
	"strings"
	"time"
)

const (
	cursorUp  = "\x1b[%dA"
	clearLine = "\r\x1b[2K"
)

type state int

const (
	starting state = iota
	ready
	notReady
)

// Proc is a process the panel tracks.
type Proc struct {
	Name    string
	Timeout time.Duration // per-process readiness timeout
}

type entry struct {
	proc    Proc
	state   state
	emitted bool  // non-TTY: the current state was already written
	written state // non-TTY: the last state written
}

// Readiness is the live readiness panel.
type Readiness struct {
	entries []*entry
	tty     bool
	start   time.Time
	drawn   bool
}

// NewReadiness starts every process in "starting" at start. tty selects the live-rewrite output mode.
func NewReadiness(procs []Proc, tty bool, start time.Time) *Readiness {
	r := &Readiness{tty: tty, start: start}
	for _, p := range procs {
		r.entries = append(r.entries, &entry{proc: p})
	}

	return r
}

// MarkReady records readiness; a process already timed out stays not ready and unknown names are ignored.
func (r *Readiness) MarkReady(name string, _ time.Time) {
	for _, e := range r.entries {
		if e.proc.Name == name && e.state == starting {
			e.state = ready
		}
	}
}

func (r *Readiness) settle(now time.Time) {
	for _, e := range r.entries {
		if e.state == starting && now.Sub(r.start) >= e.proc.Timeout {
			e.state = notReady
		}
	}
}

func (r *Readiness) line(e *entry, now time.Time) string {
	switch e.state {
	case ready:
		return "● " + e.proc.Name + " ready"
	case notReady:
		return "● " + e.proc.Name + " not ready — drover logs " + e.proc.Name
	case starting:
	}

	return fmt.Sprintf("◌ %s starting %ds", e.proc.Name, int(now.Sub(r.start)/time.Second))
}

// Render settles timeouts and returns the text to write: a full rewrite of every line in TTY mode, or only
// the lines whose state changed in non-TTY mode ("" when nothing changed).
func (r *Readiness) Render(now time.Time) string {
	r.settle(now)
	var b strings.Builder
	if r.tty {
		if r.drawn {
			fmt.Fprintf(&b, cursorUp, len(r.entries))
		}
		for _, e := range r.entries {
			if r.drawn {
				b.WriteString(clearLine)
			}
			b.WriteString(r.line(e, now) + "\n")
		}
		r.drawn = true

		return b.String()
	}
	for _, e := range r.entries {
		if e.emitted && e.written == e.state {
			continue
		}
		e.emitted, e.written = true, e.state
		b.WriteString(r.line(e, now) + "\n")
	}

	return b.String()
}

// Done reports whether every process is ready or timed out.
func (r *Readiness) Done() bool {
	for _, e := range r.entries {
		if e.state == starting {
			return false
		}
	}

	return true
}

// NotReady returns the timed-out process names in declaration order.
func (r *Readiness) NotReady() []string {
	var out []string
	for _, e := range r.entries {
		if e.state == notReady {
			out = append(out, e.proc.Name)
		}
	}

	return out
}
