package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// testKey is a 32-byte secret that satisfies the JWT min-length rule
// in production. The bucket-grant store derives its AES key via
// sha256, so any non-empty key works, but we mirror real config shape.
var testKey = []byte("01234567890123456789012345678901")

func newBucketGrants(t *testing.T) (BucketGrants, string) {
	t.Helper()
	dir := t.TempDir()
	g, err := OpenBucketGrants(dir, testKey)
	if err != nil {
		t.Fatalf("OpenBucketGrants: %v", err)
	}
	return g, dir
}

func sampleInput() BucketGrantInput {
	return BucketGrantInput{
		UserID:       "matthew",
		ConnectionID: "cluster-1",
		BucketID:     "lsi",
		AccessKeyID:  "GK1234567890",
		SecretKey:    "super-secret-plaintext",
	}
}

func TestBucketGrants_CreateGet(t *testing.T) {
	g, _ := newBucketGrants(t)
	ctx := context.Background()

	in := sampleInput()
	created, err := g.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.ID == "" {
		t.Fatal("Create returned empty ID")
	}
	if _, err := uuid.Parse(created.ID); err != nil {
		t.Errorf("ID is not a UUID: %v", err)
	}
	if created.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if created.UpdatedAt.IsZero() {
		t.Error("UpdatedAt is zero")
	}
	if created.UserID != in.UserID || created.ConnectionID != in.ConnectionID || created.BucketID != in.BucketID {
		t.Errorf("identity mismatch on Create: got %+v", created)
	}
	if created.AccessKeyID != in.AccessKeyID {
		t.Errorf("AccessKeyID mismatch: got %q want %q", created.AccessKeyID, in.AccessKeyID)
	}
	if len(created.SecretKeyEnc) == 0 {
		t.Error("SecretKeyEnc is empty after Create")
	}

	got, err := g.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("Get ID mismatch: got %q want %q", got.ID, created.ID)
	}
	if !bytes.Equal(got.SecretKeyEnc, created.SecretKeyEnc) {
		t.Error("SecretKeyEnc mismatch on Get")
	}
}

func TestBucketGrants_EncryptDecrypt(t *testing.T) {
	g, dir := newBucketGrants(t)
	ctx := context.Background()

	in := sampleInput()
	created, err := g.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Ciphertext must not equal plaintext.
	if bytes.Contains(created.SecretKeyEnc, []byte(in.SecretKey)) {
		t.Fatal("ciphertext contains plaintext bytes")
	}

	plain, err := g.Decrypt(created)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if plain != in.SecretKey {
		t.Errorf("Decrypt mismatch: got %q want %q", plain, in.SecretKey)
	}

	// Plaintext must not appear anywhere in the JSON on disk.
	raw, err := os.ReadFile(filepath.Join(dir, "bucket_grants.json"))
	if err != nil {
		t.Fatalf("read on-disk file: %v", err)
	}
	if bytes.Contains(raw, []byte(in.SecretKey)) {
		t.Fatalf("plaintext secret leaked to disk: %s", string(raw))
	}
}

