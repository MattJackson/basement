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
	"time"

	"github.com/google/uuid"
)

func newUserRegions(t *testing.T) (UserRegions, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := OpenUserRegions(dir, testKey)
	if err != nil {
		t.Fatalf("OpenUserRegions: %v", err)
	}
	return r, dir
}

// sampleRegion returns a UserRegion shaped for Create — SecretKeyEnc
// carries the plaintext bytes per the Create convention.
func sampleRegion() UserRegion {
	return UserRegion{
		UserID:       "matthew",
		Alias:        "home",
		Endpoint:     "https://s3.pq.io",
		Region:       "garage",
		AccessKeyID:  "GK1234567890",
		SecretKeyEnc: []byte("super-secret-plaintext"),
	}
}

func TestUserRegions_CreateGet(t *testing.T) {
	r, _ := newUserRegions(t)
	ctx := context.Background()

	in := sampleRegion()
	created, err := r.Create(ctx, in)
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
	if created.UserID != in.UserID {
		t.Errorf("UserID mismatch: got %q want %q", created.UserID, in.UserID)
	}
	if created.Alias != in.Alias {
		t.Errorf("Alias mismatch: got %q want %q", created.Alias, in.Alias)
	}
	if created.Endpoint != "https://s3.pq.io" {
		t.Errorf("Endpoint canonicalization unexpected: got %q", created.Endpoint)
	}
	if created.Region != in.Region {
		t.Errorf("Region mismatch: got %q want %q", created.Region, in.Region)
	}
	if created.AccessKeyID != in.AccessKeyID {
		t.Errorf("AccessKeyID mismatch: got %q want %q", created.AccessKeyID, in.AccessKeyID)
	}
	if len(created.SecretKeyEnc) == 0 {
		t.Error("SecretKeyEnc is empty after Create")
	}

	got, err := r.Get(ctx, created.ID)
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

func TestUserRegions_DefaultRegionLabel(t *testing.T) {
	r, _ := newUserRegions(t)
	ctx := context.Background()

	in := sampleRegion()
	in.Region = ""
	created, err := r.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Region != "us-east-1" {
		t.Errorf("default Region label: got %q want %q", created.Region, "us-east-1")
	}
}

func TestUserRegions_EncryptDecrypt(t *testing.T) {
	r, dir := newUserRegions(t)
	ctx := context.Background()

	in := sampleRegion()
	plaintext := string(in.SecretKeyEnc)
	created, err := r.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if bytes.Contains(created.SecretKeyEnc, []byte(plaintext)) {
		t.Fatal("ciphertext contains plaintext bytes")
	}

	plain, err := r.Decrypt(created)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if plain != plaintext {
		t.Errorf("Decrypt mismatch: got %q want %q", plain, plaintext)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "user_regions.json"))
	if err != nil {
		t.Fatalf("read on-disk file: %v", err)
	}
	if bytes.Contains(raw, []byte(plaintext)) {
		t.Fatalf("plaintext secret leaked to disk: %s", string(raw))
	}
}

func TestUserRegions_NormalizeEndpoint(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"plain https", "https://s3.pq.io", "https://s3.pq.io", true},
		{"trailing slash", "https://s3.pq.io/", "https://s3.pq.io", true},
		{"uppercase host", "https://S3.PQ.IO", "https://s3.pq.io", true},
		{"mixed-case host + trailing slash", "https://S3.PQ.IO/", "https://s3.pq.io", true},
		{"default https port", "https://s3.pq.io:443", "https://s3.pq.io", true},
		{"default https port + trailing slash", "https://s3.pq.io:443/", "https://s3.pq.io", true},
		{"uppercase scheme", "HTTPS://s3.pq.io", "https://s3.pq.io", true},
		{"http default port", "http://s3.local:80", "http://s3.local", true},
		{"http preserved vs https", "http://s3.pq.io", "http://s3.pq.io", true},
		{"non-default port preserved", "https://s3.pq.io:9000", "https://s3.pq.io:9000", true},
		{"whitespace trimmed", "  https://s3.pq.io  ", "https://s3.pq.io", true},

		{"empty", "", "", false},
		{"whitespace only", "   ", "", false},
		{"missing scheme", "s3.pq.io", "", false},
		{"unsupported scheme", "ftp://s3.pq.io", "", false},
		{"missing host", "https://", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeEndpoint(tt.in)
			if tt.ok {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tt.want {
					t.Errorf("got %q want %q", got, tt.want)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
			}
		})
	}
}

