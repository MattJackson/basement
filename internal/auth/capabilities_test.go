package auth

import "testing"

func TestCapabilitiesForReturnsCopy(t *testing.T) {
	ar := &ActiveRole{Kind: "ui-admin"}
	a := CapabilitiesFor(ar)
	b := CapabilitiesFor(ar)
	if len(a) == 0 {
		t.Fatal("expected ui-admin capabilities to be non-empty")
	}
	a[0] = "MUTATED"
	if b[0] == "MUTATED" {
		t.Fatal("CapabilitiesFor returned a shared slice; callers can mutate the role-capability map")
	}
}

func TestCapabilitiesForUnknownRole(t *testing.T) {
	if caps := CapabilitiesFor(&ActiveRole{Kind: "frog"}); caps != nil {
		t.Errorf("unknown role kind should return nil, got %v", caps)
	}
	if caps := CapabilitiesFor(nil); caps != nil {
		t.Errorf("nil active role should return nil, got %v", caps)
	}
}

func TestCanUIAdminPlatform(t *testing.T) {
	claims := &Claims{ActiveRole: &ActiveRole{Kind: "ui-admin"}}
	if !Can(claims, CapPlatformUsersCreate, "") {
		t.Error("ui-admin should be able to create users")
	}
	if !Can(claims, CapClusterWiringUpdate, "") {
		t.Error("ui-admin should be able to update cluster wiring")
	}
}

func TestCanUIAdminNotClusterContents(t *testing.T) {
	claims := &Claims{ActiveRole: &ActiveRole{Kind: "ui-admin"}}
	// The whole point of ADR-0009: UI Admin is NOT super-admin over
	// cluster contents. The wiring-vs-contents split lives or dies on
	// this assertion.
	if Can(claims, CapClusterBucketsCreate, "any") {
		t.Error("ui-admin must NOT have cluster.buckets.create — that's cluster-admin's scope")
	}
	if Can(claims, CapClusterEncryptionAdminsAdd, "any") {
		t.Error("ui-admin must NOT have cluster.encryption-admins.add")
	}
	if Can(claims, CapClusterContentsRead, "any") {
		t.Error("ui-admin must NOT read cluster contents")
	}
}

func TestCanClusterAdminScopedToCluster(t *testing.T) {
	claims := &Claims{ActiveRole: &ActiveRole{Kind: "cluster-admin", Cluster: "cluster-a"}}
	if !Can(claims, CapClusterBucketsCreate, "cluster-a") {
		t.Error("cluster-admin on cluster-a should be able to create buckets on cluster-a")
	}
	if Can(claims, CapClusterBucketsCreate, "cluster-b") {
		t.Error("cluster-admin on cluster-a must NOT have rights on cluster-b")
	}
	// Hardened (fail-closed): an empty clusterID for a cluster-scoped
	// capability is now DENIED rather than skipped. A route that resolves
	// an empty cid (e.g. a mis-named path param) must never collapse to an
	// unscoped global grant.
	if Can(claims, CapClusterBucketsCreate, "") {
		t.Error("empty clusterID must be denied for a cluster-scoped capability (fail-closed)")
	}
}

func TestCanClusterAdminNotWiring(t *testing.T) {
	claims := &Claims{ActiveRole: &ActiveRole{Kind: "cluster-admin", Cluster: "cluster-a"}}
	if Can(claims, CapClusterWiringUpdate, "cluster-a") {
		t.Error("cluster-admin must NOT have cluster.wiring.* — that's UI Admin")
	}
	if Can(claims, CapPlatformUsersCreate, "") {
		t.Error("cluster-admin must NOT have platform.* capabilities")
	}
}

func TestCanUser(t *testing.T) {
	claims := &Claims{ActiveRole: &ActiveRole{Kind: "user"}}
	if !Can(claims, CapSelfFilesRead, "") {
		t.Error("user should read own files")
	}
	if Can(claims, CapClusterBucketsCreate, "any") {
		t.Error("user must NOT have cluster capabilities")
	}
	if Can(claims, CapPlatformSystemWrite, "") {
		t.Error("user must NOT have platform capabilities")
	}
}

func TestCanNilClaims(t *testing.T) {
	if Can(nil, CapSelfFilesRead, "") {
		t.Error("nil claims must deny")
	}
	if Can(&Claims{}, CapSelfFilesRead, "") {
		t.Error("claims with nil active role must deny")
	}
}

