package app

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cabrera-evil/drover-cli/internal/config"
	"github.com/cabrera-evil/drover-cli/internal/errs"
)

func newConfigCmd() *cobra.Command {
	cfg := &cobra.Command{Use: "config", Short: "Inspect and validate the stack config"}
	cfg.AddCommand(
		&cobra.Command{
			Use: "check", Short: "Validate the active config", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				c, err := loadActive(cmd)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "ok: %s (%s)\n", c.Source, c.Name)

				return err
			},
		},
		&cobra.Command{
			Use: "show", Short: "Show the resolved config (env key names only, never values)", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				c, err := loadActive(cmd)
				if err != nil {
					return err
				}
				_, err = fmt.Fprint(cmd.OutOrStdout(), describe(c))

				return err
			},
		},
		&cobra.Command{
			Use: "migrate", Short: "Upgrade the config to the current version", Args: cobra.NoArgs,
			RunE: runMigrate,
		},
	)

	return cfg
}

// activePath returns the highest-precedence stack file.
func activePath(cmd *cobra.Command) (string, error) {
	srcs, err := discover(cmd)
	if err != nil {
		return "", err
	}
	if len(srcs) == 0 {
		return "", &errs.Error{
			Code: errs.CodeNotFound, Message: "no drover.yaml found", Exit: errs.ExitConfig,
			Fix: "create a drover.yaml, add one under ~/.config/drover/stacks/, or pass --config",
		}
	}

	return srcs[0].Path, nil
}

func loadActive(cmd *cobra.Command) (*config.Config, error) {
	p, err := activePath(cmd)
	if err != nil {
		return nil, err
	}

	return config.Load(p)
}

func runMigrate(cmd *cobra.Command, _ []string) error {
	p, err := activePath(cmd)
	if err != nil {
		return err
	}
	v, err := config.ReadVersion(p)
	if err != nil {
		return err
	}
	if v == config.SupportedVersion {
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s: already current (version %d)\n", p, v)

		return err
	}

	return &errs.Error{
		Code: errs.CodeUnsupportedVersion, Message: fmt.Sprintf("no migration from version %d", v), File: p,
		Key: "version", Exit: errs.ExitConfig, Fix: "set `version: 1` if the file already uses the v1 schema",
	}
}

// describe renders a resolved config. Env values and env_file contents are never included.
func describe(c *config.Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "stack:  %s\nsource: %s\nlogdir: %s\n", c.Name, c.Source, c.LogDir)
	b.WriteString("processes:\n")
	for _, name := range slices.Sorted(maps.Keys(c.Processes)) {
		p := c.Processes[name]
		fmt.Fprintf(&b, "  %s\n    ready: %s\n", name, p.Ready.Kind)
		if len(p.Tags) > 0 {
			fmt.Fprintf(&b, "    tags: %s\n", strings.Join(p.Tags, ", "))
		}
		if len(p.DependsOn) > 0 {
			fmt.Fprintf(&b, "    depends_on: %s\n", strings.Join(p.DependsOn, ", "))
		}
		if p.Port.Value != 0 {
			fmt.Fprintf(&b, "    port: %d\n", p.Port.Value)
		}
		if p.EnvFile != "" {
			fmt.Fprintf(&b, "    env_file: %s\n", p.EnvFile)
		}
		if len(p.Env) > 0 {
			fmt.Fprintf(&b, "    env: %s\n", strings.Join(slices.Sorted(maps.Keys(p.Env)), ", "))
		}
	}

	return b.String()
}
