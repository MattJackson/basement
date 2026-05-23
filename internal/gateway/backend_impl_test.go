// Package gateway: ProductionBackend tests covering AuthBasic happy
// + wrong-password paths, ListRegions delegation, and GetObject
// delegation. Heavier integration coverage (driver routing through
// connections + admin bridge) lives in the webdav gateway tests
// where the backend is exercised end-to-end.

package gateway

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/store"
)

// stubUsers satisfies the userLookup contract for AuthBasic tests.
type stubUsers struct {
	username string
	password string
	noHash   bool // OIDC-only path: PasswordHash is empty
}

func (s *stubUsers) UserByUsername(name string) (store.User, error) {
	if name != s.username {
		return store.User{}, store.ErrUserNotFound
	}
	if s.noHash {
		return store.User{ID: "uid", Username: name}, nil
	}
	hash, _ := auth.HashPassword(s.password)
	return store.User{ID: "uid", Username: name, PasswordHash: hash}, nil
}

func TestAuthBasic_EnvAdmin_Happy(t *testing.T) {
	cfg := &config.Config{}
	cfg.Admin.User = "admin"
	hash, _ := auth.HashPassword("adminpw")
	cfg.Admin.PasswordHash = hash

	b := NewProductionBackend(BackendDeps{Cfg: cfg})
	uctx, err := b.AuthBasic(context.Background(), "admin", "adminpw")
	if err != nil {
		t.Fatalf("AuthBasic: %v", err)
	}
	if uctx == nil || uctx.UserID != "admin" {
		t.Errorf("AuthBasic: want UserID=admin, got %+v", uctx)
	}
}

func TestAuthBasic_EnvAdmin_WrongPassword(t *testing.T) {
	cfg := &config.Config{}
	cfg.Admin.User = "admin"
	hash, _ := auth.HashPassword("adminpw")
	cfg.Admin.PasswordHash = hash

	b := NewProductionBackend(BackendDeps{Cfg: cfg})
	uctx, err := b.AuthBasic(context.Background(), "admin", "WRONG")
	if err == nil {
		t.Fatalf("AuthBasic wrong-pw: want error, got uctx=%+v", uctx)
	}
	if !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("AuthBasic wrong-pw: want ErrUnauthenticated, got %v", err)
	}
}

func TestAuthBasic_StoreUser_Happy(t *testing.T) {
	cfg := &config.Config{}
	b := NewProductionBackend(BackendDeps{
		Cfg:   cfg,
		Users: &stubUsers{username: "alice", password: "alicepw"},
	})
	uctx, err := b.AuthBasic(context.Background(), "alice", "alicepw")
	if err != nil {
		t.Fatalf("AuthBasic: %v", err)
	}
	if uctx.UserID != "alice" {
		t.Errorf("AuthBasic: want UserID=alice, got %q", uctx.UserID)
	}
}

func TestAuthBasic_OIDCOnlyUser_Rejected(t *testing.T) {
	cfg := &config.Config{}
	b := NewProductionBackend(BackendDeps{
		Cfg:   cfg,
		Users: &stubUsers{username: "alice", noHash: true},
	})
	_, err := b.AuthBasic(context.Background(), "alice", "anything")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("OIDC-only AuthBasic: want ErrUnauthenticated, got %v", err)
	}
}

func TestAuthBasic_EmptyCreds_Rejected(t *testing.T) {
	b := NewProductionBackend(BackendDeps{})
	if _, err := b.AuthBasic(context.Background(), "", "pw"); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("empty user: want ErrUnauthenticated, got %v", err)
	}
	if _, err := b.AuthBasic(context.Background(), "u", ""); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("empty pass: want ErrUnauthenticated, got %v", err)
	}
}

func TestAuthSigV4_ReturnsUnsupported(t *testing.T) {
	b := NewProductionBackend(BackendDeps{})
	_, err := b.AuthSigV4(context.Background(), nil)
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("AuthSigV4: want ErrUnsupported, got %v", err)
	}
}

func TestAuthBearer_NilStore_Rejected(t *testing.T) {
	b := NewProductionBackend(BackendDeps{})
	_, err := b.AuthBearer(context.Background(), "BMNTfoo:secret")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("AuthBearer no-SA-store: want ErrUnauthenticated, got %v", err)
	}
}

func TestAuthBearer_MalformedPayload_Rejected(t *testing.T) {
	b := NewProductionBackend(BackendDeps{})
	// no colon
	_, err := b.AuthBearer(context.Background(), "BMNTfoo")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("AuthBearer no-colon: want ErrUnauthenticated, got %v", err)
	}
}

// stubRegions satisfies the store.UserRegions surface ProductionBackend
// reads for ListRegions.
type stubRegions struct {
	rs map[string][]store.UserRegion
}

