# ADR-0005: Federation + multi-backend replication

- **Status**: Accepted
- **Date**: 2026-05-22
- **Triggered by**: ADR-0004 Option C; operator-confirmed v1.6 theme
- **Builds on**: [ADR-0002](./0002-region-tier-user-model.md) (region tier), [v1.5 backup engine](../release-notes/v1.5.0.md) (sync substrate)
- **Enables**: v2.0 S3 gateway routes over the federation map this ADR establishes

## Context

basement v1.5 ships a backup engine: scheduled bucket → bucket copies with mirror + snapshot modes + GFS retention + restore. That's "back this up over there, on a schedule."

Federation is a different shape: "this bucket lives on multiple backends *as the same logical bucket*. Treat them as one." Same plumbing (sync engine, scheduler), different polarity (replicas are first-class, continuously synced, presented unified).

The operator's specific motivation: home Garage + interest in off-site safety (B2/Wasabi). Backup = "I have a copy I can restore from." Federation = "my data lives on both, automatically, transparently."

## Decision

Add a `FederatedBucket` first-class concept. It declares a canonical bucket name + primary backend + replica backends + policy. basement's federation engine continuously mirrors writes from primary to replicas + tracks lag + handles failover.

### Data model

```go
// internal/federation/types.go (NEW)

type FederatedBucket struct {
    ID          string           `json:"id"`            // UUID
    OwnerUserID string           `json:"ownerUserId"`   // who set this up
    Name        string           `json:"name"`          // canonical name, e.g. "lsi"
    Primary     ReplicaTarget    `json:"primary"`
    Replicas    []ReplicaTarget  `json:"replicas"`
    Policy      FederationPolicy `json:"policy"`
    CreatedAt   time.Time        `json:"createdAt"`
    UpdatedAt   time.Time        `json:"updatedAt"`
}

type ReplicaTarget struct {
    RegionID  string    `json:"regionId"`  // UserRegion ID
    Bucket    string    `json:"bucket"`    // alias or ID at that region
    LastSync  time.Time `json:"lastSync,omitempty"`  // last successful sync
    Health    string    `json:"health,omitempty"`     // "in-sync" | "lagging" | "stale" | "broken"
    LagBytes  int64     `json:"lagBytes,omitempty"`   // bytes pending replication
    LagObjects int64    `json:"lagObjects,omitempty"` // objects pending replication
}

type FederationPolicy struct {
    SyncMode     string `json:"syncMode"`             // "continuous" (event-driven) | "scheduled" (cron)
    Schedule     string `json:"schedule,omitempty"`    // cron expression when SyncMode=scheduled
    LagAlertSec  int    `json:"lagAlertSec"`          // alert if replica falls behind by this many seconds
    WriteQuorum  int    `json:"writeQuorum"`          // writes confirmed when ≥N backends accept (1 = primary-only, default)
    AutoFailover bool   `json:"autoFailover,omitempty"` // promote a replica if primary down for N seconds (v1.6.x — opt-in)
    AutoFailoverSec int  `json:"autoFailoverSec,omitempty"`
}
```

### Storage

`{dataDir}/federated_buckets.json` — atomic write, same pattern as `backups.json` / `user_regions.json`.

### Replication engine

`internal/federation/engine.go`:

- Listens for "primary write" events (two sources):
  1. **Webhook-driven** (v1.7+): bucket event webhooks from the primary backend → engine queues a replicate
  2. **Polling fallback** (v1.6 first cut): every 10s, list primary's recent objects (via the audit log OR ListObjects modified-since), compare to replicas, queue deltas
- Replicate queue: per-replica goroutine pool (default 4 workers). Each task is a single-object PUT from primary to replica using the existing sync engine's stream/server-side-copy primitives.
- Lag tracking: after each replicate, update `ReplicaTarget.LastSync + LagBytes + LagObjects + Health`. Health derived from `LagSec > Policy.LagAlertSec` → `"lagging"` → `"stale"` (10× alert threshold) → `"broken"` (sync errored repeatedly).

### API endpoints

User-tier (gated on USER auth — user owns their federated buckets):
- `POST /api/v1/user/federated-buckets` — create
- `GET /api/v1/user/federated-buckets` — list user's federations
- `GET /api/v1/user/federated-buckets/{id}` — detail + health
- `PUT /api/v1/user/federated-buckets/{id}` — update policy / replicas
- `DELETE /api/v1/user/federated-buckets/{id}` — stop replicating + clean up (DOES NOT delete replica data)
- `POST /api/v1/user/federated-buckets/{id}/failover` — promote a replica to primary (manual; future auto-failover is policy-driven)
- `POST /api/v1/user/federated-buckets/{id}/resync` — force full re-replicate (e.g. after a network outage)

Audit: `federation:create`, `federation:update`, `federation:delete`, `federation:failover`, `federation:resync`, `federation:replicate_object` (one entry per object replicated — high volume, expect to filter by default in /admin/audit).

