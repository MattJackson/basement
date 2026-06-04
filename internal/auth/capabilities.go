package auth

// Capability strings are the single source of truth for what a role
// can do. See docs/adr/0009-capability-based-rbac.md for the operations
// matrix.
//
// Three categories:
//   - platform.*   — basement-level operations (UI Admin only)
//   - cluster.*    — split between cluster.wiring.* (UI Admin) and
//                    cluster.contents.*/buckets/keys/encryption/grants
//                    (Cluster Admin, scoped to ar.Cluster)
//   - self.*       — own-resource operations (User)
//
// Every capability is checked via Can(claims, capability) for inline
// checks or RequireCapability(capability) as middleware. Route mounts
// should use RequireCapability instead of ad-hoc activeRole checks.

// Platform-level capabilities (UI Admin)
const (
	CapPlatformUsersList            = "platform.users.list"
	CapPlatformUsersCreate          = "platform.users.create"
	CapPlatformUsersUpdate          = "platform.users.update"
	CapPlatformUsersDelete          = "platform.users.delete"
	CapPlatformPoliciesList         = "platform.policies.list"
	CapPlatformPoliciesWrite        = "platform.policies.write"
	CapPlatformServiceAccountsList  = "platform.service-accounts.list"
	CapPlatformServiceAccountsWrite = "platform.service-accounts.write"
	CapPlatformAuditRead            = "platform.audit.read"
	CapPlatformSkinsWrite           = "platform.skins.write"
	CapPlatformOIDCWrite            = "platform.oidc.write"
	CapPlatformSystemWrite          = "platform.system.write"
	CapPlatformOnboardingWrite      = "platform.onboarding.write"

	// Read counterparts (ADR-0009 Phase C): the matrix listed only the
	// write/list ops, but the GET routes for these surfaces need a
	// capability of their own so RequireCapability can gate them without
	// borrowing the write cap. All UI-Admin-only, same persona as the
	// write side.
	CapPlatformSystemRead     = "platform.system.read"
	CapPlatformOIDCRead       = "platform.oidc.read"
	CapPlatformSkinsRead      = "platform.skins.read"
	CapPlatformOnboardingRead = "platform.onboarding.read"
)

// Cluster wiring (UI Admin — basement<->cluster connection)
const (
	CapClusterWiringList    = "cluster.wiring.list"
	CapClusterWiringCreate  = "cluster.wiring.create"
	CapClusterWiringUpdate  = "cluster.wiring.update"
	CapClusterWiringDelete  = "cluster.wiring.delete"
	CapClusterWiringTest    = "cluster.wiring.test"
	CapClusterBucketsAggr   = "cluster.buckets.aggregate"
	CapClusterUsageAggr     = "cluster.usage.aggregate"

	// CapClusterWiringRead reads ONE cluster's redacted connection
	// record (GET /admin/clusters/{cid}) + its driver-info. Unlike the
	// rest of cluster.wiring.*, this one is held by BOTH ui-admin (any
	// cluster) and cluster-admin (their cluster only) — the cluster
	// detail page renders the label/driver/color for both personas and
	// the payload is redacted (no admin_token). Because it appears in
	// the cluster-admin grant below it is treated as cluster-scoped, so
	// cluster-admin@X cannot read cluster Y's wiring; ui-admin's grant
	// is unscoped (Can skips the scope check for non-cluster-admin).
	CapClusterWiringRead = "cluster.wiring.read"
)

// Cluster contents (Cluster Admin — what's inside a cluster, scoped)
const (
	CapClusterContentsRead          = "cluster.contents.read"
	CapClusterBucketsCreate         = "cluster.buckets.create"
	CapClusterBucketsUpdate         = "cluster.buckets.update"
	CapClusterBucketsDelete         = "cluster.buckets.delete"
	CapClusterBucketsLifecycleWrite = "cluster.buckets.lifecycle.write"
	CapClusterKeysCreate            = "cluster.keys.create"
	CapClusterKeysUpdate            = "cluster.keys.update"
	CapClusterKeysRotate            = "cluster.keys.rotate"
	CapClusterKeysDelete            = "cluster.keys.delete"
	// Layout (Garage topology stage/apply/revert) + block-scrub
	// maintenance are cluster-admin operations the ADR-0009 matrix did
	// not enumerate a write cap for. Added in Phase C so every cluster
	// route gates uniformly on RequireCapability.
	CapClusterLayoutWrite = "cluster.layout.write"
	CapClusterScrubWrite  = "cluster.scrub.write"
	CapClusterEncryptionAdminsList  = "cluster.encryption-admins.list"
	CapClusterEncryptionAdminsAdd   = "cluster.encryption-admins.add"
	CapClusterEncryptionAdminsRm    = "cluster.encryption-admins.remove"
	CapClusterEncryptionUnlock      = "cluster.encryption.unlock"
	CapClusterEncryptionLock        = "cluster.encryption.lock"
	CapClusterGrantsList            = "cluster.grants.list"
	CapClusterGrantsWrite           = "cluster.grants.write"
)