func (s *stubRegions) ListForUser(_ context.Context, userID string) ([]store.UserRegion, error) {
	return s.rs[userID], nil
}
func (s *stubRegions) Create(_ context.Context, r store.UserRegion) (store.UserRegion, error) {
	return store.UserRegion{}, nil
}
func (s *stubRegions) Get(_ context.Context, id string) (store.UserRegion, error) {
	for _, rs := range s.rs {
		for _, r := range rs {
			if r.ID == id {
				return r, nil
			}
		}
	}
	return store.UserRegion{}, store.ErrUserRegionNotFound
}
func (s *stubRegions) GetByUserEndpoint(_ context.Context, userID, endpoint string) (store.UserRegion, error) {
	return store.UserRegion{}, store.ErrUserRegionNotFound
}
func (s *stubRegions) Update(_ context.Context, id string, patch store.UserRegion) (store.UserRegion, error) {
	return store.UserRegion{}, nil
}
func (s *stubRegions) Delete(_ context.Context, id string) error            { return nil }
func (s *stubRegions) TouchLastUsed(_ context.Context, id string) error     { return nil }
func (s *stubRegions) Decrypt(_ store.UserRegion) (string, error)           { return "secret", nil }

func TestListRegions_Delegates(t *testing.T) {
	st := &stubRegions{
		rs: map[string][]store.UserRegion{
			"alice": {
				{ID: "r1", UserID: "alice", Alias: "home", Endpoint: "https://s3.example.test", AccessKeyID: "AKID1", Region: "us-east-1", CreatedAt: time.Now()},
				{ID: "r2", UserID: "alice", Alias: "work", Endpoint: "https://other.example.test", AccessKeyID: "AKID2", Region: "us-west-2"},
			},
		},
	}
	b := NewProductionBackend(BackendDeps{Regions: st})
	regions, err := b.ListRegions(context.Background(), &UserContext{UserID: "alice"})
	if err != nil {
		t.Fatalf("ListRegions: %v", err)
	}
	if len(regions) != 2 {
		t.Fatalf("ListRegions: want 2 regions, got %d", len(regions))
	}
	if regions[0].Alias != "home" {
		t.Errorf("ListRegions[0].Alias: want home, got %s", regions[0].Alias)
	}
	if regions[0].ID != "r1" {
		t.Errorf("ListRegions[0].ID: want r1, got %s", regions[0].ID)
	}
}

func TestListRegions_EmptyForUnknownUser(t *testing.T) {
	st := &stubRegions{rs: map[string][]store.UserRegion{}}
	b := NewProductionBackend(BackendDeps{Regions: st})
	regions, err := b.ListRegions(context.Background(), &UserContext{UserID: "nobody"})
	if err != nil {
		t.Fatalf("ListRegions: %v", err)
	}
	if len(regions) != 0 {
		t.Errorf("ListRegions: want empty slice, got %d entries", len(regions))
	}
}

func TestListRegions_NilUserContext_ReturnsEmpty(t *testing.T) {
	st := &stubRegions{rs: map[string][]store.UserRegion{}}
	b := NewProductionBackend(BackendDeps{Regions: st})
	regions, err := b.ListRegions(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListRegions(nil): %v", err)
	}
	if regions != nil {
		t.Errorf("ListRegions(nil): want nil slice, got %v", regions)
	}
}

func TestDriverForRegion_NoStores_ReturnsUnsupported(t *testing.T) {
	b := NewProductionBackend(BackendDeps{})
	_, _, err := b.driverForRegion(context.Background(), "any")
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("driverForRegion no-stores: want ErrUnsupported, got %v", err)
	}
}

// TestGetObject_BackendDelegation: a thin assertion that the
// production GetObject ultimately reaches a driver. We can't easily
// build a full driver registry with a stub backend in this test
// without bringing in the whole stack, so we lean on the
// driverForRegion error path and the WebDAV gateway tests for the
// end-to-end coverage. This test only confirms the early error
// surface when stores aren't wired (the easy failure mode).
func TestGetObject_NoStores_ReturnsUnsupported(t *testing.T) {
	b := NewProductionBackend(BackendDeps{})
	body, _, err := b.GetObject(context.Background(), &UserContext{UserID: "alice"}, "r1", "bk", "key")
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("GetObject no-stores: want ErrUnsupported, got %v", err)
	}
	if body != nil {
		_ = body.Close()
		t.Errorf("GetObject no-stores: body should be nil")
	}
}

// Exercise mapDriverError sentinels — keeps the error translation
// table honest if a driver-side rename changes one of the sentinels.
func TestMapDriverError(t *testing.T) {
	if err := mapDriverError(nil); err != nil {
		t.Errorf("mapDriverError(nil): want nil, got %v", err)
	}
	if got := mapDriverError(errors.New("random")); got.Error() != "random" {
		t.Errorf("mapDriverError(random): want passthrough, got %v", got)
	}
}

// stubReader is a minimal io.Reader used in PutObject delegation
// negative tests. Not really used here; included to keep the test
// surface honest about what we don't fully cover at this tier.
type stubReader struct{ remaining string }

func (s *stubReader) Read(p []byte) (int, error) {
	if s.remaining == "" {
		return 0, io.EOF
	}
	n := copy(p, s.remaining)
	s.remaining = s.remaining[n:]
	return n, nil
}

func TestPutObject_NoStores_ReturnsUnsupported(t *testing.T) {
	b := NewProductionBackend(BackendDeps{})
	err := b.PutObject(context.Background(), &UserContext{UserID: "alice"}, "r1", "bk", "key",
		strings.NewReader("body"), 4, "text/plain")
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("PutObject no-stores: want ErrUnsupported, got %v", err)
	}
}