func TestIsClusterScopedCapability(t *testing.T) {
	if !IsClusterScopedCapability(CapClusterBucketsCreate) {
		t.Error("cluster.buckets.create is cluster-scoped")
	}
	if IsClusterScopedCapability(CapClusterWiringUpdate) {
		t.Error("cluster.wiring.update is NOT cluster-scoped — it's UI Admin's, not cluster-admin's")
	}
	if IsClusterScopedCapability(CapPlatformUsersCreate) {
		t.Error("platform.users.create is NOT cluster-scoped")
	}
}

// TestWiringReadSharedByBothRoles locks the ADR-0009 Phase C decision
// that cluster.wiring.read is the one wiring capability held by both
// personas: ui-admin reads any cluster's connection record, cluster-admin
// only their own. Because it lives in the cluster-admin grant it is
// cluster-scoped — which is exactly what scopes the cluster-admin side.
func TestWiringReadSharedByBothRoles(t *testing.T) {
	if !IsClusterScopedCapability(CapClusterWiringRead) {
		t.Error("cluster.wiring.read must be cluster-scoped so cluster-admin@X can't read cluster Y")
	}

	uiAdmin := &Claims{ActiveRole: &ActiveRole{Kind: "ui-admin"}}
	if !Can(uiAdmin, CapClusterWiringRead, "cluster-a") || !Can(uiAdmin, CapClusterWiringRead, "cluster-z") {
		t.Error("ui-admin should read wiring on ANY cluster")
	}

	clusterAdmin := &Claims{ActiveRole: &ActiveRole{Kind: "cluster-admin", Cluster: "cluster-a"}}
	if !Can(clusterAdmin, CapClusterWiringRead, "cluster-a") {
		t.Error("cluster-admin@a should read cluster-a's wiring record")
	}
	if Can(clusterAdmin, CapClusterWiringRead, "cluster-b") {
		t.Error("cluster-admin@a must NOT read cluster-b's wiring record")
	}
}

// TestClusterContentsWriteCapsClusterAdminOnly covers the Phase C write
// caps the ADR matrix did not enumerate (layout/scrub/keys.update). They
// are cluster-admin-only and scoped — ui-admin must never hold them, or
// the super-admin leak class reappears for those routes.
func TestClusterContentsWriteCapsClusterAdminOnly(t *testing.T) {
	uiAdmin := &Claims{ActiveRole: &ActiveRole{Kind: "ui-admin"}}
	clusterAdmin := &Claims{ActiveRole: &ActiveRole{Kind: "cluster-admin", Cluster: "cluster-a"}}

	for _, cap := range []string{CapClusterLayoutWrite, CapClusterScrubWrite, CapClusterKeysUpdate} {
		if Can(uiAdmin, cap, "cluster-a") {
			t.Errorf("ui-admin must NOT hold %s — it's a cluster-admin contents write", cap)
		}
		if !Can(clusterAdmin, cap, "cluster-a") {
			t.Errorf("cluster-admin@a should hold %s on cluster-a", cap)
		}
		if Can(clusterAdmin, cap, "cluster-b") {
			t.Errorf("cluster-admin@a must NOT hold %s on cluster-b", cap)
		}
		if !IsClusterScopedCapability(cap) {
			t.Errorf("%s must be cluster-scoped", cap)
		}
	}
}

// TestPlatformReadCapsUIAdminOnly covers the Phase C platform read caps
// added so the GET routes can gate without borrowing the write cap.
func TestPlatformReadCapsUIAdminOnly(t *testing.T) {
	uiAdmin := &Claims{ActiveRole: &ActiveRole{Kind: "ui-admin"}}
	clusterAdmin := &Claims{ActiveRole: &ActiveRole{Kind: "cluster-admin", Cluster: "cluster-a"}}

	for _, cap := range []string{CapPlatformSystemRead, CapPlatformOIDCRead, CapPlatformSkinsRead, CapPlatformOnboardingRead} {
		if !Can(uiAdmin, cap, "") {
			t.Errorf("ui-admin should hold %s", cap)
		}
		if Can(clusterAdmin, cap, "") {
			t.Errorf("cluster-admin must NOT hold platform read cap %s", cap)
		}
		if IsClusterScopedCapability(cap) {
			t.Errorf("%s is platform-wide, not cluster-scoped", cap)
		}
	}
}
