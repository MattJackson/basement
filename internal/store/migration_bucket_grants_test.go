package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMigrateBucketGrantsToUserRegions_DedupesAndMints verifies the
// happy path: two BucketGrants for the same (user, endpoint) collapse
// into a single UserRegion, and a grant for a different endpoint
// becomes its own UserRegion.
func TestMigrateBucketGrantsToUserRegions_DedupesAndMints(t *testing.T) {
	tmp := t.TempDir()
	secret := []byte("0123456789abcdef0123456789abcdef")

	conns, err := OpenConnectionsWithKey(tmp, secret)
	if err != nil {
		t.Fatalf("OpenConnectionsWithKey: %v", err)
	}
	ctx := context.Background()

	c1, err := conns.Create(ctx, Connection{
		Label:  "garage-prod",
		Driver: DriverGarageV1,
		Config: map[string]string{
			"s3_endpoint": "https://s3.example.com",
		},
	})
	if err != nil {
		t.Fatalf("conn1: %v", err)
	}
	c2, err := conns.Create(ctx, Connection{
		Label:  "garage-lab",
		Driver: DriverGarageV1,
		Config: map[string]string{
			"s3_endpoint": "http://10.0.0.5:3902",
		},
	})
	if err != nil {
		t.Fatalf("conn2: %v", err)
	}

	bg, err := OpenBucketGrants(tmp, secret)
	if err != nil {
		t.Fatalf("OpenBucketGrants: %v", err)
	}
	// Same (user, endpoint) twice — different buckets — should collapse.
	_, err = bg.Create(ctx, BucketGrantInput{
		UserID:       "matthew",
		ConnectionID: c1.ID,
		BucketID:     "lsi",
		AccessKeyID:  "GK_PROD",
		SecretKey:    "prod-secret",
	})
	if err != nil {
		t.Fatalf("grant1: %v", err)
	}
	_, err = bg.Create(ctx, BucketGrantInput{
		UserID:       "matthew",
		ConnectionID: c1.ID,
		BucketID:     "cheshire",
		AccessKeyID:  "GK_PROD",
		SecretKey:    "prod-secret",
	})
	if err != nil {
		t.Fatalf("grant2: %v", err)
	}
	// Different endpoint — separate UserRegion.
	_, err = bg.Create(ctx, BucketGrantInput{
		UserID:       "matthew",
		ConnectionID: c2.ID,
		BucketID:     "lab-bucket",
		AccessKeyID:  "GK_LAB",
		SecretKey:    "lab-secret",
	})
	if err != nil {
		t.Fatalf("grant3: %v", err)
	}

	st := &Store{dataDir: tmp}
	st.bucketGrants = bg
	ur, err := OpenUserRegions(tmp, secret)
	if err != nil {
		t.Fatalf("OpenUserRegions: %v", err)
	}
	st.userRegions = ur

	report, err := st.MigrateBucketGrantsToUserRegions(ctx, conns)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if report.Scanned != 3 {
		t.Errorf("scanned = %d, want 3", report.Scanned)
	}
	if report.Created != 2 {
		t.Errorf("created = %d, want 2 (one per unique endpoint)", report.Created)
	}
	if report.SkippedDuplicate != 0 {
		t.Errorf("skipped = %d, want 0 on first run", report.SkippedDuplicate)
	}
	if len(report.Failed) != 0 {
		t.Errorf("failed = %d (%v), want 0", len(report.Failed), report.Failed)
	}

	// Verify the regions land as expected.
	regions, err := ur.ListForUser(ctx, "matthew")
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(regions) != 2 {
		t.Fatalf("expected 2 regions for matthew, got %d (%+v)", len(regions), regions)
	}

	// All migrated regions get alias="migrated".
	for _, r := range regions {
		if r.Alias != "migrated" {
			t.Errorf("region %s: alias = %q, want migrated", r.ID, r.Alias)
		}
		// Endpoint normalized (no trailing slash, lowercase, etc.).
		if r.Endpoint == "" {
			t.Errorf("region %s: empty endpoint", r.ID)
		}
		// AccessKeyID carried through.
		if r.AccessKeyID == "" {
			t.Errorf("region %s: empty accessKeyId", r.ID)
		}
		// Secret re-encrypted under the same JWT secret — Decrypt
		// must recover the original plaintext.
		plain, err := ur.Decrypt(r)
		if err != nil {
			t.Errorf("decrypt %s: %v", r.ID, err)
			continue
		}
		want := "prod-secret"
		if r.Endpoint == "http://10.0.0.5:3902" {
			want = "lab-secret"
		}
		if plain != want {
			t.Errorf("region %s: secret = %q, want %q", r.ID, plain, want)
		}
	}
}

