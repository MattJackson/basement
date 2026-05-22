// Package main — version.go implements `basement version`. Build-time
// metadata is shared with the server (internal/version) so a single
// -ldflags incantation stamps both binaries.
package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mattjackson/basement/internal/version"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version, commit, and build timestamp.",
		RunE: func(cmd *cobra.Command, args []string) error {
			info := version.Get()
			if !outputTable() {
				return outputJSON(cmd.OutOrStdout(), info)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "basement %s (%s, built %s)\n", info.Version, info.Commit, info.BuiltAt)
			return nil
		},
	}
}
