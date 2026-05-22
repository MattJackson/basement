// Package main — login.go implements `basement login`.
//
//	basement login --endpoint URL --key BMNT... --secret ... [--profile NAME]
//
// Writes (or overwrites) the named profile in
// ~/.config/basement/config.yaml. The login command does NOT call any
// server endpoint — bearer creds are validated server-side on the
// first subsequent request, and we don't want `login` to fail if the
// operator is configuring an offline profile (e.g. CI bootstrap).
//
// If the operator wants to verify their creds work, the canonical
// follow-up is `basement regions list` (or `basement keys list`),
// which is a one-liner that exercises the bearer pipeline end-to-end.
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newLoginCmd builds the `basement login` cobra command. Flags are
// required; cobra emits the usage block if any are missing.
func newLoginCmd() *cobra.Command {
	var endpoint, key, secret string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Save bearer credentials for a basement deployment.",
		Long: `Writes (or overwrites) a profile in ~/.config/basement/config.yaml.
The profile bundles the deployment endpoint and a service-account
access key + secret. Subsequent subcommands read the profile via
--profile NAME (default "default").

Get a service account from /admin/service-accounts inside the
basement web UI, or via the basement keys create subcommand once
you already have one logged in.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if endpoint == "" || key == "" || secret == "" {
				return fmt.Errorf("--endpoint, --key, and --secret are all required")
			}
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}
			if cfg.Profiles == nil {
				cfg.Profiles = map[string]Profile{}
			}
			name := profileName(ctx.profileFlag)
			// Preserve the prior current_region_id if the operator is
			// re-logging in to refresh a rotated secret on an existing
			// profile — they almost certainly don't want their default
			// region cleared as a side-effect.
			prev := cfg.Profiles[name]
			cfg.Profiles[name] = Profile{
				Endpoint:        endpoint,
				AccessKeyID:     key,
				SecretKey:       secret,
				CurrentRegionID: prev.CurrentRegionID,
			}
			if err := SaveConfig(cfg); err != nil {
				return err
			}
			path, _ := configPath()
			fmt.Fprintf(cmd.OutOrStdout(), "Saved profile %q to %s.\n", name, path)
			return nil
		},
	}
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "basement base URL (e.g. https://basement.pq.io)")
	cmd.Flags().StringVar(&key, "key", "", "Service-account access key ID (BMNT...)")
	cmd.Flags().StringVar(&secret, "secret", "", "Service-account secret key")
	_ = cmd.MarkFlagRequired("endpoint")
	_ = cmd.MarkFlagRequired("key")
	_ = cmd.MarkFlagRequired("secret")
	return cmd
}