// User self-service (own resources, scoped by region tier)
const (
	CapSelfFilesRead       = "self.files.read"
	CapSelfFilesWrite      = "self.files.write"
	CapSelfKeysList        = "self.keys.list"
	CapSelfKeysCreate      = "self.keys.create"
	CapSelfKeysRotate      = "self.keys.rotate"
	CapSelfKeysDelete      = "self.keys.delete"
	CapSelfSharesWrite     = "self.shares.write"
	CapSelfBackupsWrite    = "self.backups.write"
	CapSelfFederationsWrite = "self.federations.write"
	CapSelfWebhooksWrite   = "self.webhooks.write"
)

// roleCapabilities maps each role kind to the capabilities it grants.
// Cluster scoping (ar.Cluster) is enforced separately at the route
// layer via path param matching — this map is the role-level grant
// only.
var roleCapabilities = map[string][]string{
	"ui-admin": {
		CapPlatformUsersList,
		CapPlatformUsersCreate,
		CapPlatformUsersUpdate,
		CapPlatformUsersDelete,
		CapPlatformPoliciesList,
		CapPlatformPoliciesWrite,
		CapPlatformServiceAccountsList,
		CapPlatformServiceAccountsWrite,
		CapPlatformAuditRead,
		CapPlatformSkinsWrite,
		CapPlatformOIDCWrite,
		CapPlatformSystemWrite,
		CapPlatformOnboardingWrite,
		CapPlatformSystemRead,
		CapPlatformOIDCRead,
		CapPlatformSkinsRead,
		CapPlatformOnboardingRead,
		CapClusterWiringList,
		CapClusterWiringCreate,
		CapClusterWiringUpdate,
		CapClusterWiringDelete,
		CapClusterWiringTest,
		CapClusterWiringRead,
		CapClusterBucketsAggr,
		CapClusterUsageAggr,
	},
	"cluster-admin": {
		CapClusterWiringRead,
		CapClusterContentsRead,
		CapClusterBucketsCreate,
		CapClusterBucketsUpdate,
		CapClusterBucketsDelete,
		CapClusterBucketsLifecycleWrite,
		CapClusterLayoutWrite,
		CapClusterScrubWrite,
		CapClusterKeysCreate,
		CapClusterKeysUpdate,
		CapClusterKeysRotate,
		CapClusterKeysDelete,
		CapClusterEncryptionAdminsList,
		CapClusterEncryptionAdminsAdd,
		CapClusterEncryptionAdminsRm,
		CapClusterEncryptionUnlock,
		CapClusterEncryptionLock,
		CapClusterGrantsList,
		CapClusterGrantsWrite,
	},
	"user": {
		CapSelfFilesRead,
		CapSelfFilesWrite,
		CapSelfKeysList,
		CapSelfKeysCreate,
		CapSelfKeysRotate,
		CapSelfKeysDelete,
		CapSelfSharesWrite,
		CapSelfBackupsWrite,
		CapSelfFederationsWrite,
		CapSelfWebhooksWrite,
	},
}

// CapabilitiesFor returns the capability list for an active role.
// Returns an empty slice for a nil active role.
func CapabilitiesFor(ar *ActiveRole) []string {
	if ar == nil {
		return nil
	}
	caps, ok := roleCapabilities[ar.Kind]
	if !ok {
		return nil
	}
	out := make([]string, len(caps))
	copy(out, caps)
	return out
}

// Can reports whether the claims' active role grants a capability.
// Cluster-scoped capabilities additionally require ar.Cluster to
// match the requested cluster — pass clusterID="" when the capability
// isn't cluster-scoped (e.g. all platform.* and cluster.wiring.*).
func Can(claims *Claims, capability string, clusterID string) bool {
	if claims == nil || claims.ActiveRole == nil {
		return false
	}
	ar := claims.ActiveRole
	caps, ok := roleCapabilities[ar.Kind]
	if !ok {
		return false
	}
	found := false
	for _, c := range caps {
		if c == capability {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	// Cluster-scoped capability enforcement (hardened, fail-closed).
	//
	// Previously this only checked scope for ar.Kind == "cluster-admin"
	// and skipped entirely when clusterID was empty — so any cluster-
	// scoped cap accidentally granted to another role, OR a route that
	// resolved an empty cid (e.g. a mis-named path param), became an
	// unscoped global grant. Now: a cluster-scoped capability requires a
	// matching, non-empty clusterID for EVERY role, with exactly one
	// explicit exception — UI Admin holds cluster.wiring.read as a
	// platform-wide grant (reading any cluster's redacted connection
	// record). Every other cluster-scoped cap is cluster-admin contents
	// and is denied without an exact cluster match.
	if IsClusterScopedCapability(capability) {
		uiAdminWiringRead := ar.Kind == "ui-admin" && capability == CapClusterWiringRead
		if !uiAdminWiringRead {
			if clusterID == "" || ar.Cluster != clusterID {
				return false
			}
		}
	}
	return true
}

// IsClusterScopedCapability reports whether a capability is scoped to
// a single cluster (vs platform-wide). cluster-admin holds these and
// they require ar.Cluster to match the target cluster.
func IsClusterScopedCapability(capability string) bool {
	for _, c := range roleCapabilities["cluster-admin"] {
		if c == capability {
			return true
		}
	}
	return false
}
