// Package main — keys.go implements `basement keys ...` over the
// v1.7.0a service-account admin API. The CLI lets an operator
// mint / rotate / delete other SAs (e.g. for CI runners) using their
// own SA's host:manage_users capability.
//
// Endpoints (see internal/api/admin_service_accounts.go):
//
//	GET    /admin/service-accounts
//	POST   /admin/service-accounts
//	GET    /admin/service-accounts/{id}
//	DELETE /admin/service-accounts/{id}
//	POST   /admin/service-accounts/{id}/rotate
//
// The plaintext secret rides on create + rotate responses ONCE — the
// CLI prints it to stdout (and to stderr if --output-format=table)
// with a one-time-only warning.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

type serviceAccountCap struct {
	ID    string `json:"id"`
	Scope string `json:"scope"`
}

type serviceAccount struct {
	ID           string              `json:"id"`
	OwnerUserID  string              `json:"ownerUserId"`
	Name         string              `json:"name"`
	AccessKeyID  string              `json:"accessKeyId"`
	Capabilities []serviceAccountCap `json:"capabilities"`
	Scopes       []string            `json:"scopes"`
	CreatedAt    time.Time           `json:"createdAt"`
	ExpiresAt    *time.Time          `json:"expiresAt,omitempty"`
	LastUsedAt   *time.Time          `json:"lastUsedAt,omitempty"`
	RevokedAt    *time.Time          `json:"revokedAt,omitempty"`
}

type serviceAccountWithSecret struct {
	ServiceAccount serviceAccount `json:"serviceAccount"`
	Secret         string         `json:"secret"`
}

func newKeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage service accounts (bearer credentials).",
		Long: `Service accounts are basement-issued long-lived bearer
credentials. They're scoped by capability (e.g. host:manage_users) and
audited per use. See /admin/service-accounts in the web UI for the
full management surface.`,
	}
	cmd.AddCommand(newKeysListCmd())
	cmd.AddCommand(newKeysCreateCmd())
	cmd.AddCommand(newKeysRotateCmd())
	cmd.AddCommand(newKeysDeleteCmd())
	return cmd
}

func newKeysListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List service accounts owned by the calling user.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := clientForProfile()
			if err != nil {
				return err
			}
			var out []serviceAccount
			if err := client.GetJSON(context.Background(), "/admin/service-accounts", &out); err != nil {
				return err
			}
			if !outputTable() {
				return outputJSON(cmd.OutOrStdout(), out)
			}
			rows := make([][]string, 0, len(out))
			for _, sa := range out {
				status := "active"
				if sa.RevokedAt != nil {
					status = "revoked"
				}
				rows = append(rows, []string{sa.ID, sa.Name, sa.AccessKeyID, status, sa.CreatedAt.Format(time.RFC3339)})
			}
			renderTable(cmd.OutOrStdout(), []string{"ID", "NAME", "ACCESS_KEY", "STATUS", "CREATED"}, rows)
			return nil
		},
	}
}

func newKeysCreateCmd() *cobra.Command {
	var caps []string
	var scopes []string
	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Mint a new service account.",
		Long: `Creates a new service account and returns the plaintext
secret ONCE — store it immediately, the server never echoes it back.

Capabilities are specified as --capability ID --scope SCOPE pairs. To
pass multiple, repeat both flags in order: --capability host:manage_users
--scope host:* --capability bucket:read --scope bucket:foo:bar.

The number of --capability and --scope flags MUST match.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(caps) != len(scopes) {
				return fmt.Errorf("--capability count (%d) must match --scope count (%d)", len(caps), len(scopes))
			}
			client, _, err := clientForProfile()
			if err != nil {
				return err
			}
			capabilities := make([]serviceAccountCap, 0, len(caps))
			for i, id := range caps {
				capabilities = append(capabilities, serviceAccountCap{ID: id, Scope: scopes[i]})
			}
			body := map[string]any{
				"name":         args[0],
				"capabilities": capabilities,
				"scopes":       []string{}, // SA-wide scope hints — kept empty in CLI v1.8.0a
			}
			var out serviceAccountWithSecret
			if err := client.PostJSON(context.Background(), "/admin/service-accounts", body, &out); err != nil {
				return err
			}
			if !outputTable() {
				return outputJSON(cmd.OutOrStdout(), out)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created service account %s (%s)\n", out.ServiceAccount.ID, out.ServiceAccount.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Access key:  %s\n", out.ServiceAccount.AccessKeyID)
			fmt.Fprintf(cmd.OutOrStdout(), "Secret key:  %s\n", out.Secret)
			fmt.Fprintln(cmd.OutOrStdout(), "WARNING: the secret is shown only once — copy it now.")
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&caps, "capability", nil, "Capability ID (repeatable; must pair with --scope)")
	cmd.Flags().StringSliceVar(&scopes, "scope", nil, "Capability scope (repeatable; must pair with --capability)")
	return cmd
}

func newKeysRotateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rotate ID",
		Short: "Rotate a service account's secret.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := clientForProfile()
			if err != nil {
				return err
			}
			var out serviceAccountWithSecret
			if err := client.PostJSON(context.Background(), "/admin/service-accounts/"+args[0]+"/rotate", nil, &out); err != nil {
				return err
			}
			if !outputTable() {
				return outputJSON(cmd.OutOrStdout(), out)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Rotated service account %s\n", out.ServiceAccount.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "Access key:  %s\n", out.ServiceAccount.AccessKeyID)
			fmt.Fprintf(cmd.OutOrStdout(), "New secret:  %s\n", out.Secret)
			fmt.Fprintln(cmd.OutOrStdout(), "WARNING: the secret is shown only once — copy it now.")
			return nil
		},
	}
}

func newKeysDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete ID",
		Short: "Revoke a service account.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := clientForProfile()
			if err != nil {
				return err
			}
			if err := client.DeleteJSON(context.Background(), "/admin/service-accounts/"+args[0], nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Revoked service account %s\n", args[0])
			return nil
		},
	}
}
