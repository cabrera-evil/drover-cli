// Package selection resolves which processes a command acts on from groups and presets.
package selection

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/cabrera-evil/drover-cli/internal/config"
	"github.com/cabrera-evil/drover-cli/internal/errs"
)

// Options narrows the selection. Empty Groups means every process; empty Preset means no preset.
type Options struct {
	Groups []string
	Preset string
}

// matchAny is the tags_any entry that matches every process.
const matchAny = "*"

// Select returns the process names in cfg.Order: the union of the groups' members, intersected with the preset.
// An empty intersection is not an error.
//
// A process belongs to a group when it is listed in the group's `processes`, OR when it matches the group's
// tag criteria: `tags_all` (every tag present) AND `tags_any` (at least one tag present, or `*`), each applied
// only when set. A group with no criteria has no members.
func Select(cfg *config.Config, o Options) ([]string, error) {
	groups := o.Groups
	for _, g := range groups {
		if _, ok := cfg.Groups[g]; !ok {
			return nil, unknown("group", g, slices.Sorted(maps.Keys(cfg.Groups)))
		}
	}
	var preset config.Preset
	if o.Preset != "" {
		p, ok := cfg.Presets[o.Preset]
		if !ok {
			return nil, unknown("preset", o.Preset, slices.Sorted(maps.Keys(cfg.Presets)))
		}
		preset = p
	}
	out := make([]string, 0, len(cfg.Order))
	for _, name := range cfg.Order {
		p := cfg.Processes[name]
		member := func(g string) bool { return inGroup(cfg.Groups[g], name, p) }
		if len(groups) > 0 && !slices.ContainsFunc(groups, member) {
			continue
		}
		if o.Preset != "" && !matchTags(p.Tags, preset.TagsAll, preset.TagsAny, true) {
			continue
		}
		out = append(out, name)
	}

	return out, nil
}

func inGroup(g config.Group, name string, p config.Process) bool {
	if slices.Contains(g.Processes, name) {
		return true
	}

	return matchTags(p.Tags, g.TagsAll, g.TagsAny, false)
}

// matchTags applies tags_all and tags_any, each only when set. With neither set it returns emptyOK.
func matchTags(tags, all, anyOf []string, emptyOK bool) bool {
	if len(all) == 0 && len(anyOf) == 0 {
		return emptyOK
	}
	if !isSubset(all, tags) {
		return false
	}

	return len(anyOf) == 0 || slices.Contains(anyOf, matchAny) || slices.ContainsFunc(anyOf, func(t string) bool {
		return slices.Contains(tags, t)
	})
}

func isSubset(want, have []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}

	return true
}

func unknown(kind, name string, valid []string) error {
	return &errs.Error{
		Code:    errs.CodeUsage,
		Message: fmt.Sprintf("unknown %s %q (valid: %s)", kind, name, strings.Join(valid, ", ")),
		Fix:     "use one of the valid " + kind + "s",
		Exit:    errs.ExitUsage,
	}
}
