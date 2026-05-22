# ADR-0004: v2.0 scoping proposal (HALT for operator review)

- **Status**: Resolved — operator picked Option C (hybrid). Federation in v1.6, gateway in v2.0. Full v1.6 → v2.0 sequence appended at bottom.
- **Date**: 2026-05-22
- **Author**: Senior (Claude Opus)
- **Decision**: Operator chose Option C + expanded the v1.x slots with concrete themes (federation → interaction surface → compliance → MinIO-parity polish → gateway). Roadmap section added at the bottom.

## Where we are

v1.0 → v1.5 shipped in one unattended session-and-a-half. Recap:

| Tag | Headline | Key contracts |
|---|---|---|
| v1.0 | Multi-backend admin + RBAC + scoped creds | ADR-0001 (three-tier roles), capability matrix, encrypted admin_token, audit log, metrics persistence |
| v1.1 | Region tier replaces phantom Connections at user persona | ADR-0002 (UserRegion as primary user noun), Garage admin bridge, per-user S3 signing |
| v1.2 | Sudo-style admin elevation | ADR-0003 (USER + ADMIN modes, configurable TTL, drop-in-place banner, OIDC step-up) |
| v1.3 | Multi-user polish | OIDC group mapping, invite tokens, bulk-import, key-first model, per-cluster cluster_admin UI |
| v1.4 | Scale + perf | Bucket browser virtualization, paginated key perms, batch object ops, audit pagination + CSV, Garage block-scrub, growth analytics |
| v1.5 | Backup story | Scheduled S3 → S3 backups, mirror + snapshot modes, GFS retention, point-in-time restore |

basement is now a polished multi-backend admin UI with **complete operator workflows** for an at-home or small-team self-host. Each minor version was milestone-tagged after a senior-run smoke + screenshot + manual exercise pass.

## What v2.0 should be (the question)

ADR-0002 already named v2.0 as the "long-haul answer" milestone. Two candidate visions; operator picks one (or hybrid).

---

### Option A — S3 API gateway

basement IS the S3 endpoint. Clients point at `https://s3.basement.local`; basement terminates the S3 request, looks up which UserRegion + cluster owns the bucket, and proxies/signs to the backend.

**The user experience**:
- One S3 endpoint for the whole environment, not N
- aws-cli + rclone + s3fs etc. point at basement, not at the backend directly
- Per-request access enforced by basement (its own policy matrix) — backend keys can be locked down to per-cluster shared service accounts
- Audit trail is complete + uniform across backends because basement sees every request

**What changes**:
- New `internal/s3gateway/` package — listens on a separate port, terminates S3 requests, dispatches to driver clients
- basement needs to implement enough of the S3 API to be useful: ListBuckets, ListObjects (+ delimiter), Get/Put/Delete object, multipart, presign
- SigV4 verification on inbound requests (using basement-issued credentials, not backend creds)
- Capability gating moves from per-route handlers to per-S3-op
- Per-operator basement keys (separate from backend keys) — they replace user-region keys for tools

**Pros**:
- One endpoint, one access model — drastically simpler client config
- basement owns auth + audit, not the backend
- Enables cross-bucket transparency: a single client URL can target buckets on different physical backends
- Differentiator: no other open-source multi-backend admin UI does this

**Cons**:
- HUGE scope. Implementing enough S3 to satisfy real clients (aws-cli, rclone, mc, s3fs, Veeam, etc.) is months of work, not weeks
- All traffic flows through basement — becomes a bandwidth + latency bottleneck. Need HTTP/2 + connection pooling + maybe streaming pass-through
- Single point of failure for object access (today: basement crash = admin pane gone, but objects still reachable directly)
- Compatibility surface is enormous — every S3 quirk operators rely on becomes basement's problem
- Maintenance burden: AWS adds S3 features, we have to chase them

**Effort estimate**: 4-6 unattended sessions like the v1.x ones. Two months of focused work.

---

### Option B — Multi-region federation + read replicas

basement stays as admin UI but gains a **federation layer**: bucket aliases can span multiple backends. "lsi" can live on `home-garage` AND `b2-offsite` with basement aware of both. Read-through caching + write-routing handled by basement's federation layer; clients still hit the backend directly.

