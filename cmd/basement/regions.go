// Package main — regions.go implements `basement regions ...`. The
// region keychain is the user-tier construct from ADR-0002: every
// user owns a set of (alias, endpoint, accessKey, secret) tuples
// pointing at S3-compatible backends. The CLI's "current region" is
// stored in the profile so subsequent buckets / objects commands
// implicitly target the right backend.
//
// Endpoints (all under /api/v1/user/regions, see internal/api/user_regions.go):
//
//	GET    /user/regions                     — list
//	POST   /user/regions                     — create
//	DELETE /user/regions/{regionId}          — delete
//
// We don't expose rotate / update here this cycle — the operator's
// rotation workflow normally happens in the web UI, and the CLI's
// minimal v1.8.0a surface is "stand up an automation runner" not
// "replace the full UI".
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// region is the wire shape returned by /user/regions endpoints —
// mirrors api.userRegionResponse. The CLI keeps its own struct so a
// future server-side rename doesn't break the build directly (we'd
// catch it in the e2e mock-server tests instead).
type region struct {
	ID              string    `json:"id"`
	UserID          string    `json:"userId"`
	Alias           string    `json:"alias"`
	Endpoint        string    `json:"endpoint"`
	Region          string    `json:"region"`
	AccessKeyID     string    `json:"accessKeyId"`
	AddressingStyle string    `json:"addressingStyle"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
	LastUsedAt      time.Time `json:"lastUsedAt,omitempty"`
}

func newRegionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "regions",
		Short: "Manage the user region keychain.",
		Long:  "Each region binds an alias, S3 endpoint, and access key. Subsequent buckets/objects commands target the region named by --region REGION_ID or the profile's current_region_id.",
	}
	cmd.AddCommand(newRegionsListCmd())
	cmd.AddCommand(newRegionsAddCmd())
	cmd.AddCommand(newRegionsDeleteCmd())
	return cmd
}

func newRegionsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all configured regions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := clientForProfile()
			if err != nil {
				return err
			}
			var out []region
			if err := client.GetJSON(context.Background(), "/user/regions", &out); err != nil {
				return err
			}
			if !outputTable() {
				return outputJSON(cmd.OutOrStdout(), out)
			}
			rows := make([][]string, 0, len(out))
			for _, r := range out {
				rows = append(rows, []string{r.ID, r.Alias, r.Endpoint, r.Region, r.AccessKeyID})
			}
			renderTable(cmd.OutOrStdout(), []string{"ID", "ALIAS", "ENDPOINT", "REGION", "ACCESS_KEY"}, rows)
			return nil
		},
	}
}

func newRegionsAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add ALIAS ENDPOINT KEY SECRET REGION",
		Short: "Register a new region with the keychain.",
		Long: `Adds a (alias, endpoint, accessKey, secret, region) tuple to the
authenticated user's keychain. The region's bucket access is gated by
whatever the S3 key itself can reach — basement doesn't impose an
additional permission layer on top of the backend.`,
		Args: cobra.ExactArgs(5),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := clientForProfile()
			if err != nil {
				return err
			}
			body := map[string]string{
				"alias":       args[0],
				"endpoint":    args[1],
				"accessKeyId": args[2],
				"secretKey":   args[3],
				"region":      args[4],
			}
			var out region
			if err := client.PostJSON(context.Background(), "/user/regions", body, &out); err != nil {
				return err
			}
			if !outputTable() {
				return outputJSON(cmd.OutOrStdout(), out)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added region %s (%s) at %s\n", out.ID, out.Alias, out.Endpoint)
			return nil
		},
	}
}

func newRegionsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete REGION_ID",
		Short: "Remove a region from the keychain.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := clientForProfile()
			if err != nil {
				return err
			}
			id := strings.TrimSpace(args[0])
			if err := client.DeleteJSON(context.Background(), "/user/regions/"+id, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted region %s\n", id)
			return nil
		},
	}
}
