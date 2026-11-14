// Package app wires the drover command line.
package app

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cabrera-evil/drover-cli/internal/errs"
)

// NewRootCmd builds the CLI.
func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "drover",
		Short:         "Supervise a stack of local processes",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(os.Stdout)
	root.PersistentFlags().String("config", "", "path to a drover.yaml (overrides discovery)")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError(err) })
	root.AddCommand(newConfigCmd(), newStacksCmd(), newUpdateCmd(version))

	return root
}

func usageError(err error) error {
	return &errs.Error{
		Code: errs.CodeUsage, Message: err.Error(), Fix: "run with --help for usage", Exit: errs.ExitUsage,
	}
}

// Run executes the CLI with the given arguments.
func Run(version string, args []string) error {
	root := NewRootCmd(version)
	root.SetArgs(args)
	err := root.Execute()
	if err != nil && strings.HasPrefix(err.Error(), "unknown command") {
		return usageError(err)
	}

	return err
}
