package garage_v1 //nolint:revive // package name matches the API generation we target

import (
	"context"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// LifecycleSupport reports lifecycle capabilities. Garage 1.x's admin
// API does NOT surface lifecycle CRUD (it has no /v1/Lifecycle or
// equivalent), so we report Supported=false and the UI hides the
// editor. The basement doctrine: capability gating drives the UX,
// not driver-name checks — so the v0.9.0i bucket detail screen will
// show "Lifecycle policies not supported on this driver" against any
// Garage v1 cluster, including matthew's basement.pq.io classe.
func (d *driver) LifecycleSupport() driverpkg.LifecycleCapabilities {
	return driverpkg.LifecycleCapabilities{Supported: false}
}

// GetLifecycle is a stub: Garage v1 admin API has no lifecycle CRUD.
// Returns ErrUnsupported so direct API callers see a clean sentinel
// even though the UI short-circuits on LifecycleSupport().Supported.
func (d *driver) GetLifecycle(_ context.Context, _ string) ([]driverpkg.LifecycleRule, error) {
	return nil, d.unsupported("GetLifecycle")
}

// PutLifecycle is a stub matching GetLifecycle.
func (d *driver) PutLifecycle(_ context.Context, _ string, _ []driverpkg.LifecycleRule) error {
	return d.unsupported("PutLifecycle")
}

// PerBucketStatsAvailable reports whether the user-region tier
// surfaces Objects + Bytes on its bucket list. v1.4.0a: Garage v1
// returns false here because the public ListBuckets path the user-key
// driver signs against does not carry counters, and the admin bridge
// (s.garageRegionBucketsBridge) is a per-deployment opt-in rather
// than a guarantee. Returning false keeps the FE column visibility
// honest for matthew's basement.pq.io until v2 + the admin-bridge
// stats path matures.
func (d *driver) PerBucketStatsAvailable() bool {
	return false
}