// TestMigrateBucketGrantsToUserRegions_Idempotent verifies a second
// run with no new grants leaves the store unchanged (skipped count
// matches the previous Created count).
func TestMigrateBucketGrantsToUserRegions_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	secret := []byte("0123456789abcdef0123456789abcdef")
	ctx := context.Background()

	conns, _ := OpenConnectionsWithKey(tmp, secret)
	c, _ := conns.Create(ctx, Connection{
		Label:  "garage-prod",
		Driver: DriverGarageV1,
		Config: map[string]string{"s3_endpoint": "https://s3.example.com"},
	})

	bg, _ := OpenBucketGrants(tmp, secret)
	_, _ = bg.Create(ctx, BucketGrantInput{
		UserID:       "matthew",
		ConnectionID: c.ID,
		BucketID:     "lsi",
		AccessKeyID:  "GK_PROD",
		SecretKey:    "prod-secret",
	})

	st := &Store{dataDir: tmp, bucketGrants: bg}
	ur, _ := OpenUserRegions(tmp, secret)
	st.userRegions = ur

	r1, err := st.MigrateBucketGrantsToUserRegions(ctx, conns)
	if err != nil {
		t.Fatalf("migrate1: %v", err)
	}
	if r1.Created != 1 {
		t.Errorf("first run: created = %d, want 1", r1.Created)
	}

	r2, err := st.MigrateBucketGrantsToUserRegions(ctx, conns)
	if err != nil {
		t.Fatalf("migrate2: %v", err)
	}
	if r2.Created != 0 {
		t.Errorf("second run: created = %d, want 0 (idempotent)", r2.Created)
	}
	if r2.SkippedDuplicate != 1 {
		t.Errorf("second run: skipped = %d, want 1", r2.SkippedDuplicate)
	}
	if len(r2.Failed) != 0 {
		t.Errorf("second run: failed = %d (%v), want 0", len(r2.Failed), r2.Failed)
	}

	// Region store should still hold exactly one row.
	all, _ := ur.ListForUser(ctx, "matthew")
	if len(all) != 1 {
		t.Errorf("after idempotent re-run: regions = %d, want 1", len(all))
	}
}

// TestMigrateBucketGrantsToUserRegions_NoFile is the fresh-install
// path: no bucket_grants.json exists, migration is a no-op.
func TestMigrateBucketGrantsToUserRegions_NoFile(t *testing.T) {
	tmp := t.TempDir()
	secret := []byte("0123456789abcdef0123456789abcdef")
	ctx := context.Background()

	conns, _ := OpenConnectionsWithKey(tmp, secret)
	bg, _ := OpenBucketGrants(tmp, secret)
	// Sanity: bucket_grants.json absent on disk — the store only
	// writes when Create is called.
	if _, err := os.Stat(filepath.Join(tmp, "bucket_grants.json")); !os.IsNotExist(err) {
		t.Fatalf("expected bucket_grants.json absent before any Create, stat err=%v", err)
	}

	st := &Store{dataDir: tmp, bucketGrants: bg}
	ur, _ := OpenUserRegions(tmp, secret)
	st.userRegions = ur

	report, err := st.MigrateBucketGrantsToUserRegions(ctx, conns)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if report.Scanned != 0 || report.Created != 0 {
		t.Errorf("expected empty report on fresh install, got %+v", report)
	}
}