// TestUserRegions_AllowMultipleKeysPerEndpoint: v1.2.0d's product
// shift — each access key is the primary user noun. A user can
// register two UserRegions against the same endpoint as long as the
// aliases differ ("Work S3" + "Personal S3").
func TestUserRegions_AllowMultipleKeysPerEndpoint(t *testing.T) {
	r, _ := newUserRegions(t)
	ctx := context.Background()

	first := sampleRegion()
	first.Alias = "home"
	if _, err := r.Create(ctx, first); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	// Same user, same endpoint, DIFFERENT alias — allowed post-v1.2.0d.
	second := sampleRegion()
	second.Alias = "work"
	second.AccessKeyID = "GK_WORK_KEY"
	if _, err := r.Create(ctx, second); err != nil {
		t.Errorf("different alias at same endpoint should succeed: %v", err)
	}

	// Different user, same endpoint, same alias — still allowed (cross-user
	// isolation hasn't changed).
	other := sampleRegion()
	other.UserID = "wife"
	if _, err := r.Create(ctx, other); err != nil {
		t.Errorf("different user should succeed: %v", err)
	}

	// Same user, different endpoint — allowed.
	otherEndpoint := sampleRegion()
	otherEndpoint.Endpoint = "https://s3.amazonaws.com"
	if _, err := r.Create(ctx, otherEndpoint); err != nil {
		t.Errorf("different endpoint should succeed: %v", err)
	}

	// matthew should now have 3 rows (home, work, aws).
	rows, err := r.ListForUser(ctx, "matthew")
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("matthew should have 3 regions after multi-key adds, got %d", len(rows))
	}
}

// TestUserRegions_UniquePerUserEndpointAlias: same user + same
// canonicalized endpoint + same alias still errors. Stylistic variants
// of the endpoint URL collide too, because endpoints are canonicalized
// before the alias comparison.
func TestUserRegions_UniquePerUserEndpointAlias(t *testing.T) {
	r, _ := newUserRegions(t)
	ctx := context.Background()

	first := sampleRegion()
	first.Alias = "home"
	if _, err := r.Create(ctx, first); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	// Same alias, same endpoint — collides.
	dup := sampleRegion()
	dup.Alias = "home"
	if _, err := r.Create(ctx, dup); !errors.Is(err, ErrUserRegionDuplicate) {
		t.Fatalf("expected ErrUserRegionDuplicate for same alias, got %v", err)
	}

	// Same alias, endpoint expressed with stylistic noise — still
	// collides because NormalizeEndpoint folds them.
	stylistic := sampleRegion()
	stylistic.Alias = "home"
	stylistic.Endpoint = "https://S3.PQ.IO:443/"
	if _, err := r.Create(ctx, stylistic); !errors.Is(err, ErrUserRegionDuplicate) {
		t.Fatalf("expected ErrUserRegionDuplicate after canonicalization, got %v", err)
	}

	// Different alias — allowed (covered by the sibling test but
	// re-asserted here so this test reads standalone).
	diff := sampleRegion()
	diff.Alias = "work"
	if _, err := r.Create(ctx, diff); err != nil {
		t.Errorf("different alias at same endpoint should succeed: %v", err)
	}
}

