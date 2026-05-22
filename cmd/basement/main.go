// Package main is the `basement` CLI binary entrypoint
// (v1.8.0a). The CLI talks to a running basement deployment via
// the JSON API using bearer auth (service-account creds from
// /admin/service-accounts).
//
// Subcommand layout uses cobra:
//
//	basement login                 — write a profile to config.yaml
//	basement regions list/add/...  — manage user-tier region keychain
//	basement buckets list          — list buckets for the current region
//	basement objects list/get/...  — data-plane ops via presigned URLs
//	basement keys list/create/...  — service-account management
//	basement version               — print build metadata
//
// Every subcommand inherits the persistent flags:
//
//	--profile NAME                 — config-file profile (default "default")
//	--region REGION_ID             — override profile.current_region_id
//	--output-format json|table     — JSON for scripts, table for humans
//
// Global env vars: $BASEMENT_PROFILE, $BASEMENT_SECRET_KEY.
//
// main() is intentionally tiny — it constructs the root command and
// hands off to cobra. Each subcommand file (login.go, regions.go ...)
// registers itself in init() so adding a command is a one-file change.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// rootContext carries the resolved profile + parsed flags through to
// each subcommand handler. Subcommands read this via getContext() — a
// helper that lazily loads the config + applies env overrides on first
// access. Doing it lazily means `basement login` (which has no
// profile yet) doesn't crash on a missing config file.
type rootContext struct {
	profileFlag      string
	regionFlag       string
	outputFormatFlag string
}

var ctx = &rootContext{}

// newRootCmd builds the cobra root. Exported only for tests — main()
// calls Execute() on the result.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "basement",
		Short:         "Command-line client for basement.",
		Long:          "basement is the official CLI for basement, the multi-backend S3/Garage admin + user portal. It uses service-account bearer credentials (see /admin/service-accounts) and supports multiple profiles via ~/.config/basement/config.yaml.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&ctx.profileFlag, "profile", "", "Profile name in ~/.config/basement/config.yaml (default \"default\" or $BASEMENT_PROFILE)")
	root.PersistentFlags().StringVar(&ctx.regionFlag, "region", "", "Region ID override (default = profile.current_region_id)")
	root.PersistentFlags().StringVar(&ctx.outputFormatFlag, "output-format", "table", "Output format: table or json")

	root.AddCommand(newLoginCmd())
	root.AddCommand(newRegionsCmd())
	root.AddCommand(newBucketsCmd())
	root.AddCommand(newObjectsCmd())
	root.AddCommand(newKeysCmd())
	root.AddCommand(newVersionCmd())

	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// clientForProfile loads the config, resolves the active profile, and
// returns a ready Client. Every subcommand calls this exactly once at
// the top of its Run.
func clientForProfile() (*Client, Profile, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, Profile{}, err
	}
	name := profileName(ctx.profileFlag)
	p, err := resolveProfile(cfg, name)
	if err != nil {
		return nil, Profile{}, err
	}
	if p.SecretKey == "" {
		return nil, Profile{}, fmt.Errorf("profile %q has no secret_key (set in config.yaml or pass $BASEMENT_SECRET_KEY)", name)
	}
	return NewClient(p.Endpoint, p.AccessKeyID, p.SecretKey), p, nil
}

// regionID resolves the active region ID: --region flag wins, then
// the profile's current_region_id. Returns an error if neither is set
// — every region-scoped subcommand calls this so the error surface is
// consistent.
func regionID(p Profile) (string, error) {
	if ctx.regionFlag != "" {
		return ctx.regionFlag, nil
	}
	if p.CurrentRegionID != "" {
		return p.CurrentRegionID, nil
	}
	return "", fmt.Errorf("no region specified — pass --region REGION_ID or set current_region_id in your profile")
}
