package driver

import (
	"context"
	"errors"
	"testing"

	"github.com/mattjackson/basement/internal/store"
)

// fakeUserRegions is the minimum store.UserRegions implementation
// needed to satisfy Registry.SetUserRegionsStore in tests that don't
// care about persistence. The Registry only ever stores the reference
// — it never calls methods on it (ForUserRegion just nil-checks).
type fakeUserRegions struct{}

func (fakeUserRegions) Create(_ context.Context, _ store.UserRegion) (store.UserRegion, error) {
	return store.UserRegion{}, nil
}
func (fakeUserRegions) Get(_ context.Context, _ string) (store.UserRegion, error) {
	return store.UserRegion{}, nil
}
func (fakeUserRegions) GetByUserEndpoint(_ context.Context, _, _ string) (store.UserRegion, error) {
	return store.UserRegion{}, nil
}
func (fakeUserRegions) Update(_ context.Context, _ string, _ store.UserRegion) (store.UserRegion, error) {
	return store.UserRegion{}, nil
}
func (fakeUserRegions) Delete(_ context.Context, _ string) error { return nil }
func (fakeUserRegions) ListForUser(_ context.Context, _ string) ([]store.UserRegion, error) {
	return nil, nil
}
func (fakeUserRegions) TouchLastUsed(_ context.Context, _ string) error { return nil }
func (fakeUserRegions) Decrypt(_ store.UserRegion) (string, error)      { return "", nil }

// TestForUserRegion_UnwiredStoreReturnsErrUnsupported: ADR-0002
// defensive check — if Store.UserRegions() was nil at wire-up and the
// registry's regions reference stayed unset, ForUserRegion refuses to
// hand back a driver even though it could technically build one.
func TestForUserRegion_UnwiredStoreReturnsErrUnsupported(t *testing.T) {
	reg := NewRegistry(newMockConnStore())

	_, err := reg.ForUserRegion(context.Background(), "https://s3.example.com", "AK", "SK", "us-east-1", "")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported on unwired regions store, got %v", err)
	}
}

// TestForUserRegion_CachesByEndpointAndKey: two calls with the same
// (endpoint, accessKeyID) share an underlying instance; differing
// accessKeyID gets a fresh build.
func TestForUserRegion_CachesByEndpointAndKey(t *testing.T) {
	reg := NewRegistry(newMockConnStore())
	reg.SetUserRegionsStore(fakeUserRegions{})

	built := 0
	reg.SetRegionDriverBuilder(func(_, _, _, _, _ string) (Driver, error) {
		built++
		return &mockDriver{id: int64(built)}, nil
	})

	ctx := context.Background()

	a1, err := reg.ForUserRegion(ctx, "https://s3.example.com", "AK1", "SK1", "garage", "")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	a2, err := reg.ForUserRegion(ctx, "https://s3.example.com", "AK1", "SK1", "garage", "")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if a1 != a2 {
		t.Errorf("expected same cached instance for same (endpoint,key), got different")
	}
	if built != 1 {
		t.Errorf("expected 1 build, got %d", built)
	}

	// Different access key → separate cache entry.
	b1, err := reg.ForUserRegion(ctx, "https://s3.example.com", "AK2", "SK2", "garage", "")
	if err != nil {
		t.Fatalf("third call: %v", err)
	}
	if a1 == b1 {
		t.Errorf("expected distinct instances for different access keys")
	}
	if built != 2 {
		t.Errorf("expected 2 builds after distinct-key call, got %d", built)
	}
}

// TestForUserRegion_InvalidateUserRegionEvicts: after
// InvalidateUserRegion, the next ForUserRegion rebuilds.
func TestForUserRegion_InvalidateUserRegionEvicts(t *testing.T) {
	reg := NewRegistry(newMockConnStore())
	reg.SetUserRegionsStore(fakeUserRegions{})

	built := 0
	reg.SetRegionDriverBuilder(func(_, _, _, _, _ string) (Driver, error) {
		built++
		return &mockDriver{id: int64(built)}, nil
	})

	ctx := context.Background()
	endpoint := "https://s3.example.com"

	first, err := reg.ForUserRegion(ctx, endpoint, "AK", "SK", "garage", "")
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	reg.InvalidateUserRegion(endpoint, "AK")

	second, err := reg.ForUserRegion(ctx, endpoint, "AK", "SK", "garage", "")
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if first == second {
		t.Errorf("expected fresh instance after InvalidateUserRegion, got cached")
	}
	if built != 2 {
		t.Errorf("expected 2 builds after invalidate, got %d", built)
	}
}

// TestForUserRegion_RejectsEmptyArgs: empty endpoint / accessKey /
// secret each return a distinct error (not ErrUnsupported, which is
// reserved for the unwired-store case).
func TestForUserRegion_RejectsEmptyArgs(t *testing.T) {
	reg := NewRegistry(newMockConnStore())
	reg.SetUserRegionsStore(fakeUserRegions{})
	reg.SetRegionDriverBuilder(func(_, _, _, _, _ string) (Driver, error) {
		return &mockDriver{}, nil
	})

	cases := []struct {
		name                       string
		endpoint, ak, sk, region   string
	}{
		{"empty endpoint", "", "AK", "SK", "garage"},
		{"empty accessKey", "https://s3.example.com", "", "SK", "garage"},
		{"empty secret", "https://s3.example.com", "AK", "", "garage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := reg.ForUserRegion(context.Background(), tc.endpoint, tc.ak, tc.sk, tc.region, "")
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if errors.Is(err, ErrUnsupported) {
				t.Errorf("expected non-ErrUnsupported error for %s, got %v", tc.name, err)
			}
		})
	}
}