func TestUserRegions_GetByUserEndpoint(t *testing.T) {
	r, _ := newUserRegions(t)
	ctx := context.Background()

	in := sampleRegion()
	created, err := r.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Found via the exact canonical form.
	got, err := r.GetByUserEndpoint(ctx, in.UserID, "https://s3.pq.io")
	if err != nil {
		t.Fatalf("GetByUserEndpoint: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("got %q want %q", got.ID, created.ID)
	}

	// Found via a stylistic variant that canonicalizes to the same thing.
	got2, err := r.GetByUserEndpoint(ctx, in.UserID, "https://S3.PQ.IO:443/")
	if err != nil {
		t.Fatalf("GetByUserEndpoint (variant): %v", err)
	}
	if got2.ID != created.ID {
		t.Errorf("variant lookup got %q want %q", got2.ID, created.ID)
	}

	// Not found — different user.
	if _, err := r.GetByUserEndpoint(ctx, "nobody", "https://s3.pq.io"); !errors.Is(err, ErrUserRegionNotFound) {
		t.Errorf("expected ErrUserRegionNotFound, got %v", err)
	}

	// Not found — different endpoint.
	if _, err := r.GetByUserEndpoint(ctx, in.UserID, "https://s3.amazonaws.com"); !errors.Is(err, ErrUserRegionNotFound) {
		t.Errorf("expected ErrUserRegionNotFound, got %v", err)
	}
}

func TestUserRegions_Update_RotatesSecret(t *testing.T) {
	r, _ := newUserRegions(t)
	ctx := context.Background()

	created, err := r.Create(ctx, sampleRegion())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	origEnc := append([]byte(nil), created.SecretKeyEnc...)
	origUpdated := created.UpdatedAt

	newSecret := "rotated-secret-value"
	updated, err := r.Update(ctx, created.ID, UserRegion{SecretKeyEnc: []byte(newSecret)})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if bytes.Equal(updated.SecretKeyEnc, origEnc) {
		t.Fatal("SecretKeyEnc did not change after rotation")
	}
	if !updated.UpdatedAt.After(origUpdated) && !updated.UpdatedAt.Equal(origUpdated) {
		t.Errorf("UpdatedAt regressed: %v -> %v", origUpdated, updated.UpdatedAt)
	}

	plain, err := r.Decrypt(updated)
	if err != nil {
		t.Fatalf("Decrypt after rotation: %v", err)
	}
	if plain != newSecret {
		t.Errorf("rotated plaintext mismatch: got %q want %q", plain, newSecret)
	}

	persisted, err := r.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after rotation: %v", err)
	}
	if !bytes.Equal(persisted.SecretKeyEnc, updated.SecretKeyEnc) {
		t.Error("persisted SecretKeyEnc does not match rotated value")
	}
}

