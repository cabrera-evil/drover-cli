package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/cabrera-evil/drover-cli/internal/app"
	"github.com/cabrera-evil/drover-cli/internal/errs"
)

// Version is injected at build time via -ldflags "-X main.Version=...".
var Version = "dev" //nolint:gochecknoglobals // set via -ldflags

func main() {
	if err := app.Run(Version, os.Args[1:]); err != nil {
		// The interactive update view already showed an ErrReported error.
		if !errors.Is(err, app.ErrReported) {
			fmt.Fprintln(os.Stderr, "error:", err)
			var de *errs.Error
			if errors.As(err, &de) && de.Fix != "" {
				fmt.Fprintln(os.Stderr, "fix:", de.Fix)
			}
		}
		os.Exit(errs.ExitCode(err))
	}
}
