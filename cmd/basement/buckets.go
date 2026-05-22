// Package main — buckets.go implements `basement buckets list`.
//
// Endpoint:
//
//	GET /api/v1/user/regions/{regionId}/buckets — list buckets the
//	    region's key can reach. Returns {buckets: [], perBucketStatsAvailable}
//	    per api.userRegionBucketListResponse.
//
// We don't expose create / delete this cycle — bucket-create is a
// rare-enough op that the operator can do it via the UI; the CLI's
// v1.8.0a north star is "list + browse for automation".
package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

// bucketListItem is the wire shape returned in the buckets array of
// userRegionBucketListResponse — mirrors driver.Bucket. Capability +
// pagination metadata ride on the outer envelope.
type bucketListItem struct {
	ID                string    `json:"id"`
	Aliases           []string  `json:"aliases"`
	Created           time.Time `json:"created,omitempty"`
	Objects           int64     `json:"objects"`
	Bytes             int64     `json:"bytes"`
	UnfinishedUploads int64     `json:"unfinishedUploads"`
}

type bucketListResponse struct {
	Buckets                 []bucketListItem `json:"buckets"`
	PerBucketStatsAvailable bool             `json:"perBucketStatsAvailable"`
}

func newBucketsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "buckets",
		Short: "Browse buckets in the current region.",
	}
	cmd.AddCommand(newBucketsListCmd())
	return cmd
}

func newBucketsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List buckets in the current region.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, p, err := clientForProfile()
			if err != nil {
				return err
			}
			rid, err := regionID(p)
			if err != nil {
				return err
			}
			var out bucketListResponse
			if err := client.GetJSON(context.Background(), "/user/regions/"+rid+"/buckets", &out); err != nil {
				return err
			}
			if !outputTable() {
				return outputJSON(cmd.OutOrStdout(), out)
			}
			// Stats columns are only meaningful when the backend
			// exposes per-bucket counters (S3, MinIO, Garage v2). For
			// Garage v1 we render em-dashes in those cells so the
			// width stays predictable.
			rows := make([][]string, 0, len(out.Buckets))
			for _, b := range out.Buckets {
				name := b.ID
				if len(b.Aliases) > 0 {
					name = b.Aliases[0]
				}
				objs, size := "—", "—"
				if out.PerBucketStatsAvailable {
					objs = strconv.FormatInt(b.Objects, 10)
					size = strconv.FormatInt(b.Bytes, 10)
				}
				rows = append(rows, []string{name, b.ID, objs, size})
			}
			renderTable(cmd.OutOrStdout(), []string{"NAME", "ID", "OBJECTS", "BYTES"}, rows)
			if !out.PerBucketStatsAvailable {
				fmt.Fprintln(cmd.OutOrStdout(), "(per-bucket stats unavailable for this driver)")
			}
			return nil
		},
	}
}