func TestUserRegions_Update_AliasOnly(t *testing.T) {
	r, _ := newUserRegions(t)
	ctx := context.Background()

	created, err := r.Create(ctx, sampleRegion())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	origEnc := append([]byte(nil), created.SecretKeyEnc...)

	updated, err := r.Update(ctx, created.ID, UserRegion{Alias: "renamed"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Alias != "renamed" {
		t.Errorf("Alias not updated: got %q", updated.Alias)
	}
	if !bytes.Equal(updated.SecretKeyEnc, origEnc) {
		t.Error("SecretKeyEnc should not change when only Alias updates")
	}
}

func TestUserRegions_Update_NotFound(t *testing.T) {
	r, _ := newUserRegions(t)
	ctx := context.Background()
	_, err := r.Update(ctx, "no-such-id", UserRegion{SecretKeyEnc: []byte("x")})
	if !errors.Is(err, ErrUserRegionNotFound) {
		t.Errorf("expected ErrUserRegionNotFound, got %v", err)
	}
}

func TestUserRegions_ListForUser(t *testing.T) {
	r, _ := newUserRegions(t)
	ctx := context.Background()

	a := sampleRegion()
	a.Endpoint = "https://s3.pq.io"
	if _, err := r.Create(ctx, a); err != nil {
		t.Fatalf("Create matthew/pq: %v", err)
	}

	b := sampleRegion()
	b.Endpoint = "https://s3.amazonaws.com"
	if _, err := r.Create(ctx, b); err != nil {
		t.Fatalf("Create matthew/aws: %v", err)
	}

	c := sampleRegion()
	c.UserID = "wife"
	c.Endpoint = "https://s3.pq.io"
	if _, err := r.Create(ctx, c); err != nil {
		t.Fatalf("Create wife/pq: %v", err)
	}

	matthewRows, err := r.ListForUser(ctx, "matthew")
	if err != nil {
		t.Fatalf("ListForUser matthew: %v", err)
	}
	if len(matthewRows) != 2 {
		t.Errorf("matthew should have 2 regions, got %d", len(matthewRows))
	}
	for _, row := range matthewRows {
		if row.UserID != "matthew" {
			t.Errorf("ListForUser leaked row for %q", row.UserID)
		}
	}

	wifeRows, err := r.ListForUser(ctx, "wife")
	if err != nil {
		t.Fatalf("ListForUser wife: %v", err)
	}
	if len(wifeRows) != 1 {
		t.Errorf("wife should have 1 region, got %d", len(wifeRows))
	}

	none, err := r.ListForUser(ctx, "nobody")
	if err != nil {
		t.Fatalf("ListForUser nobody: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("nobody should have 0 regions, got %d", len(none))
	}
}

func TestUserRegions_Delete(t *testing.T) {
	r, _ := newUserRegions(t)
	ctx := context.Background()

	created, err := r.Create(ctx, sampleRegion())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := r.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := r.Get(ctx, created.ID); !errors.Is(err, ErrUserRegionNotFound) {
		t.Errorf("expected ErrUserRegionNotFound after Delete, got %v", err)
	}

	if err := r.Delete(ctx, created.ID); !errors.Is(err, ErrUserRegionNotFound) {
		t.Errorf("double Delete should return ErrUserRegionNotFound, got %v", err)
	}
}

func TestUserRegions_Persists(t *testing.T) {
	dir := t.TempDir()
	r, err := OpenUserRegions(dir, testKey)
	if err != nil {
		t.Fatalf("OpenUserRegions: %v", err)
	}
	ctx := context.Background()

	in := sampleRegion()
	plaintext := string(in.SecretKeyEnc)
	created, err := r.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	r2, err := OpenUserRegions(dir, testKey)
	if err != nil {
		t.Fatalf("reopen OpenUserRegions: %v", err)
	}

	got, err := r2.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	plain, err := r2.Decrypt(got)
	if err != nil {
		t.Fatalf("Decrypt after reopen: %v", err)
	}
	if plain != plaintext {
		t.Errorf("Decrypt after reopen mismatch: got %q want %q", plain, plaintext)
	}
}

func TestUserRegions_TouchLastUsed_Debounce(t *testing.T) {
	dir := t.TempDir()
	r, err := OpenUserRegions(dir, testKey)
	if err != nil {
		t.Fatalf("OpenUserRegions: %v", err)
	}
	ctx := context.Background()

	created, err := r.Create(ctx, sampleRegion())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	path := filepath.Join(dir, "user_regions.json")
	info0, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after Create: %v", err)
	}
	mt0 := info0.ModTime()

	// First touch — must persist (no prior touch tracked) and bump mtime.
	if err := r.TouchLastUsed(ctx, created.ID); err != nil {
		t.Fatalf("TouchLastUsed (first): %v", err)
	}

	// Burst of nine more touches — all should be debounced (no further
	// disk writes). mtime should NOT advance past the first-touch
	// write.
	info1, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after first touch: %v", err)
	}
	mt1 := info1.ModTime()
	if !mt1.After(mt0) && !mt1.Equal(mt0) {
		t.Fatalf("mtime regressed after first touch: %v -> %v", mt0, mt1)
	}

	for i := 0; i < 9; i++ {
		if err := r.TouchLastUsed(ctx, created.ID); err != nil {
			t.Fatalf("TouchLastUsed (burst %d): %v", i, err)
		}
	}

	info2, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after burst: %v", err)
	}
	if !info2.ModTime().Equal(mt1) {
		t.Errorf("expected mtime unchanged after debounced burst; mt1=%v mt2=%v", mt1, info2.ModTime())
	}

	// In-memory LastUsedAt should still advance every call even though
	// disk persistence is skipped — verify via Get.
	got, err := r.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastUsedAt.IsZero() {
		t.Error("LastUsedAt is zero after touches")
	}
	// And it shouldn't be in the future.
	if got.LastUsedAt.After(time.Now().UTC().Add(time.Second)) {
		t.Errorf("LastUsedAt is in the future: %v", got.LastUsedAt)
	}
}

