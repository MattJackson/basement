//go:build integration

// Integration tests for the Garage v2 driver against a real Garage
// 2.0.0 container. Spun up + bootstrapped via internal/drivers/garagetest;
// run only under `go test -tags=integration` (Docker required).
//
// These tests are the regression net for the bug classes the v1.11.0.x
// sub-cycle fixed:
//   - v1.11.0.1 — admin-only driver (no S3 creds) must construct OK
//     and serve admin ops (ListBuckets, etc.) without an S3 client.
//   - v1.11.0.2 — bucket IDs round-trip end-to-end (this test asserts
//     CreateBucket returns the same ID the cluster records, which is
//     what the per-cluster API handlers depend on).
//   - v1.11.0.5 BUG02 — UpdateKeyPermissions + GetKey must surface the
//     grant. The bug was a silent-wire-shape decode error that zeroed
//     every grant on the readback path.
//
// Local-run: `make integration` (Docker required).
// CI: `.github/workflows/integration.yml`.

package garage

import (
	"context"
	"testing"

	driverpkg "github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/drivers/garagetest"
)

// TestIntegration_AdminOnlyDriver covers v1.11.0.1: an admin-only
// connection (admin_url + admin_token, no S3 creds) must build,
// advertise capabilities with Presign=false, and serve admin ops
// against the live cluster.
//
// The buggy v1.11.0 code path returned an error from newDriver when
// s3_endpoint was set but the access key was missing — and refused
// ListBuckets via DRIVER_BUILD_FAILED entirely. Post-fix, both must
// work cleanly.
func TestIntegration_AdminOnlyDriver(t *testing.T) {
	cluster := garagetest.Bootstrap(t, garagetest.V2)
	ctx := context.Background()

	d, err := newDriver(cluster.AdminConfig())
	if err != nil {
		t.Fatalf("newDriver(admin-only): %v", err)
	}

	caps, err := d.Capabilities(ctx)
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if caps.Driver != "garage" {
		t.Errorf("Capabilities().Driver = %q, want %q", caps.Driver, "garage")
	}
	// Admin-only drivers MUST advertise Presign=false so the UI doesn't
	// route data-plane ops to a missing S3 client.
	if caps.Presign {
		t.Errorf("admin-only driver advertised Presign=true; want false")
	}

	// ListBuckets is the v1.11.0.1 regression test: it MUST work on an
	// admin-only driver. Empty list is the expected initial state.
	buckets, err := d.ListBuckets(ctx)
	if err != nil {
		t.Fatalf("ListBuckets on admin-only driver: %v", err)
	}
	if len(buckets) != 0 {
		t.Errorf("fresh cluster ListBuckets returned %d buckets; want 0", len(buckets))
	}
}

// TestIntegration_BucketLifecycle exercises Create/List/Get/Delete
// against the live cluster + asserts the driver's claimed bucket ID
// matches what the admin API records for the alias. That round-trip
// is what the v1.11.0.2 per-cluster handler fix depends on (handlers
// look up buckets by ID, and the ID must agree between basement-ui's
// store and the cluster).
func TestIntegration_BucketLifecycle(t *testing.T) {
	cluster := garagetest.Bootstrap(t, garagetest.V2)
	ctx := context.Background()

	d, err := newDriver(cluster.AdminConfig())
	if err != nil {
		t.Fatalf("newDriver: %v", err)
	}

	alias := "lifecycle-test-bucket"

	// Create — driver returns its view of the new bucket.
	created, err := d.CreateBucket(ctx, driverpkg.BucketSpec{Alias: alias})
	if err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	if created.ID == "" {
		t.Fatal("CreateBucket returned empty ID")
	}

	// Cross-check: the cluster's own record for that alias must match
	// what the driver claimed. v1.11.0.2 root cause was misrouted IDs
	// at the API layer; if the driver itself lies here every downstream
	// per-cluster lookup is broken.
	directID, err := cluster.GetBucketDirect(ctx, alias)
	if err != nil {
		t.Fatalf("GetBucketDirect via admin: %v", err)
	}
	if directID != created.ID {
		t.Errorf("bucket ID mismatch: driver=%q cluster=%q", created.ID, directID)
	}

	// List — should now contain our bucket.
	list, err := d.ListBuckets(ctx)
	if err != nil {
		t.Fatalf("ListBuckets: %v", err)
	}
	found := false
	for _, b := range list {
		if b.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListBuckets did not return created bucket %q", created.ID)
	}

	// Get — should return the same record.
	got, err := d.GetBucket(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetBucket: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("GetBucket.ID = %q, want %q", got.ID, created.ID)
	}
	if len(got.Aliases) == 0 || got.Aliases[0] != alias {
		t.Errorf("GetBucket.Aliases = %v, want [%q]", got.Aliases, alias)
	}

	// Delete — fresh bucket has zero objects, so this should succeed.
	if err := d.DeleteBucket(ctx, created.ID); err != nil {
		t.Fatalf("DeleteBucket: %v", err)
	}

	after, err := d.ListBuckets(ctx)
	if err != nil {
		t.Fatalf("ListBuckets after delete: %v", err)
	}
	for _, b := range after {
		if b.ID == created.ID {
			t.Errorf("deleted bucket still in ListBuckets: %q", b.ID)
		}
	}
}

