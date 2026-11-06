package app

import (
	"os"

	"github.com/spf13/cobra"

	"go-template/internal/config"
)

// NewRootCmd builds the CLI; the root command prints the greeting.
func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "go-template",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Println(greeting())

			return nil
		},
	}
	root.SetOut(os.Stdout)
	root.AddCommand(newUpdateCmd(version))

	return root
}

func greeting() string {
	return "Hello, " + config.Load().Name + "!"
}

// Run executes the CLI with the given arguments.
func Run(version string, args []string) error {
	root := NewRootCmd(version)
	root.SetArgs(args)

	return root.Execute()
}