func TestBucketGrants_GetByUserBucket(t *testing.T) {
	g, _ := newBucketGrants(t)
	ctx := context.Background()

	in := sampleInput()
	created, err := g.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := g.GetByUserBucket(ctx, in.UserID, in.ConnectionID, in.BucketID)
	if err != nil {
		t.Fatalf("GetByUserBucket: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("got %q want %q", got.ID, created.ID)
	}

	if _, err := g.GetByUserBucket(ctx, "other", in.ConnectionID, in.BucketID); !errors.Is(err, ErrBucketGrantNotFound) {
		t.Errorf("expected ErrBucketGrantNotFound, got %v", err)
	}
	if _, err := g.GetByUserBucket(ctx, in.UserID, "other", in.BucketID); !errors.Is(err, ErrBucketGrantNotFound) {
		t.Errorf("expected ErrBucketGrantNotFound (cluster mismatch), got %v", err)
	}
	if _, err := g.GetByUserBucket(ctx, in.UserID, in.ConnectionID, "other"); !errors.Is(err, ErrBucketGrantNotFound) {
		t.Errorf("expected ErrBucketGrantNotFound (bucket mismatch), got %v", err)
	}
}

func TestBucketGrants_UniquePerUserBucket(t *testing.T) {
	g, _ := newBucketGrants(t)
	ctx := context.Background()

	in := sampleInput()
	if _, err := g.Create(ctx, in); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	in2 := in
	in2.AccessKeyID = "GK-different-key"
	in2.SecretKey = "different-secret"
	_, err := g.Create(ctx, in2)
	if !errors.Is(err, ErrBucketGrantDuplicate) {
		t.Fatalf("expected ErrBucketGrantDuplicate, got %v", err)
	}

	// Different user on same bucket is allowed.
	in3 := in
	in3.UserID = "wife"
	if _, err := g.Create(ctx, in3); err != nil {
		t.Errorf("Create for different user should succeed: %v", err)
	}

	// Different bucket on same user is allowed.
	in4 := in
	in4.BucketID = "family-photos"
	if _, err := g.Create(ctx, in4); err != nil {
		t.Errorf("Create for different bucket should succeed: %v", err)
	}
}

func TestBucketGrants_Update_RotatesSecret(t *testing.T) {
	g, _ := newBucketGrants(t)
	ctx := context.Background()

	created, err := g.Create(ctx, sampleInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	origEnc := append([]byte(nil), created.SecretKeyEnc...)
	origUpdated := created.UpdatedAt

	// Tiny sleep-free wait: UpdatedAt uses time.Now().UTC() which has
	// monotonic resolution >> the work between these calls on any
	// modern machine, so the timestamp will differ. If it ever fails
	// flaky on a beefy CI box we'd switch to a fake clock.
	newSecret := "rotated-secret-value"
	updated, err := g.Update(ctx, created.ID, BucketGrantInput{SecretKey: newSecret})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if bytes.Equal(updated.SecretKeyEnc, origEnc) {
		t.Fatal("SecretKeyEnc did not change after rotation")
	}
	if !updated.UpdatedAt.After(origUpdated) && !updated.UpdatedAt.Equal(origUpdated) {
		t.Errorf("UpdatedAt regressed: %v -> %v", origUpdated, updated.UpdatedAt)
	}

	plain, err := g.Decrypt(updated)
	if err != nil {
		t.Fatalf("Decrypt after rotation: %v", err)
	}
	if plain != newSecret {
		t.Errorf("rotated plaintext mismatch: got %q want %q", plain, newSecret)
	}

	// Old ciphertext should no longer decrypt to anything meaningful — we
	// don't actually keep it, so just verify the in-store record is the
	// rotated one.
	persisted, err := g.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after rotation: %v", err)
	}
	if !bytes.Equal(persisted.SecretKeyEnc, updated.SecretKeyEnc) {
		t.Error("persisted SecretKeyEnc does not match rotated value")
	}
}

func TestBucketGrants_Update_AccessKeyOnly(t *testing.T) {
	g, _ := newBucketGrants(t)
	ctx := context.Background()

	created, err := g.Create(ctx, sampleInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	origEnc := append([]byte(nil), created.SecretKeyEnc...)

	updated, err := g.Update(ctx, created.ID, BucketGrantInput{AccessKeyID: "GK-renamed"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.AccessKeyID != "GK-renamed" {
		t.Errorf("AccessKeyID not updated: got %q", updated.AccessKeyID)
	}
	if !bytes.Equal(updated.SecretKeyEnc, origEnc) {
		t.Error("SecretKeyEnc should not change when only AccessKeyID rotates")
	}
}

func TestBucketGrants_Update_NotFound(t *testing.T) {
	g, _ := newBucketGrants(t)
	ctx := context.Background()
	_, err := g.Update(ctx, "no-such-id", BucketGrantInput{SecretKey: "x"})
	if !errors.Is(err, ErrBucketGrantNotFound) {
		t.Errorf("expected ErrBucketGrantNotFound, got %v", err)
	}
}

func TestBucketGrants_ListForUser(t *testing.T) {
	g, _ := newBucketGrants(t)
	ctx := context.Background()

	in := sampleInput()
	if _, err := g.Create(ctx, in); err != nil {
		t.Fatalf("Create matthew/lsi: %v", err)
	}

	in2 := in
	in2.BucketID = "family-photos"
	if _, err := g.Create(ctx, in2); err != nil {
		t.Fatalf("Create matthew/family-photos: %v", err)
	}

	in3 := in
	in3.UserID = "wife"
	if _, err := g.Create(ctx, in3); err != nil {
		t.Fatalf("Create wife/lsi: %v", err)
	}

	matthewGrants, err := g.ListForUser(ctx, "matthew")
	if err != nil {
		t.Fatalf("ListForUser matthew: %v", err)
	}
	if len(matthewGrants) != 2 {
		t.Errorf("matthew should have 2 grants, got %d", len(matthewGrants))
	}
	for _, gr := range matthewGrants {
		if gr.UserID != "matthew" {
			t.Errorf("ListForUser leaked grant for %q", gr.UserID)
		}
	}

	wifeGrants, err := g.ListForUser(ctx, "wife")
	if err != nil {
		t.Fatalf("ListForUser wife: %v", err)
	}
	if len(wifeGrants) != 1 {
		t.Errorf("wife should have 1 grant, got %d", len(wifeGrants))
	}

	none, err := g.ListForUser(ctx, "nobody")
	if err != nil {
		t.Fatalf("ListForUser nobody: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("nobody should have 0 grants, got %d", len(none))
	}
}

func TestBucketGrants_ListForBucket(t *testing.T) {
	g, _ := newBucketGrants(t)
	ctx := context.Background()

	in := sampleInput()
	if _, err := g.Create(ctx, in); err != nil {
		t.Fatalf("Create matthew/cluster-1/lsi: %v", err)
	}

	in2 := in
	in2.UserID = "wife"
	if _, err := g.Create(ctx, in2); err != nil {
		t.Fatalf("Create wife/cluster-1/lsi: %v", err)
	}

	// Different cluster, same bucket name — must not collide.
	in3 := in
	in3.ConnectionID = "cluster-2"
	if _, err := g.Create(ctx, in3); err != nil {
		t.Fatalf("Create matthew/cluster-2/lsi: %v", err)
	}

	bucketGrants, err := g.ListForBucket(ctx, in.ConnectionID, in.BucketID)
	if err != nil {
		t.Fatalf("ListForBucket: %v", err)
	}
	if len(bucketGrants) != 2 {
		t.Errorf("expected 2 grants for cluster-1/lsi, got %d", len(bucketGrants))
	}
	for _, gr := range bucketGrants {
		if gr.ConnectionID != in.ConnectionID || gr.BucketID != in.BucketID {
			t.Errorf("leaked grant: %+v", gr)
		}
	}
}

func TestBucketGrants_Delete(t *testing.T) {
	g, _ := newBucketGrants(t)
	ctx := context.Background()

	created, err := g.Create(ctx, sampleInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := g.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := g.Get(ctx, created.ID); !errors.Is(err, ErrBucketGrantNotFound) {
		t.Errorf("expected ErrBucketGrantNotFound after Delete, got %v", err)
	}

	if err := g.Delete(ctx, created.ID); !errors.Is(err, ErrBucketGrantNotFound) {
		t.Errorf("double Delete should return ErrBucketGrantNotFound, got %v", err)
	}
}

func TestBucketGrants_Persists(t *testing.T) {
	dir := t.TempDir()
	g, err := OpenBucketGrants(dir, testKey)
	if err != nil {
		t.Fatalf("OpenBucketGrants: %v", err)
	}
	ctx := context.Background()

	in := sampleInput()
	created, err := g.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Reopen with the same key.
	g2, err := OpenBucketGrants(dir, testKey)
	if err != nil {
		t.Fatalf("reopen OpenBucketGrants: %v", err)
	}

	got, err := g2.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	plain, err := g2.Decrypt(got)
	if err != nil {
		t.Fatalf("Decrypt after reopen: %v", err)
	}
	if plain != in.SecretKey {
		t.Errorf("Decrypt after reopen mismatch: got %q want %q", plain, in.SecretKey)
	}
}

func TestBucketGrants_Create_Validation(t *testing.T) {
	g, _ := newBucketGrants(t)
	ctx := context.Background()

	for _, tt := range []struct {
		name string
		mut  func(*BucketGrantInput)
	}{
		{"missing user", func(in *BucketGrantInput) { in.UserID = "" }},
		{"missing connection", func(in *BucketGrantInput) { in.ConnectionID = "" }},
		{"missing bucket", func(in *BucketGrantInput) { in.BucketID = "" }},
		{"missing access key", func(in *BucketGrantInput) { in.AccessKeyID = "" }},
		{"missing secret", func(in *BucketGrantInput) { in.SecretKey = "" }},
		{"whitespace user", func(in *BucketGrantInput) { in.UserID = "   " }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			in := sampleInput()
			tt.mut(&in)
			if _, err := g.Create(ctx, in); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestBucketGrants_OnDiskShape(t *testing.T) {
	dir := t.TempDir()
	g, err := OpenBucketGrants(dir, testKey)
	if err != nil {
		t.Fatalf("OpenBucketGrants: %v", err)
	}
	ctx := context.Background()

	if _, err := g.Create(ctx, sampleInput()); err != nil {
		t.Fatalf("Create: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "bucket_grants.json"))
	if err != nil {
		t.Fatalf("read disk: %v", err)
	}

	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("unmarshal disk JSON: %v", err)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 grant on disk, got %d", len(arr))
	}

	// Make sure the JSON has the expected field names and no
	// "secretKey" plaintext field accidentally introduced.
	for _, banned := range []string{"secretKey\"", "secret_key\""} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("on-disk JSON contains banned field substring %q", banned)
		}
	}
	if _, ok := arr[0]["secretKeyEnc"]; !ok {
		t.Errorf("on-disk JSON missing secretKeyEnc field; got: %v", arr[0])
	}
}
