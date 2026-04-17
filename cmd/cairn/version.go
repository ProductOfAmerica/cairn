package main

import (
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Overridden at link time via -ldflags "-X main.version=...".
var version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version info",
		RunE: func(cmd *cobra.Command, _ []string) error {
			v := version
			if v == "dev" {
				if bi, ok := debug.ReadBuildInfo(); ok {
					v = bi.Main.Version
				}
			}
			cmd.Println(v)
			return nil
		},
	}
}
