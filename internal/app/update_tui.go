package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// errBlank replaces an error whose text is empty once sanitized, so the view never shows a blank line.
const errBlank = "update failed"

// urlQuery matches the query string of any URL embedded in text.
var urlQuery = regexp.MustCompile(`(\w+://[^\s"'<>?]+)\?[^\s"'<>]*`)

// clean makes untrusted text safe to render: no escape sequences, no control characters (newlines
// aside) and no URL query strings, which may carry tokens. Queries are removed before and after the
// escape stripping because ansi.Strip can swallow a "?" and glue a query back onto a URL.
func clean(s string) string {
	s = ansi.Strip(urlQuery.ReplaceAllString(s, "$1"))
	s = strings.Map(func(r rune) rune {
		if r != '\n' && unicode.IsControl(r) {
			return -1
		}

		return r
	}, s)

	return urlQuery.ReplaceAllString(s, "$1")
}

// swallowQuitWhileUpdating is a tea.WithFilter that drops quit and interrupt (SIGINT/SIGTERM) messages
// while the replacement runs, so it is not cut short.
//
//nolint:ireturn // matches the tea.WithFilter signature
func swallowQuitWhileUpdating(m tea.Model, msg tea.Msg) tea.Msg {
	if um, ok := m.(updateModel); ok && um.state == stateUpdating {
		switch msg.(type) {
		case tea.QuitMsg, tea.InterruptMsg:
			return nil
		}
	}

	return msg
}

// updateCheck is the outcome of looking up a release; versions carry no "v" prefix.
type updateCheck struct {
	Current, Latest string
	Available       bool   // false means already up to date
	ManualURL       string // non-empty means the executable is not writable
}

type updateState int

const (
	stateChecking updateState = iota
	stateConfirm
	stateUpdating
	stateDone
)

type (
	checkedMsg struct {
		check updateCheck
		err   error
	}
	replacedMsg struct{ err error }
)

// updateModel is the interactive update flow: check, confirm, replace, result.
type updateModel struct {
	detectCmd  tea.Cmd
	replaceCmd tea.Cmd
	spin       spinner.Model
	state      updateState
	check      updateCheck
	view       string
	err        error
}

// newUpdateModel builds the model; detect and replace own all error wrapping.
func newUpdateModel(
	ctx context.Context,
	detect func(context.Context) (updateCheck, error),
	replace func(context.Context) error,
) updateModel {
	return updateModel{
		spin: spinner.New(spinner.WithSpinner(spinner.Dot)),
		detectCmd: func() tea.Msg {
			c, err := detect(ctx)

			return checkedMsg{c, err}
		},
		replaceCmd: func() tea.Msg { return replacedMsg{replace(ctx)} },
	}
}

// Err is the flow's outcome: nil on success, up to date or unwritable.
func (m updateModel) Err() error { return m.err }

// Init starts the spinner and the release lookup.
func (m updateModel) Init() tea.Cmd { return tea.Batch(m.spin.Tick, m.detectCmd) }

// Update advances the flow on key presses and command results.
//
//nolint:ireturn // implements tea.Model
func (m updateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.state == stateDone {
			return m, nil
		}

		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)

		return m, cmd
	case checkedMsg:
		return m.checked(msg)
	case replacedMsg:
		if msg.err != nil {
			return m.finish("", msg.err)
		}

		return m.finish(fmt.Sprintf("updated %s -> %s\n", m.check.Current, m.check.Latest), nil)
	case tea.KeyMsg:
		return m.key(msg.String())
	}

	return m, nil
}

//nolint:ireturn // implements tea.Model
func (m updateModel) checked(msg checkedMsg) (tea.Model, tea.Cmd) {
	if m.state != stateChecking {
		return m, nil
	}

	m.check = msg.check

	switch {
	case msg.err != nil:
		return m.finish("", msg.err)
	case !msg.check.Available:
		return m.finish(fmt.Sprintf("already up to date (%s)\n", msg.check.Current), nil)
	case msg.check.ManualURL != "":
		return m.finish(fmt.Sprintf(
			"not writable; download %s manually from:\n%s\n", msg.check.Latest, clean(msg.check.ManualURL)), nil)
	}

	m.state = stateConfirm

	return m, nil
}

//nolint:ireturn // implements tea.Model
func (m updateModel) key(k string) (tea.Model, tea.Cmd) {
	switch m.state {
	case stateChecking:
		if k == "q" || k == "ctrl+c" {
			return m.finish("", errCancelled)
		}
	case stateConfirm:
		switch k {
		case "y", "Y":
			m.state = stateUpdating

			return m, m.replaceCmd
		case "n", "N", "q", "enter", "esc", "ctrl+c":
			return m.finish("", errCancelled)
		}
	case stateUpdating, stateDone: // not cancellable once the replacement started
	}

	return m, nil
}

//nolint:ireturn // implements tea.Model
func (m updateModel) finish(view string, err error) (tea.Model, tea.Cmd) {
	m.state, m.view, m.err = stateDone, view, err
	if err != nil {
		text := clean(err.Error())
		if strings.TrimSpace(text) == "" {
			text = errBlank
		}

		m.view = text + "\n"
	}

	return m, tea.Quit
}

// View renders the current step.
func (m updateModel) View() string {
	switch m.state {
	case stateChecking:
		return m.spin.View() + " checking for updates...\n"
	case stateConfirm:
		return fmt.Sprintf("Update %s -> %s? [y/N] ", m.check.Current, m.check.Latest)
	case stateUpdating:
		return m.spin.View() + " updating...\n"
	case stateDone:
	}

	return m.view
}
