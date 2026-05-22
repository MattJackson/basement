// Package api: /admin/usage/overview — OBS.USAGE storage overview
// (cycle v0.9.0k).
//
// The current-state snapshot operators want when they open basement
// at 2am asking "where is my storage going?" — totals across clusters,
// per-cluster breakdown, and the top buckets by bytes / objects. No
// time series; metrics persistence is a v1.x concern.
//
// All numbers are aggregated from EXISTING per-cluster reads (the same
// ListBuckets fan-out admin_clusters.go uses for /admin/buckets) plus
// a single ListKeys per cluster. No new driver methods, no new fetch
// infrastructure, no metrics store. Per-cluster failures are swallowed
// into the response's perCluster entries with healthy=false so one bad
// cluster doesn't blank the whole dashboard.
package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// UsageTotals carries the top-of-page big-number stats.
type UsageTotals struct {
	Clusters int   `json:"clusters"`
	Buckets  int   `json:"buckets"`
	Keys     int   `json:"keys"`
	Bytes    int64 `json:"bytes"`
	Objects  int64 `json:"objects"`
	Grants   int   `json:"grants"`
}

// UsagePerCluster is one row of the per-cluster usage table. Healthy
// flips to false when the per-cluster fan-out for this connection
// failed (driver build error, list error, deadline) — the row stays
// in the response so the operator sees the cluster + the failure mode,
// not a silently-missing entry.
type UsagePerCluster struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Bytes   int64  `json:"bytes"`
	Objects int64  `json:"objects"`
	Buckets int    `json:"buckets"`
	Keys    int    `json:"keys"`
	Healthy bool   `json:"healthy"`
	Error   string `json:"error,omitempty"`
}

// UsageTopBucket is one entry in either top-N table. clusterId is kept
// alongside clusterLabel so the frontend row click can route to
// /admin/clusters/{cid}/buckets/{bid} without a second lookup.
type UsageTopBucket struct {
	ClusterID    string `json:"clusterId"`
	ClusterLabel string `json:"clusterLabel"`
	BucketID     string `json:"bucketId"`
	BucketAlias  string `json:"bucketAlias"`
	Bytes        int64  `json:"bytes"`
	Objects      int64  `json:"objects"`
}

// UsageOverviewResponse is the wire shape returned by
// GET /admin/usage/overview.
type UsageOverviewResponse struct {
	Totals              UsageTotals       `json:"totals"`
	PerCluster          []UsagePerCluster `json:"perCluster"`
	TopBucketsByBytes   []UsageTopBucket  `json:"topBucketsByBytes"`
	TopBucketsByObjects []UsageTopBucket  `json:"topBucketsByObjects"`
}

// usageTopN is how many rows the two top-bucket panels show. Tuned at
// 10 to match the brief and keep the response under ~5KB even with
// many clusters.
const usageTopN = 10