**The user experience**:
- Operator declares a federated bucket: "lsi → primary: home-garage, replicas: [b2-offsite]"
- Writes routed to primary; reads can fall through to replicas (latency-based routing)
- basement knows the topology + serves a unified bucket browser across replicas
- Each backend keeps its own S3 endpoint; clients can pick (or use basement's gateway as a thin federation router, smaller scope than Option A)
- Backup wizard (v1.5) becomes the substrate — backup IS a replica, basement just promotes it to first-class

**What changes**:
- New `FederatedBucket` concept: `(canonical_name, primary_target, [replica_targets])`
- Federation engine: tracks which replicas are in-sync with primary, monitors lag, signals stale
- Bucket browser unified across replicas (one view, footnote "replicated to N backends")
- Write engine: every PUT to a federated bucket is mirrored to replicas (async, with eventual consistency tracking)
- Light "federation router" for clients: an optional `/api/v1/route/{bucket}/...` endpoint that picks the right backend — narrower scope than full S3 gateway
- Failover: if primary fails, promote a replica + reroute

**Pros**:
- Builds DIRECTLY on v1.5's backup engine — minimum new ground
- Solves the operator's real pain: "I have data on this Garage and want it mirrored to B2 for safety, and accessible from both"
- Per-operator config — federation only happens where declared, not everywhere
- Smaller scope than gateway, ships in weeks not months
- Backend connections stay direct + fast; basement only handles control plane

**Cons**:
- Less ambitious as a "vision" milestone
- Consistency story is "eventual" — write to primary + async replicate. Not strong consistency.
- Doesn't simplify client config — clients still need backend credentials
- Doesn't address per-request audit uniformity (basement only sees control-plane ops)

**Effort estimate**: 2-3 unattended sessions. ~3 weeks of focused work.

---

### Option C — Both, sequenced (hybrid)

**v2.0** = Option B (federation, ~3 weeks)
**v3.0** = Option A (S3 gateway, ~2 months)

Federation in v2.0 ships fast + delivers operator-real value. Gateway in v3.0 is the long-term answer. Both rest on the v1.5 sync/backup engine.

---

## Other considerations

These should be folded into whichever option, not standalone:

- **Plugin SDK**: Let third parties write drivers for backends we don't support (Wasabi, Tigris, R2, custom). Useful regardless of A/B/C.
- **Web Console SDK**: Let other projects embed basement screens (admin panes for SaaS providers). Independent of A/B/C.
- **K8s operator + Helm chart**: Productionization. Pure ops, not product. Could be a side track.
- **Multi-tenancy with billing**: Different product. Not v2.0 territory.

## What I recommend

**Option C (Hybrid)** — Federation in v2.0, Gateway in v3.0.

- Operator gets a tangible v2.0 quickly: cross-backend replication that solves real data-safety problems
- v3.0 ambition is preserved + has a v2.0 substrate to build on (federated buckets = natural input to the gateway's routing layer)
- Risk distributed across two milestones instead of one giant bet

Federation specifically because:
- It's the next logical step after v1.5's backup engine
- Operator's setup (Garage at home + interest in off-site) directly benefits
- Doesn't require basement to become a bandwidth bottleneck

## What I need from you

1. **A, B, or C?** Pick one as v2.0's headline.
2. **Federation scope** (if B or C): replication only? Or full failover? Or read-replicas + write-through too?
3. **Gateway timing** (if C): same calendar quarter as federation, or wait for v3.0?
4. **Any priorities to bump in front of v2.0?** (e.g. "ship the K8s operator first, then start v2.0")

After your call, I draft ADR-0005 (the chosen v2.0 design) + dispatch the v2.0.0a-h cycle chain.

## Halt

Senior is halting per `[[project_long_haul_autonomy]]`. No further dispatches without operator decision.

---

## Resolution (2026-05-22, post-halt)

Operator picked Option C + concretized the v1.x slot themes during the halt conversation. Final roadmap to v2.0:

| Tag | Theme | Highlights |
|---|---|---|
| **v1.6** | **Federation + replication** | FederatedBucket type, continuous replication (event-driven or sub-minute poll), lag tracking, unified bucket view, manual failover. Builds DIRECTLY on v1.5's backup engine. Solves DR + multi-backend mirror story. See ADR-0005. |
| **v1.7** | **Service accounts + webhooks** | M2M auth (basement-issued long-lived keys for CI/k8s/CLI/MCP), bucket event notifications (HTTP webhooks). Auth substrate for v1.8 CLI/MCP + v2.0 gateway. Webhooks enable event-driven federation polling-replacement. |
| **v1.8** | **CLI + MCP server + Mobile PWA** | `basement` Go binary (uses v1.7 service accounts), MCP server exposing basement as Claude tools, PWA-first mobile (native deferred to v2.x). Interaction surfaces for S3-native users + AI workflows. |
| **v1.9** | **WebDAV + SMB gateways** | Native network-drive protocols for non-tech users. Finder / Explorer / file managers see basement buckets as folders. Separate ports, separate code paths, shared auth + audit. |
| **v1.10** | **Compliance + integrity** — Versioning + Object-Lock + SSE | Per-driver versioning capability, S3 object-lock (governance + compliance + legal hold), SSE-S3 + SSE-KMS. Ransomware shield + compliance posture. Versioning + lock are coupled by S3 semantics — ship together. |
| **v2.0** | **basement IS a backend (S3 gateway)** | basement terminates inbound S3 requests, SigV4 verifies its own keys, routes to backend(s) using federation map from v1.6. Cert mgmt, HTTP/2, streaming pass-through, per-key rate limits. "Migrate AWAY from MinIO entirely" lands here. |

### Threading

- v1.6 federation provides the bucket→backend(s) map that v2.0 gateway routes over
- v1.7 service accounts ARE the credentials v1.8 CLI/MCP use + v2.0 gateway verifies on inbound requests
- v1.7 webhooks upgrade v1.6's polling-based federation to event-driven (PUT to primary → webhook → instant replicate)
- v1.8 interaction surfaces (CLI/MCP/Mobile PWA) hit S3-native + AI workflows
- v1.9 WebDAV + SMB hit non-tech / family / Finder-Explorer users
- v1.10 versioning + lock make federated replicas immutable (lock COMPLEMENTS federation — without it, federation just spreads ransomware blast radius)
- v2.0 ties them together: gateway routes federated traffic, authed by basement-issued service accounts, with versioning + lock guarantees, observable via webhooks, manageable via CLI/MCP, accessible via native S3 + WebDAV + SMB

### Beyond v2.0 (sketch only — not committed)

- v2.1 — Client-side encryption (E2EE) for federated replicas to untrusted backends
- v2.2 — Search + tags + smart collections (also unlocks MCP search() at full power)
- v2.3 — WebDAV + SMB + NFS gateways
- v2.4 — Cost engine + lifecycle v2 (cross-backend tiering)
- v3.0 — Plugin SDK + multi-site mesh + IPFS / CDN / marketplace

That's a 6-9 month arc to v2.0. Whatever falls out of operator pain-of-the-week may bump these around.
