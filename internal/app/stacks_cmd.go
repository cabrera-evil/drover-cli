package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cabrera-evil/drover-cli/internal/config"
)

func newStacksCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stacks",
		Short: "List every discovered stack file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			srcs, err := discover(cmd)
			if err != nil {
				return err
			}
			var b strings.Builder
			owner := map[string]string{} // stack name -> nearest source that declares it
			for _, s := range srcs {
				fmt.Fprintf(&b, "%-20s %s", s.Name, s.Path)
				if first, ok := owner[s.Name]; ok && s.Name != "" {
					fmt.Fprintf(&b, "  (shadowed by %s)", first)
				} else {
					owner[s.Name] = s.Path
				}
				b.WriteByte('\n')
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), b.String())

			return err
		},
	}
}

// discover lists stack sources for the current directory and the --config flag.
func discover(cmd *cobra.Command) ([]config.Source, error) {
	flag, _ := cmd.Flags().GetString("config")
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	return config.Sources(cwd, flag)
}