// getUsageOverviewHandler handles GET /api/v1/admin/usage/overview.
//
// Gated on host:manage_users at host:* — same coarse "is this a Host
// Admin?" check the user-management page uses. There's no dedicated
// "view usage" capability yet; reusing the existing one keeps the
// matrix small and matches the operator's intent ("any host admin
// should see the dashboard").
func (s *Server) getUsageOverviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	if _, ok := s.requireCapability(w, r, "host:manage_users", "host:*"); !ok {
		return
	}

	conns, err := s.conns.List(r.Context())
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to list connections")
		return
	}

	// Per-cluster fan-out. Same structure as listAllBucketsHandler:
	// bounded concurrency, per-cluster 3s deadline, errors swallowed
	// into the response. We collect ListBuckets + ListKeys back to
	// back per cluster (sequential within a single goroutine — they're
	// the same backend, so parallelising them buys nothing and just
	// doubles the worst-case in-flight count).
	type clusterResult struct {
		conn    store.Connection
		buckets []driver.Bucket
		keyCt   int
		err     error
	}

	results := make([]clusterResult, 0, len(conns))
	mu := sync.Mutex{}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	for _, conn := range conns {
		wg.Add(1)
		go func(conn store.Connection) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()

			drv, err := s.reg.For(ctx, conn.ID)
			if err != nil {
				mu.Lock()
				results = append(results, clusterResult{conn: conn, err: fmt.Errorf("building driver: %w", err)})
				mu.Unlock()
				return
			}

			buckets, err := drv.ListBuckets(ctx)
			if err != nil {
				mu.Lock()
				results = append(results, clusterResult{conn: conn, err: fmt.Errorf("listing buckets: %w", err)})
				mu.Unlock()
				return
			}

			keys, kerr := drv.ListKeys(ctx)
			keyCt := 0
			if kerr == nil {
				keyCt = len(keys)
			}
			// Key-listing failure isn't fatal for the dashboard; the
			// bucket numbers are the headline. We still record keyCt=0
			// rather than blocking the row.

			mu.Lock()
			results = append(results, clusterResult{conn: conn, buckets: buckets, keyCt: keyCt})
			mu.Unlock()
		}(conn)
	}
	wg.Wait()

	// Grant count retired with ADR-0002 v1.1.0e — the per-bucket
	// BucketGrants table is gone. The Totals.Grants field stays in the
	// wire shape (zero-valued) so OpenAPI consumers don't break; a
	// future cycle that surfaces UserRegions per cluster can repurpose
	// the field or rename it without another wire break.
	grantCt := 0

	// Build the per-cluster table + aggregate totals + top-N candidates.
	perCluster := make([]UsagePerCluster, 0, len(results))
	totals := UsageTotals{
		Clusters: len(conns),
		Grants:   grantCt,
	}
	allBuckets := make([]UsageTopBucket, 0)

	for _, r := range results {
		row := UsagePerCluster{
			ID:    r.conn.ID,
			Label: r.conn.Label,
		}
		if r.err != nil {
			row.Healthy = false
			row.Error = r.err.Error()
			perCluster = append(perCluster, row)
			continue
		}

		row.Healthy = true
		row.Buckets = len(r.buckets)
		row.Keys = r.keyCt

		var clusterBytes, clusterObjects int64
		for _, b := range r.buckets {
			clusterBytes += b.Bytes
			clusterObjects += b.Objects
			alias := ""
			if len(b.Aliases) > 0 {
				alias = b.Aliases[0]
			}
			allBuckets = append(allBuckets, UsageTopBucket{
				ClusterID:    r.conn.ID,
				ClusterLabel: r.conn.Label,
				BucketID:     b.ID,
				BucketAlias:  alias,
				Bytes:        b.Bytes,
				Objects:      b.Objects,
			})
		}
		row.Bytes = clusterBytes
		row.Objects = clusterObjects

		totals.Buckets += row.Buckets
		totals.Keys += row.Keys
		totals.Bytes += row.Bytes
		totals.Objects += row.Objects

		perCluster = append(perCluster, row)
	}

	// Stable per-cluster ordering by label so the table renders
	// consistently across refreshes (the fan-out goroutines append in
	// completion order, which is racy).
	sort.Slice(perCluster, func(i, j int) bool {
		return perCluster[i].Label < perCluster[j].Label
	})

	// Top-N by bytes.
	byBytes := make([]UsageTopBucket, len(allBuckets))
	copy(byBytes, allBuckets)
	sort.Slice(byBytes, func(i, j int) bool {
		return byBytes[i].Bytes > byBytes[j].Bytes
	})
	if len(byBytes) > usageTopN {
		byBytes = byBytes[:usageTopN]
	}

	// Top-N by object count.
	byObjects := make([]UsageTopBucket, len(allBuckets))
	copy(byObjects, allBuckets)
	sort.Slice(byObjects, func(i, j int) bool {
		return byObjects[i].Objects > byObjects[j].Objects
	})
	if len(byObjects) > usageTopN {
		byObjects = byObjects[:usageTopN]
	}

	writeJSON(w, http.StatusOK, UsageOverviewResponse{
		Totals:              totals,
		PerCluster:          perCluster,
		TopBucketsByBytes:   byBytes,
		TopBucketsByObjects: byObjects,
	})
}
