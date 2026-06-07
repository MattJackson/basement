// ADR-0009 capability ID mirror.
//
// SOURCE OF TRUTH: internal/auth/capabilities.go. These string
// constants are copied verbatim from the Go const blocks. The backend
// computes the active role's capability list and ships it on
// /auth/me (the `capabilities` field); the frontend reads that list
// via useCan/<Can> for defense-in-depth gating only — the backend
// independently enforces every capability.
//
// IF YOU ADD/CHANGE A CAPABILITY: change capabilities.go FIRST, then
// mirror it here. Keep the IDs and grouping aligned with the Go file so
// a reviewer can diff the two side by side.

// Platform-level capabilities (UI Admin)
export const CAP_PLATFORM = {
  USERS_LIST: "platform.users.list",
  USERS_CREATE: "platform.users.create",
  USERS_UPDATE: "platform.users.update",
  USERS_DELETE: "platform.users.delete",
  POLICIES_LIST: "platform.policies.list",
  POLICIES_WRITE: "platform.policies.write",
  SERVICE_ACCOUNTS_LIST: "platform.service-accounts.list",
  SERVICE_ACCOUNTS_WRITE: "platform.service-accounts.write",
  AUDIT_READ: "platform.audit.read",
  SKINS_WRITE: "platform.skins.write",
  OIDC_WRITE: "platform.oidc.write",
  SYSTEM_WRITE: "platform.system.write",
  ONBOARDING_WRITE: "platform.onboarding.write",
  // Read counterparts (ADR-0009 Phase C) — GET-route gates.
  SYSTEM_READ: "platform.system.read",
  OIDC_READ: "platform.oidc.read",
  SKINS_READ: "platform.skins.read",
  ONBOARDING_READ: "platform.onboarding.read",
} as const;

// Cluster wiring (UI Admin — basement<->cluster connection).
// NOTE: cluster.wiring.read is the ONE wiring cap held by BOTH
// ui-admin (any cluster) and cluster-admin (their cluster only).
export const CAP_CLUSTER_WIRING = {
  LIST: "cluster.wiring.list",
  CREATE: "cluster.wiring.create",
  UPDATE: "cluster.wiring.update",
  DELETE: "cluster.wiring.delete",
  TEST: "cluster.wiring.test",
  READ: "cluster.wiring.read",
  BUCKETS_AGGREGATE: "cluster.buckets.aggregate",
  USAGE_AGGREGATE: "cluster.usage.aggregate",
} as const;

// Cluster contents (Cluster Admin — scoped to their assigned cluster).
// ui-admin does NOT hold any of these.
export const CAP_CLUSTER_CONTENTS = {
  READ: "cluster.contents.read",
  BUCKETS_CREATE: "cluster.buckets.create",
  BUCKETS_UPDATE: "cluster.buckets.update",
  BUCKETS_DELETE: "cluster.buckets.delete",
  BUCKETS_LIFECYCLE_WRITE: "cluster.buckets.lifecycle.write",
  LAYOUT_WRITE: "cluster.layout.write",
  SCRUB_WRITE: "cluster.scrub.write",
  KEYS_CREATE: "cluster.keys.create",
  KEYS_UPDATE: "cluster.keys.update",
  KEYS_ROTATE: "cluster.keys.rotate",
  KEYS_DELETE: "cluster.keys.delete",
  ENCRYPTION_ADMINS_LIST: "cluster.encryption-admins.list",
  ENCRYPTION_ADMINS_ADD: "cluster.encryption-admins.add",
  ENCRYPTION_ADMINS_REMOVE: "cluster.encryption-admins.remove",
  ENCRYPTION_UNLOCK: "cluster.encryption.unlock",
  ENCRYPTION_LOCK: "cluster.encryption.lock",
  GRANTS_LIST: "cluster.grants.list",
  GRANTS_WRITE: "cluster.grants.write",
} as const;

// User self-service (own resources, scoped by region tier).
export const CAP_SELF = {
  FILES_READ: "self.files.read",
  FILES_WRITE: "self.files.write",
  KEYS_LIST: "self.keys.list",
  KEYS_CREATE: "self.keys.create",
  KEYS_ROTATE: "self.keys.rotate",
  KEYS_DELETE: "self.keys.delete",
  SHARES_WRITE: "self.shares.write",
  BACKUPS_WRITE: "self.backups.write",
  FEDERATIONS_WRITE: "self.federations.write",
  WEBHOOKS_WRITE: "self.webhooks.write",
} as const;