func TestUserRegions_TouchLastUsed_NotFound(t *testing.T) {
	r, _ := newUserRegions(t)
	if err := r.TouchLastUsed(context.Background(), "no-such-id"); !errors.Is(err, ErrUserRegionNotFound) {
		t.Errorf("expected ErrUserRegionNotFound, got %v", err)
	}
}

// TestUserRegions_AddressingStyle_DefaultsToPath (v1.3.0c): empty
// AddressingStyle on Create coalesces to "path" — the backwards-compat
// guarantee. Every UserRegion persisted before v1.3.0c rehydrates as
// path-style without a backfill migration.
func TestUserRegions_AddressingStyle_DefaultsToPath(t *testing.T) {
	r, _ := newUserRegions(t)
	ctx := context.Background()

	in := sampleRegion()
	in.AddressingStyle = "" // explicit zero-value
	created, err := r.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.AddressingStyle != AddressingStylePath {
		t.Errorf("AddressingStyle = %q, want %q for unset input", created.AddressingStyle, AddressingStylePath)
	}

	// Read paths also normalize: Get / List / GetByUserEndpoint never
	// return a UserRegion with an empty AddressingStyle.
	got, err := r.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AddressingStyle != AddressingStylePath {
		t.Errorf("Get.AddressingStyle = %q, want %q", got.AddressingStyle, AddressingStylePath)
	}

	rows, err := r.ListForUser(ctx, in.UserID)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(rows) == 0 || rows[0].AddressingStyle != AddressingStylePath {
		t.Errorf("ListForUser row addressing style = %v, want %q", rows, AddressingStylePath)
	}
}

// TestUserRegions_AddressingStyle_HonorsVirtualHost (v1.3.0c): explicit
// "virtual_host" survives the round-trip through validation + the
// read-path defaulting helper.
func TestUserRegions_AddressingStyle_HonorsVirtualHost(t *testing.T) {
	r, _ := newUserRegions(t)
	ctx := context.Background()

	in := sampleRegion()
	in.AddressingStyle = AddressingStyleVirtualHost
	created, err := r.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.AddressingStyle != AddressingStyleVirtualHost {
		t.Errorf("AddressingStyle = %q, want %q", created.AddressingStyle, AddressingStyleVirtualHost)
	}

	got, err := r.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AddressingStyle != AddressingStyleVirtualHost {
		t.Errorf("Get.AddressingStyle = %q, want %q", got.AddressingStyle, AddressingStyleVirtualHost)
	}
}