### Unified bucket view

`/files/{regionId}/b/{bucket}` already shows "Objects in bucket X". When the bucket is part of a federation:
- Badge "Federated: 3 replicas, 2 in sync, 1 lagging"
- Sub-section "Replicas" with per-target health row + lag + last-sync time
- Action: "Manage federation" link → `/files/federated-buckets/{id}`

`/files/federated-buckets/` new route — list, create, manage.

### Federation wizard

`/files/federated-buckets/new`:
- Step 1: Pick primary — region + bucket dropdown (must already exist)
- Step 2: Add replicas — N region+bucket pairs (can add 1-5 in the wizard; more via API)
- Step 3: Policy — sync mode (continuous default; scheduled if backend doesn't support webhooks), lag alert threshold, write quorum (1 default), auto-failover off by default
- Step 4: Initial-sync confirmation — "This will copy {N} objects ({size}) from primary to each replica. Estimated time: X. Continue?"
- Step 5: Review + save

### Failover semantics

Manual failover via `POST .../failover` body: `{newPrimaryRegionId, newPrimaryBucket}`. Effect:
1. Verify the new primary is currently a replica (must be in the federation)
2. Swap primary ↔ that replica in storage
3. Drain the replication queue (old primary writes finish flushing)
4. Audit `federation:failover` with old + new primary
5. Future writes via the bucket browser route to new primary

Auto-failover (opt-in via policy): a watchdog goroutine pings primary health every 30s. After `AutoFailoverSec` consecutive failures, promote the healthiest replica. Logs + alerts (per the audit system) so operator notices.

### What stays unchanged

- UserRegion model (ADR-0002) — federations are LAYERED ON TOP of regions; each replica is a (region, bucket) pair
- Backup engine — backup is still backup; federation is its own concept. Operators may use both ("federated for live mirror + scheduled backup for historical snapshots")
- Sync engine — federation uses it as the copy primitive, no changes
- Audit log — gets new event types, otherwise unchanged
- All four drivers — no driver interface change needed; federation is store-layer + engine-layer

## Consequences

### Good

- **Real DR story**: home Garage + B2 off-site become ONE bucket. Lose Garage → promote B2, keep working.
- **No client tooling changes**: clients still hit backends directly. basement is control plane only. No bandwidth bottleneck.
- **Builds directly on v1.5 backup engine**: minimum new ground.
- **Lays the routing map for v2.0 gateway**: when the gateway lands, it routes inbound requests USING the federation topology (read → nearest healthy replica; write → primary).
- **Operator-friendly defaults**: continuous sync via polling = works on any S3 backend without webhook support. Webhooks (v1.7) upgrade it transparently.

### Painful

- **Continuous polling** in v1.6 first cut → some load on the primary. 10s tick is conservative; tune via policy later.
- **Eventually consistent**: writes confirmed by primary may not be on replicas for seconds. Policy.WriteQuorum > 1 can require multiple backends to confirm, but introduces write latency.
- **Conflict resolution during failover**: if a write hit primary just before failover, it may not yet be on the replica being promoted. Lost write window. Mitigated by quorum + by failover always draining the queue first.
- **Audit log volume**: `federation:replicate_object` per object × N replicas can flood the audit log. Default filter excludes it from the /admin/audit view; visible via `?action=federation:replicate_object`.

### Open questions

- **Two-way / multi-primary federation**: every replica accepts writes, eventually consistent across all. CRDT-ish. Out of scope for v1.6; punted to v2.x.
- **Delete propagation**: today's first cut treats deletes as replicated PUTs of "tombstone" objects. Real S3 delete-marker support requires v1.10 versioning. v1.6 documents this as a known limitation.
- **Cross-driver auth**: federation between (e.g.) Garage + AWS works fine because each replica has its own UserRegion. No special cross-driver auth needed.

## Implementation plan

7 cycles for v1.6.

| Cycle | Deliverable |
|---|---|
| **v1.6.0a** | `internal/federation/` package — types, store, JSON persistence, tests |
| **v1.6.0b** | Replication engine — polling-based + per-replica worker pool + lag tracking |
| **v1.6.0c** | API endpoints — CRUD + failover + resync, gated on USER auth, audited |
| **v1.6.0d** | Frontend — `/files/federated-buckets` list + wizard + detail page |
| **v1.6.0e** | Unified bucket view — badge + replicas section + "Manage federation" link on `/files/{regionId}/b/{bucket}` |
| **v1.6.0f** | Failover flow — manual UI + opt-in auto-failover policy |
| **v1.6.0g** | Release notes + smoke + v1.6.0 milestone tag (senior gate per [[feedback_senior_smoke_at_minor_release_only]]) |

## Tags

`federation`, `replication`, `multi-backend`, `dr`, `v1.6`, `enables-v2.0-gateway`