// TestMigrateBucketGrantsToUserRegions_NewestSecretWins exercises
// the dedup-by-UpdatedAt rule: when the same (user, endpoint) has
// two grants with different secrets, the newest secret carries
// through.
func TestMigrateBucketGrantsToUserRegions_NewestSecretWins(t *testing.T) {
	tmp := t.TempDir()
	secret := []byte("0123456789abcdef0123456789abcdef")
	ctx := context.Background()

	conns, _ := OpenConnectionsWithKey(tmp, secret)
	c, _ := conns.Create(ctx, Connection{
		Label:  "garage-prod",
		Driver: DriverGarageV1,
		Config: map[string]string{"s3_endpoint": "https://s3.example.com"},
	})

	bg, _ := OpenBucketGrants(tmp, secret)
	older, _ := bg.Create(ctx, BucketGrantInput{
		UserID:       "matthew",
		ConnectionID: c.ID,
		BucketID:     "lsi",
		AccessKeyID:  "GK_OLD",
		SecretKey:    "old-secret",
	})
	// Force the second grant to have a newer UpdatedAt — easiest is
	// to Update it (which bumps UpdatedAt).
	_ = older
	newer, _ := bg.Create(ctx, BucketGrantInput{
		UserID:       "matthew",
		ConnectionID: c.ID,
		BucketID:     "cheshire",
		AccessKeyID:  "GK_NEW",
		SecretKey:    "new-secret",
	})
	// Ensure a measurable gap between rows for the dedup comparison.
	time.Sleep(10 * time.Millisecond)
	_, err := bg.Update(ctx, newer.ID, BucketGrantInput{SecretKey: "new-secret-bumped"})
	if err != nil {
		t.Fatalf("bump newer.UpdatedAt: %v", err)
	}

	st := &Store{dataDir: tmp, bucketGrants: bg}
	ur, _ := OpenUserRegions(tmp, secret)
	st.userRegions = ur

	if _, err := st.MigrateBucketGrantsToUserRegions(ctx, conns); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	regions, _ := ur.ListForUser(ctx, "matthew")
	if len(regions) != 1 {
		t.Fatalf("expected 1 region, got %d", len(regions))
	}
	plain, err := ur.Decrypt(regions[0])
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if plain != "new-secret-bumped" {
		t.Errorf("region secret = %q, want %q (newest by UpdatedAt)", plain, "new-secret-bumped")
	}
	if regions[0].AccessKeyID != "GK_NEW" {
		t.Errorf("accessKeyId = %q, want GK_NEW", regions[0].AccessKeyID)
	}
}

// TestMigrateBucketGrantsToUserRegions_UnwiredStores returns an
// error rather than silently corrupting state.
func TestMigrateBucketGrantsToUserRegions_UnwiredStores(t *testing.T) {
	tmp := t.TempDir()
	secret := []byte("0123456789abcdef0123456789abcdef")
	ctx := context.Background()
	conns, _ := OpenConnectionsWithKey(tmp, secret)

	// Both stores nil.
	st := &Store{dataDir: tmp}
	if _, err := st.MigrateBucketGrantsToUserRegions(ctx, conns); err == nil {
		t.Errorf("expected error when bucket-grants store unwired")
	}

	// Only bucket-grants wired.
	bg, _ := OpenBucketGrants(tmp, secret)
	st = &Store{dataDir: tmp, bucketGrants: bg}
	if _, err := st.MigrateBucketGrantsToUserRegions(ctx, conns); err == nil {
		t.Errorf("expected error when user-regions store unwired")
	}

	// Nil connections store.
	ur, _ := OpenUserRegions(tmp, secret)
	st = &Store{dataDir: tmp, bucketGrants: bg, userRegions: ur}
	if _, err := st.MigrateBucketGrantsToUserRegions(ctx, nil); err == nil {
		t.Errorf("expected error when conns is nil")
	}
}

// TestMigrateBucketGrantsToUserRegions_MissingConnection records
// the grant under Failed (no panic) when its ConnectionID points to
// a Connection that no longer exists.
func TestMigrateBucketGrantsToUserRegions_MissingConnection(t *testing.T) {
	tmp := t.TempDir()
	secret := []byte("0123456789abcdef0123456789abcdef")
	ctx := context.Background()

	conns, _ := OpenConnectionsWithKey(tmp, secret)
	// No connections created on purpose.

	bg, _ := OpenBucketGrants(tmp, secret)
	_, _ = bg.Create(ctx, BucketGrantInput{
		UserID:       "matthew",
		ConnectionID: "ghost-conn-id",
		BucketID:     "lsi",
		AccessKeyID:  "GK",
		SecretKey:    "S",
	})

	st := &Store{dataDir: tmp, bucketGrants: bg}
	ur, _ := OpenUserRegions(tmp, secret)
	st.userRegions = ur

	report, err := st.MigrateBucketGrantsToUserRegions(ctx, conns)
	if err != nil {
		t.Fatalf("migrate should not hard-error on orphan grant: %v", err)
	}
	if report.Created != 0 {
		t.Errorf("created = %d, want 0", report.Created)
	}
	if len(report.Failed) != 1 {
		t.Fatalf("failed = %d, want 1", len(report.Failed))
	}
	if report.Failed[0].Err == nil {
		t.Errorf("expected failure to carry non-nil err, got %+v", report.Failed[0])
	}
}