// TestUserRegions_AddressingStyle_LegacyOnDiskJSON (v1.3.0c): a
// UserRegion row written to disk BEFORE the cycle (i.e. with no
// addressingStyle JSON field) rehydrates with the field set to "path".
// Backwards-compat regression guard — no migration required.
func TestUserRegions_AddressingStyle_LegacyOnDiskJSON(t *testing.T) {
	dir := t.TempDir()
	// Hand-craft a v1.2.x-shaped JSON row (no addressingStyle field).
	// We don't bother with encryption-correct SecretKeyEnc bytes
	// because Decrypt isn't exercised on this path; the read-default
	// helper is.
	legacy := `[{"id":"legacy-1","userId":"matthew","alias":"home","endpoint":"https://s3.pq.io","region":"garage","accessKeyId":"GK_LEGACY","secretKeyEnc":"AAAA","createdAt":"2026-05-01T00:00:00Z","updatedAt":"2026-05-01T00:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(dir, "user_regions.json"), []byte(legacy), 0o600); err != nil {
		t.Fatalf("seed legacy JSON: %v", err)
	}

	r, err := OpenUserRegions(dir, testKey)
	if err != nil {
		t.Fatalf("OpenUserRegions: %v", err)
	}

	got, err := r.Get(context.Background(), "legacy-1")
	if err != nil {
		t.Fatalf("Get legacy: %v", err)
	}
	if got.AddressingStyle != AddressingStylePath {
		t.Errorf("legacy row AddressingStyle = %q, want %q", got.AddressingStyle, AddressingStylePath)
	}

	rows, err := r.ListForUser(context.Background(), "matthew")
	if err != nil {
		t.Fatalf("ListForUser legacy: %v", err)
	}
	if len(rows) != 1 || rows[0].AddressingStyle != AddressingStylePath {
		t.Errorf("ListForUser legacy = %+v, want one row with addressingStyle=path", rows)
	}
}

// TestUserRegions_AddressingStyle_UnknownValueFallsBackToPath: a
// non-canonical value passed in (e.g. typo, future style we don't yet
// recognise) coalesces to "path" so the registered region produces a
// working S3 client regardless.
func TestUserRegions_AddressingStyle_UnknownValueFallsBackToPath(t *testing.T) {
	r, _ := newUserRegions(t)
	ctx := context.Background()

	in := sampleRegion()
	in.AddressingStyle = "global_endpoint" // not yet a thing
	created, err := r.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.AddressingStyle != AddressingStylePath {
		t.Errorf("unknown style should coalesce to path, got %q", created.AddressingStyle)
	}
}

func TestUserRegions_Create_Validation(t *testing.T) {
	r, _ := newUserRegions(t)
	ctx := context.Background()

	for _, tt := range []struct {
		name string
		mut  func(*UserRegion)
	}{
		{"missing user", func(in *UserRegion) { in.UserID = "" }},
		{"missing access key", func(in *UserRegion) { in.AccessKeyID = "" }},
		{"missing secret", func(in *UserRegion) { in.SecretKeyEnc = nil }},
		{"missing endpoint", func(in *UserRegion) { in.Endpoint = "" }},
		{"bad endpoint scheme", func(in *UserRegion) { in.Endpoint = "ftp://x" }},
		{"endpoint missing scheme", func(in *UserRegion) { in.Endpoint = "s3.pq.io" }},
		{"whitespace user", func(in *UserRegion) { in.UserID = "   " }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			in := sampleRegion()
			tt.mut(&in)
			if _, err := r.Create(ctx, in); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestUserRegions_OnDiskShape(t *testing.T) {
	dir := t.TempDir()
	r, err := OpenUserRegions(dir, testKey)
	if err != nil {
		t.Fatalf("OpenUserRegions: %v", err)
	}
	ctx := context.Background()

	if _, err := r.Create(ctx, sampleRegion()); err != nil {
		t.Fatalf("Create: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "user_regions.json"))
	if err != nil {
		t.Fatalf("read disk: %v", err)
	}

	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("unmarshal disk JSON: %v", err)
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 region on disk, got %d", len(arr))
	}

	for _, banned := range []string{"secretKey\"", "secret_key\""} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("on-disk JSON contains banned field substring %q", banned)
		}
	}
	if _, ok := arr[0]["secretKeyEnc"]; !ok {
		t.Errorf("on-disk JSON missing secretKeyEnc field; got: %v", arr[0])
	}
	if _, ok := arr[0]["endpoint"]; !ok {
		t.Errorf("on-disk JSON missing endpoint field; got: %v", arr[0])
	}
}
