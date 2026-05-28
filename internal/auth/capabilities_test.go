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
	// Passing empty clusterID skips scoping check — useful for tests
	// of role-level grants, but production callers should always pass
	// the target cluster.
	if !Can(claims, CapClusterBucketsCreate, "") {
		t.Error("empty clusterID should skip cluster scoping")
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
