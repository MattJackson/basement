// Package federation defines the data model and persistence for
// FederatedBuckets — the v1.6 concept of "this bucket lives on multiple
// backends as the same logical bucket, kept in sync continuously".
//
// Per ADR-0005, federation reuses the v1.5 backup/sync engine as the
// copy primitive but reverses the polarity: backups are scheduled,
// one-way, point-in-time copies; federations are continuous, primary-
// to-replicas mirrors with first-class lag/health tracking.
//
// Cycle v1.6.0a (this file) covers the data layer only — types,
// uniqueness, JSON persistence. The replication engine, API surface
// and frontend land in v1.6.0b / 0c / 0d.
package federation

import "time"

// FederatedBucket is one operator-declared logical bucket spanning a
// primary backend and N replicas. The owner (OwnerUserID) is the user
// who set it up; uniqueness on Name is per-owner — two different users
// can each have a federation called "photos" without colliding.
//
// JSON tags are camelCase to match the rest of the v1.x user API
// surface (see internal/backup/types.go).
type FederatedBucket struct {
	ID          string           `json:"id"`          // UUID assigned by Create
	OwnerUserID string           `json:"ownerUserId"` // who owns this federation
	Name        string           `json:"name"`        // canonical logical name, user-scoped
	Primary     ReplicaTarget    `json:"primary"`
	Replicas    []ReplicaTarget  `json:"replicas"`
	Policy      FederationPolicy `json:"policy"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
}

// ReplicaTarget is one (region, bucket) endpoint inside a federation.
// Used for both the primary and each replica — the polarity comes from
// where it sits on the parent FederatedBucket.
//
// Health/lag fields are written by the replication engine (v1.6.0b),
// not by the API handlers. The store's UpdateReplicaHealth path is the
// engine's hot-path callback and mutates JUST these fields on a single
// replica entry — see store.go.
type ReplicaTarget struct {
	RegionID   string    `json:"regionId"`             // UserRegion ID
	Bucket     string    `json:"bucket"`               // bucket name at that region
	LastSync   time.Time `json:"lastSync,omitempty"`   // last successful replicate
	Health     string    `json:"health,omitempty"`     // see Health* constants
	LagBytes   int64     `json:"lagBytes,omitempty"`   // bytes pending replication
	LagObjects int64     `json:"lagObjects,omitempty"` // objects pending replication
}

// FederationPolicy controls the replication engine's behaviour for one
// FederatedBucket. Safe defaults are produced by DefaultPolicy() — the
// API handlers in v1.6.0c will substitute them when a client posts a
// federation without explicit policy values.
//
// WriteQuorum defaults to 1 (primary-only confirm). Setting it higher
// requires multiple backends to acknowledge before a write is
// considered durable — that's a v1.6.x feature gated behind operator
// policy; the v1.6.0a store just records the value.
//
// AutoFailover is opt-in (v1.6.0f). When set, AutoFailoverSec is the
// consecutive-failure window before the watchdog promotes a replica.
type FederationPolicy struct {
	SyncMode        string `json:"syncMode"`                  // "continuous" | "scheduled"
	Schedule        string `json:"schedule,omitempty"`        // cron expression for SyncMode=scheduled
	LagAlertSec     int    `json:"lagAlertSec"`               // alert if replica lag exceeds this many seconds
	WriteQuorum     int    `json:"writeQuorum"`               // 1 = primary-only confirm (default)
	AutoFailover    bool   `json:"autoFailover,omitempty"`    // opt-in: promote replica on primary failure
	AutoFailoverSec int    `json:"autoFailoverSec,omitempty"` // seconds of consecutive primary failure before promote
}

// SyncMode constants — the legal values for FederationPolicy.SyncMode.
const (
	// SyncModeContinuous runs the replication engine on a short tick
	// (10s default) plus any webhook signals from the primary. This
	// is the v1.6 default and the only mode implemented in v1.6.0b.
	SyncModeContinuous = "continuous"
	// SyncModeScheduled runs the replication engine on a cron
	// expression carried in Policy.Schedule. Reserved for v1.7+.
	SyncModeScheduled = "scheduled"
)

// Health constants — the legal values for ReplicaTarget.Health.
//
// The engine derives these from Policy.LagAlertSec:
//   - HealthPending: the engine has not yet verified this replica
//     (no successful replicate, no observed zero-source confirmation).
//     Added in v1.11.0.4 to stop the engine from falsely claiming
//     "in-sync" on a brand-new federation where the boot tick fired
//     against an empty primary before the operator's first upload.
//   - HealthInSync: replicate succeeded within the last LagAlertSec OR
//     the engine has confidently HEAD-verified every source object on
//     the replica (no diff observed).
//   - HealthLagging: behind by more than LagAlertSec but less than 10x
//   - HealthStale: behind by more than 10 * LagAlertSec
//   - HealthBroken: repeated sync errors regardless of lag
//
// Empty string is the zero/never-replicated state and renders the same
// as HealthPending in the FE (v1.6.0d / v1.11.0.4) — a fresh federation
// isn't yet reporting health.
const (
	HealthPending = "pending"
	HealthInSync  = "in-sync"
	HealthLagging = "lagging"
	HealthStale   = "stale"
	HealthBroken  = "broken"
)

// DefaultPolicy returns the safe-by-default federation policy used by
// the v1.6.0c API handlers when a client posts a federation without
// an explicit policy block.
//
// Defaults:
//   - SyncMode: continuous (event-driven engine)
//   - LagAlertSec: 300 (5 minutes — past this the replica is "lagging")
//   - WriteQuorum: 1 (primary-only confirms a write)
//   - AutoFailover: off (manual failover only until operator opts in)
func DefaultPolicy() FederationPolicy {
	return FederationPolicy{
		SyncMode:    SyncModeContinuous,
		LagAlertSec: 300,
		WriteQuorum: 1,
	}
}
