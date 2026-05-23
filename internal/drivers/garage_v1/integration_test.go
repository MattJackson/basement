//go:build integration

// Integration tests for the Garage v1 driver against a real Garage
// 1.0.1 container. Spun up + bootstrapped via internal/drivers/garagetest;
// run only under `go test -tags=integration` (Docker required).
//
// Driver-parity doctrine: every regression test the v2 driver carries
// also runs against v1 so a bug class can never silently re-surface in
// the older code path. The v1 admin API has different endpoint paths
// (/v1/* vs /v2/*) but the same conceptual surface, so the test shape
// mirrors internal/drivers/garage/driver_integration_test.go intentionally.
//
// Bug-class coverage (v1.11.0.8 cycle):
//   - v1.11.0.1 — admin-only driver builds + serves admin ops
//   - v1.11.0.2 — bucket ID round-trip
//   - v1.11.0.5 BUG02 — UpdateKeyPermissions + GetKey surfaces grants

package garage_v1

import (
	"context"
	"testing"

	driverpkg "github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/drivers/garagetest"
)

// TestIntegration_AdminOnlyDriver_V1 covers v1.11.0.1 for the v1
// driver. The v1 driver always had the correct admin-only gate (see
// internal/drivers/garage_v1/garage.go) — this test pins the
// behaviour so future refactors can't regress it back to the v2-style
// bug.
func TestIntegration_AdminOnlyDriver_V1(t *testing.T) {
	cluster := garagetest.Bootstrap(t, garagetest.V1)
	ctx := context.Background()

	d, err := newDriver(cluster.AdminConfig())
	if err != nil {
		t.Fatalf("newDriver(admin-only): %v", err)
	}

	caps, err := d.Capabilities(ctx)
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if caps.Driver != "garage-v1" {
		t.Errorf("Capabilities().Driver = %q, want %q", caps.Driver, "garage-v1")
	}
	if caps.Presign {
		t.Errorf("admin-only v1 driver advertised Presign=true; want false")
	}

	buckets, err := d.ListBuckets(ctx)
	if err != nil {
		t.Fatalf("ListBuckets on admin-only v1 driver: %v", err)
	}
	if len(buckets) != 0 {
		t.Errorf("fresh v1 cluster ListBuckets returned %d buckets; want 0", len(buckets))
	}
}

// TestIntegration_BucketLifecycle_V1 mirrors the v2 driver's bucket
// lifecycle assertion against the v1 admin API endpoints
// (/v1/bucket?id=..., PUT /v1/bucket, DELETE /v1/bucket?id=...).
func TestIntegration_BucketLifecycle_V1(t *testing.T) {
	cluster := garagetest.Bootstrap(t, garagetest.V1)
	ctx := context.Background()

	d, err := newDriver(cluster.AdminConfig())
	if err != nil {
		t.Fatalf("newDriver: %v", err)
	}

	alias := "lifecycle-test-bucket-v1"

	created, err := d.CreateBucket(ctx, driverpkg.BucketSpec{Alias: alias})
	if err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	if created.ID == "" {
		t.Fatal("CreateBucket returned empty ID")
	}

	directID, err := cluster.GetBucketDirect(ctx, alias)
	if err != nil {
		t.Fatalf("GetBucketDirect via admin: %v", err)
	}
	if directID != created.ID {
		t.Errorf("v1 bucket ID mismatch: driver=%q cluster=%q", created.ID, directID)
	}

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
		t.Errorf("ListBuckets did not return created v1 bucket %q", created.ID)
	}

	got, err := d.GetBucket(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetBucket: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("GetBucket.ID = %q, want %q", got.ID, created.ID)
	}

	if err := d.DeleteBucket(ctx, created.ID); err != nil {
		t.Fatalf("DeleteBucket: %v", err)
	}
}

// TestIntegration_KeyLifecycle_V1 mirrors the BUG02 regression against
// the v1 admin API. v1 uses /v1/bucket/allow + /v1/bucket/deny with
// the same {permissions: {read, write, owner}} nested shape as v2
// (garage-admin-v1.yml KeyInfo § 1228-1276 — same as v2 since they
// share the same upstream wire types). A future refactor that touches
// v1's keyFromInfo could repeat the v2 BUG02 mistake; this pins it.
func TestIntegration_KeyLifecycle_V1(t *testing.T) {
	cluster := garagetest.Bootstrap(t, garagetest.V1)
	ctx := context.Background()

	d, err := newDriver(cluster.AdminConfig())
	if err != nil {
		t.Fatalf("newDriver: %v", err)
	}

	bucket, err := d.CreateBucket(ctx, driverpkg.BucketSpec{Alias: "key-test-bucket-v1"})
	if err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}

	key, err := d.CreateKey(ctx, driverpkg.KeySpec{Name: "key-test-key-v1"})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	if key.SecretAccessKey == nil || *key.SecretAccessKey == "" {
		t.Errorf("v1 CreateKey did not surface SecretAccessKey")
	}

	perms := []driverpkg.BucketPermission{
		{BucketID: bucket.ID, Read: true, Write: true, Owner: false},
	}
	if err := d.UpdateKeyPermissions(ctx, key.ID, perms); err != nil {
		t.Fatalf("UpdateKeyPermissions: %v", err)
	}

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
		t.Fatalf("v1 GetKey did not include grant for bucket %q", bucket.ID)
	}
	if !grant.Read || !grant.Write {
		t.Errorf("v1 grant readback = {R:%v W:%v}; want both true (BUG02-class regression)",
			grant.Read, grant.Write)
	}
	if grant.Owner {
		t.Errorf("v1 grant.Owner = true; want false")
	}

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
		t.Fatalf("v1 cluster GetKeyInfo missing grant for bucket %q", bucket.ID)
	}
	if directGrant.Read != grant.Read || directGrant.Write != grant.Write {
		t.Errorf("v1 driver vs cluster mismatch: driver={R:%v W:%v} cluster={R:%v W:%v}",
			grant.Read, grant.Write, directGrant.Read, directGrant.Write)
	}

	if err := d.DeleteKey(ctx, key.ID); err != nil {
		t.Errorf("DeleteKey cleanup: %v", err)
	}
	if err := d.DeleteBucket(ctx, bucket.ID); err != nil {
		t.Errorf("DeleteBucket cleanup: %v", err)
	}
}

// TestIntegration_HealthAndNodes_V1 smoke test for the v1 cluster
// endpoints: /v1/health (mapped through HealthCheck) and /v1/status
// (mapped through ListNodes).
func TestIntegration_HealthAndNodes_V1(t *testing.T) {
	cluster := garagetest.Bootstrap(t, garagetest.V1)
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
		t.Error("v1 HealthCheck returned empty status")
	}

	nodes, err := d.ListNodes(ctx)
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("v1 ListNodes returned %d nodes; want 1", len(nodes))
	}
}
