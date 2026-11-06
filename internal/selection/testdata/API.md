# Expected production API for slice S2 selection (derived from the tests)

## package `config` additions
`Config.Order []string` (`yaml:"-"`): process names in YAML declaration order, filled by `Load`
(maps are unordered, so this is the single source of deterministic ordering).

## package `selection` (internal/selection)
```go
type Options struct {
    Groups []string // empty => all groups (every process)
    Preset string   // "" => no preset (whole group); else must exist in cfg.Presets
}
// Select returns process names: union of the groups' members, intersected with the preset,
// in cfg.Order. Returns an empty (non-error) result for an empty intersection.
func Select(cfg *config.Config, o Options) ([]string, error)
```
Group membership: `processes` list, `tags_all`, `tags_any`. Preset: `tags_all`, `tags_any`; a `tags_any`
entry `*` matches every process. Unknown group/preset => `*errs.Error{Code: errs.CodeUsage, Exit: errs.ExitUsage}`
whose message names the bad value and lists every valid option.
