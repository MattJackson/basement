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
)

// Cluster contents (Cluster Admin — what's inside a cluster, scoped)
const (
	CapClusterContentsRead          = "cluster.contents.read"
	CapClusterBucketsCreate         = "cluster.buckets.create"
	CapClusterBucketsUpdate         = "cluster.buckets.update"
	CapClusterBucketsDelete         = "cluster.buckets.delete"
	CapClusterBucketsLifecycleWrite = "cluster.buckets.lifecycle.write"
	CapClusterKeysCreate            = "cluster.keys.create"
	CapClusterKeysRotate            = "cluster.keys.rotate"
	CapClusterKeysDelete            = "cluster.keys.delete"
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
		CapClusterWiringList,
		CapClusterWiringCreate,
		CapClusterWiringUpdate,
		CapClusterWiringDelete,
		CapClusterWiringTest,
		CapClusterBucketsAggr,
		CapClusterUsageAggr,
	},
	"cluster-admin": {
		CapClusterContentsRead,
		CapClusterBucketsCreate,
		CapClusterBucketsUpdate,
		CapClusterBucketsDelete,
		CapClusterBucketsLifecycleWrite,
		CapClusterKeysCreate,
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
	// Cluster-scoped capabilities (anything cluster-admin holds) also
	// require ar.Cluster to match clusterID when one is supplied.
	if ar.Kind == "cluster-admin" && clusterID != "" && ar.Cluster != clusterID {
		return false
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