// Flat map — convenient single-import access (e.g. CAP.CLUSTER_CONTENTS_READ).
export const CAP = {
  // platform
  PLATFORM_USERS_LIST: CAP_PLATFORM.USERS_LIST,
  PLATFORM_USERS_CREATE: CAP_PLATFORM.USERS_CREATE,
  PLATFORM_USERS_UPDATE: CAP_PLATFORM.USERS_UPDATE,
  PLATFORM_USERS_DELETE: CAP_PLATFORM.USERS_DELETE,
  PLATFORM_POLICIES_LIST: CAP_PLATFORM.POLICIES_LIST,
  PLATFORM_POLICIES_WRITE: CAP_PLATFORM.POLICIES_WRITE,
  PLATFORM_SERVICE_ACCOUNTS_LIST: CAP_PLATFORM.SERVICE_ACCOUNTS_LIST,
  PLATFORM_SERVICE_ACCOUNTS_WRITE: CAP_PLATFORM.SERVICE_ACCOUNTS_WRITE,
  PLATFORM_AUDIT_READ: CAP_PLATFORM.AUDIT_READ,
  PLATFORM_SKINS_WRITE: CAP_PLATFORM.SKINS_WRITE,
  PLATFORM_OIDC_WRITE: CAP_PLATFORM.OIDC_WRITE,
  PLATFORM_SYSTEM_WRITE: CAP_PLATFORM.SYSTEM_WRITE,
  PLATFORM_ONBOARDING_WRITE: CAP_PLATFORM.ONBOARDING_WRITE,
  PLATFORM_SYSTEM_READ: CAP_PLATFORM.SYSTEM_READ,
  PLATFORM_OIDC_READ: CAP_PLATFORM.OIDC_READ,
  PLATFORM_SKINS_READ: CAP_PLATFORM.SKINS_READ,
  PLATFORM_ONBOARDING_READ: CAP_PLATFORM.ONBOARDING_READ,
  // cluster wiring
  CLUSTER_WIRING_LIST: CAP_CLUSTER_WIRING.LIST,
  CLUSTER_WIRING_CREATE: CAP_CLUSTER_WIRING.CREATE,
  CLUSTER_WIRING_UPDATE: CAP_CLUSTER_WIRING.UPDATE,
  CLUSTER_WIRING_DELETE: CAP_CLUSTER_WIRING.DELETE,
  CLUSTER_WIRING_TEST: CAP_CLUSTER_WIRING.TEST,
  CLUSTER_WIRING_READ: CAP_CLUSTER_WIRING.READ,
  CLUSTER_BUCKETS_AGGREGATE: CAP_CLUSTER_WIRING.BUCKETS_AGGREGATE,
  CLUSTER_USAGE_AGGREGATE: CAP_CLUSTER_WIRING.USAGE_AGGREGATE,
  // cluster contents
  CLUSTER_CONTENTS_READ: CAP_CLUSTER_CONTENTS.READ,
  CLUSTER_BUCKETS_CREATE: CAP_CLUSTER_CONTENTS.BUCKETS_CREATE,
  CLUSTER_BUCKETS_UPDATE: CAP_CLUSTER_CONTENTS.BUCKETS_UPDATE,
  CLUSTER_BUCKETS_DELETE: CAP_CLUSTER_CONTENTS.BUCKETS_DELETE,
  CLUSTER_BUCKETS_LIFECYCLE_WRITE: CAP_CLUSTER_CONTENTS.BUCKETS_LIFECYCLE_WRITE,
  CLUSTER_LAYOUT_WRITE: CAP_CLUSTER_CONTENTS.LAYOUT_WRITE,
  CLUSTER_SCRUB_WRITE: CAP_CLUSTER_CONTENTS.SCRUB_WRITE,
  CLUSTER_KEYS_CREATE: CAP_CLUSTER_CONTENTS.KEYS_CREATE,
  CLUSTER_KEYS_UPDATE: CAP_CLUSTER_CONTENTS.KEYS_UPDATE,
  CLUSTER_KEYS_ROTATE: CAP_CLUSTER_CONTENTS.KEYS_ROTATE,
  CLUSTER_KEYS_DELETE: CAP_CLUSTER_CONTENTS.KEYS_DELETE,
  CLUSTER_ENCRYPTION_ADMINS_LIST: CAP_CLUSTER_CONTENTS.ENCRYPTION_ADMINS_LIST,
  CLUSTER_ENCRYPTION_ADMINS_ADD: CAP_CLUSTER_CONTENTS.ENCRYPTION_ADMINS_ADD,
  CLUSTER_ENCRYPTION_ADMINS_REMOVE: CAP_CLUSTER_CONTENTS.ENCRYPTION_ADMINS_REMOVE,
  CLUSTER_ENCRYPTION_UNLOCK: CAP_CLUSTER_CONTENTS.ENCRYPTION_UNLOCK,
  CLUSTER_ENCRYPTION_LOCK: CAP_CLUSTER_CONTENTS.ENCRYPTION_LOCK,
  CLUSTER_GRANTS_LIST: CAP_CLUSTER_CONTENTS.GRANTS_LIST,
  CLUSTER_GRANTS_WRITE: CAP_CLUSTER_CONTENTS.GRANTS_WRITE,
  // self
  SELF_FILES_READ: CAP_SELF.FILES_READ,
  SELF_FILES_WRITE: CAP_SELF.FILES_WRITE,
  SELF_KEYS_LIST: CAP_SELF.KEYS_LIST,
  SELF_KEYS_CREATE: CAP_SELF.KEYS_CREATE,
  SELF_KEYS_ROTATE: CAP_SELF.KEYS_ROTATE,
  SELF_KEYS_DELETE: CAP_SELF.KEYS_DELETE,
  SELF_SHARES_WRITE: CAP_SELF.SHARES_WRITE,
  SELF_BACKUPS_WRITE: CAP_SELF.BACKUPS_WRITE,
  SELF_FEDERATIONS_WRITE: CAP_SELF.FEDERATIONS_WRITE,
  SELF_WEBHOOKS_WRITE: CAP_SELF.WEBHOOKS_WRITE,
} as const;

export type Capability = (typeof CAP)[keyof typeof CAP];
