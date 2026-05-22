// Package api: migration helpers — ADR-0001 cycle v0.9.0h, retired
// in ADR-0002 cycle v1.1.0e.
//
// The orphan-creds migration was a one-shot tool that minted
// per-bucket BucketGrants for users out of legacy in-config S3
// credentials on each Connection. ADR-0002 retired the entire
// BucketGrant model in favour of per-user region keychains — so the
// migration target no longer exists. Both endpoints now return
// 410 GONE with a pointer to the replacement flow (the user pastes
// the credentials into /files/regions/new themselves).
//
// The HTTP surface is preserved so any operator scripts or saved
// links still get a coherent answer instead of a 404 they'd have to
// debug. The wire shapes (orphanCred, etc.) are kept in case a
// future cycle ressurects the helper for a different migration.
package api

import (
	"net/http"
)

// orphanCred is the (now-frozen) per-Connection wire shape that GET
// /admin/migrations/orphan_creds used to return. Kept for future
// migrations that may need a similar shape, even though the v0.9.0h
// implementation is retired.
type orphanCred struct {
	ConnectionID      string   `json:"connectionId"`
	Label             string   `json:"label"`
	Driver            string   `json:"driver"`
	AccessKeyID       string   `json:"accessKeyId"`
	HasSecretKey      bool     `json:"hasSecretKey"`
	DiscoveredBuckets []string `json:"discoveredBuckets"`
}

// orphanCredsResponse mirrors the historical GET shape (always-non-nil
// slice). Reserved for a future repurpose of this endpoint.
type orphanCredsResponse struct {
	Orphans []orphanCred `json:"orphans"`
}

// migrateOrphanCredsRequest is the (frozen) POST body shape.
type migrateOrphanCredsRequest struct {
	UserID        string   `json:"userId"`
	BucketAliases []string `json:"bucketAliases"`
}

// migrateOrphanCredsResponse is the (frozen) POST response shape.
type migrateOrphanCredsResponse struct {
	GrantsCreated     int  `json:"grantsCreated"`
	ConnectionUpdated bool `json:"connectionUpdated"`
}

// listOrphanCredsHandler returns 410 GONE — the BucketGrant model
// the v0.9.0h helper targeted no longer exists. Users now self-serve
// credentials via /files/regions/new (POST /api/v1/user/regions).
func (s *Server) listOrphanCredsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}
	if _, ok := s.requireCapability(w, r, "host:manage_policies", "host:*"); !ok {
		return
	}
	writeErrorSimple(w, http.StatusGone, "MIGRATION_RETIRED",
		"The orphan-creds migration helper was retired in v1.1.0e. "+
			"Users now self-serve credentials via /files/regions/new.")
}

// migrateOrphanCredsHandler returns 410 GONE — see listOrphanCredsHandler.
func (s *Server) migrateOrphanCredsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}
	if _, ok := s.requireCapability(w, r, "host:manage_policies", "host:*"); !ok {
		return
	}
	writeErrorSimple(w, http.StatusGone, "MIGRATION_RETIRED",
		"The orphan-creds migration helper was retired in v1.1.0e. "+
			"Users now self-serve credentials via /files/regions/new.")
}
