package main

import (
	"fmt"
	"os"

	"go-template/internal/app"
)

// Version is injected at build time via -ldflags "-X main.Version=...".
var Version = "dev" //nolint:gochecknoglobals // set via -ldflags

func main() {
	if err := app.Run(Version, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