// TestIntegration_KeyLifecycle covers v1.11.0.5 BUG02: AllowBucketKey
// must succeed AND GetKey must surface the grant. The shipped bug was
// a quiet decode mismatch on GetBucketInfo / GetKeyInfo where the
// nested {permissions: {read, write, owner}} shape was being read as
// flat fields — every grant came back all-false on the readback even
// though the underlying cluster recorded them correctly.
//
// The test pattern:
//  1. CreateBucket + CreateKey via the driver
//  2. UpdateKeyPermissions to grant read+write
//  3. Driver's GetKey reports the grant
//  4. Cluster's direct GetKeyInfo agrees with the driver
//
// Step 3 is the regression — pre-fix code reported {false, false, false}.
// Step 4 catches any future bug that gets the readback wrong AGAIN by
// inverting the test (driver might lie consistently in both directions,
// but the cluster's view never lies).
func TestIntegration_KeyLifecycle(t *testing.T) {
	cluster := garagetest.Bootstrap(t, garagetest.V2)
	ctx := context.Background()

	d, err := newDriver(cluster.AdminConfig())
	if err != nil {
		t.Fatalf("newDriver: %v", err)
	}

	bucket, err := d.CreateBucket(ctx, driverpkg.BucketSpec{Alias: "key-test-bucket"})
	if err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}

	key, err := d.CreateKey(ctx, driverpkg.KeySpec{Name: "key-test-key"})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	if key.ID == "" {
		t.Fatal("CreateKey returned empty ID")
	}
	// Garage returns the secret exactly once at create time; the driver
	// MUST surface it (v0.x design constraint — UI's shown-once dialog
	// depends on this).
	if key.SecretAccessKey == nil || *key.SecretAccessKey == "" {
		t.Errorf("CreateKey did not surface SecretAccessKey on the response")
	}

	// Grant the key read+write on our bucket.
	perms := []driverpkg.BucketPermission{
		{BucketID: bucket.ID, Read: true, Write: true, Owner: false},
	}
	if err := d.UpdateKeyPermissions(ctx, key.ID, perms); err != nil {
		t.Fatalf("UpdateKeyPermissions: %v", err)
	}

	// Read back via the driver. This is the v1.11.0.5 BUG02 regression
	// assertion — pre-fix the driver returned all-false despite the
	// AllowBucketKey call succeeding.
	got, err := d.GetKey(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	var grant *driverpkg.KeyBucketAccess
	for i := range got.Buckets {
		if got.Buckets[i].BucketID == bucket.ID {
			grant = &got.Buckets[i]
			break
		}
	}
	if grant == nil {
		t.Fatalf("GetKey did not include grant for bucket %q (driver returned %d bucket grants)",
			bucket.ID, len(got.Buckets))
	}
	if !grant.Read {
		t.Errorf("grant.Read = false; want true (v1.11.0.5 BUG02 regression — driver dropped read on readback)")
	}
	if !grant.Write {
		t.Errorf("grant.Write = false; want true (v1.11.0.5 BUG02 regression — driver dropped write on readback)")
	}
	if grant.Owner {
		t.Errorf("grant.Owner = true; want false (we never granted Owner)")
	}

	// Cross-check against the cluster's own admin-side view. If the
	// driver lies consistently in both directions (a future-bug class
	// the BUG02 fix can't pre-empt), this catches it.
	directKey, err := cluster.GetKeyDirect(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetKeyDirect via admin: %v", err)
	}
	var directGrant *garagetest.BucketGrantDirect
	for i := range directKey.Buckets {
		if directKey.Buckets[i].BucketID == bucket.ID {
			directGrant = &directKey.Buckets[i]
			break
		}
	}
	if directGrant == nil {
		t.Fatalf("cluster's GetKeyInfo does not include grant for bucket %q", bucket.ID)
	}
	if directGrant.Read != grant.Read || directGrant.Write != grant.Write || directGrant.Owner != grant.Owner {
		t.Errorf("driver vs cluster grant mismatch: driver={R:%v W:%v O:%v} cluster={R:%v W:%v O:%v}",
			grant.Read, grant.Write, grant.Owner,
			directGrant.Read, directGrant.Write, directGrant.Owner)
	}

	// Cleanup: delete the key + bucket so a re-run inside the same
	// container (if the harness ever batches tests) starts clean.
	if err := d.DeleteKey(ctx, key.ID); err != nil {
		t.Errorf("DeleteKey cleanup: %v", err)
	}
	if err := d.DeleteBucket(ctx, bucket.ID); err != nil {
		t.Errorf("DeleteBucket cleanup: %v", err)
	}
}

// TestIntegration_HealthAndNodes hits the cluster-tier endpoints
// (HealthCheck + ListNodes) against a real bootstrapped cluster. Mostly
// a smoke test for the response decode path — the bootstrapper already
// proves the admin token works.
func TestIntegration_HealthAndNodes(t *testing.T) {
	cluster := garagetest.Bootstrap(t, garagetest.V2)
	ctx := context.Background()

	d, err := newDriver(cluster.AdminConfig())
	if err != nil {
		t.Fatalf("newDriver: %v", err)
	}

	health, err := d.HealthCheck(ctx)
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if health.Status == "" {
		t.Error("HealthCheck returned empty status")
	}

	nodes, err := d.ListNodes(ctx)
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("ListNodes returned %d nodes; want 1 (single-node test cluster)", len(nodes))
	}
}
