// Package orchestrator plans process start order and checks preconditions before anything is created.
package orchestrator

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cabrera-evil/drover-cli/internal/config"
	"github.com/cabrera-evil/drover-cli/internal/errs"
)

// Levels returns start waves for the selected names: a process runs in the wave after all of its selected
// dependencies; within a wave names follow cfg.Order. Dependencies outside names are ignored.
func Levels(cfg *config.Config, names []string) ([][]string, error) {
	if err := unknownDeps(cfg, names); err != nil {
		return nil, err
	}
	if err := findCycle(cfg, names); err != nil {
		return nil, err
	}
	sel := map[string]bool{}
	for _, n := range names {
		sel[n] = true
	}
	var levels [][]string
	placed := map[string]bool{}
	for len(placed) < len(sel) {
		var wave []string
		for _, n := range cfg.Order {
			if sel[n] && !placed[n] && depsPlaced(cfg.Processes[n].DependsOn, sel, placed) {
				wave = append(wave, n)
			}
		}
		for _, n := range wave {
			placed[n] = true
		}
		levels = append(levels, wave)
	}

	return levels, nil
}

// depsPlaced reports whether every selected dependency is already placed in an earlier wave.
func depsPlaced(deps []string, sel, placed map[string]bool) bool {
	for _, d := range deps {
		if sel[d] && !placed[d] {
			return false
		}
	}

	return true
}

func preflightErr(code, process, msg, fix string) *errs.Error {
	return &errs.Error{Code: code, Message: msg, Key: "processes." + process, Fix: fix, Exit: errs.ExitPreflight}
}

func unknownDeps(cfg *config.Config, names []string) error {
	for _, n := range names {
		for _, d := range cfg.Processes[n].DependsOn {
			if _, ok := cfg.Processes[d]; !ok {
				return unknownDep(n, d)
			}
		}
	}

	return nil
}

func unknownDep(n, d string) *errs.Error {
	return preflightErr(errs.CodeDepUnknown, n, fmt.Sprintf("process %q depends on unknown process %q", n, d),
		"fix the name in depends_on or define the process")
}

// findCycle returns a DEP-CYCLE error with the path "a -> b -> a" when the selected processes form a cycle.
func findCycle(cfg *config.Config, names []string) error {
	sel := map[string]bool{}
	for _, n := range names {
		sel[n] = true
	}
	const (
		visiting = 1
		done     = 2
	)
	state := map[string]int{}
	var stack []string
	var visit func(n string) []string
	visit = func(n string) []string {
		state[n] = visiting
		stack = append(stack, n)
		for _, d := range cfg.Processes[n].DependsOn {
			if !sel[d] {
				continue
			}
			switch state[d] {
			case visiting:
				return append(slices.Clone(stack[slices.Index(stack, d):]), d)
			case 0:
				if p := visit(d); p != nil {
					return p
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[n] = done

		return nil
	}
	for _, n := range cfg.Order {
		if !sel[n] || state[n] != 0 {
			continue
		}
		if p := visit(n); p != nil {
			return preflightErr(errs.CodeDepCycle, p[0], "dependency cycle: "+strings.Join(p, " -> "),
				"remove one depends_on edge in the cycle")
		}
	}

	return nil
}
