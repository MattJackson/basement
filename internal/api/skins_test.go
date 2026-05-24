// Package api: tests for GET /api/v1/skins (v1.13.0a, ADR-0008).
//
// Coverage: returns the registry roster in alphabetical order, with
// at least basement-default present; returns 503 SKINS_NOT_WIRED
// when the registry hasn't been wired; requires authentication (the
// endpoint sits in the authG group so an unauthenticated request
// 401s before the handler runs).

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/skin"
	"github.com/mattjackson/basement/internal/store"
)

func TestListSkinsHandler_ReturnsBuiltInDefault(t *testing.T) {
	st, err := store.Open(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	srv := New(newTestConfig(), st, nil, nil, nil)

	reg := skin.New()
	if err := reg.Register(skin.BuiltInDefault()); err != nil {
		t.Fatalf("register basement-default: %v", err)
	}
	srv.SetSkinRegistry(reg)

	req := createAuthRequest(http.MethodGet, "/api/v1/skins")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	type skinListItem struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Version     string `json:"version"`
		Swatch      string `json:"swatch"`
		BuiltIn     bool   `json:"builtIn"`
	}
	var out []skinListItem
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, rr.Body.String())
	}
	if len(out) != 1 {
		t.Fatalf("want 1 skin (basement-default), got %d: %+v", len(out), out)
	}
	if out[0].Name != skin.BuiltInDefaultName {
		t.Errorf("Name: got %q, want %q", out[0].Name, skin.BuiltInDefaultName)
	}
	if out[0].DisplayName == "" {
		t.Errorf("DisplayName: empty after round-trip")
	}
	if out[0].Swatch == "" {
		t.Errorf("Swatch (primary color): empty after round-trip")
	}
}

func TestListSkinsHandler_MultipleSkins_AlphabeticalOrder(t *testing.T) {
	st, err := store.Open(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	srv := New(newTestConfig(), st, nil, nil, nil)

	reg := skin.New()
	// Register out-of-order so a non-sorting impl would fail.
	for _, name := range []string{"zeta", "basement-default", "acme"} {
		s := skin.BuiltInDefault()
		s.Name = name
		if err := reg.Register(s); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	srv.SetSkinRegistry(reg)

	req := createAuthRequest(http.MethodGet, "/api/v1/skins")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	type skinListItem struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Version     string `json:"version"`
		Swatch      string `json:"swatch"`
		BuiltIn     bool   `json:"builtIn"`
	}
	var out []skinListItem
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, rr.Body.String())
	}
	want := []string{"acme", "basement-default", "zeta"}
	if len(out) != len(want) {
		t.Fatalf("want %d skins, got %d", len(want), len(out))
	}
	for i, name := range want {
		if out[i].Name != name {
			t.Errorf("out[%d].Name: got %q, want %q", i, out[i].Name, name)
		}
	}
}

func TestListSkinsHandler_NoRegistry_503(t *testing.T) {
	st, err := store.Open(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	srv := New(newTestConfig(), st, nil, nil, nil)
	// SetSkinRegistry deliberately NOT called.

	req := createAuthRequest(http.MethodGet, "/api/v1/skins")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s, want 503", rr.Code, rr.Body.String())
	}
	var er ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &er); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if er.Error.Code != "SKINS_NOT_WIRED" {
		t.Errorf("error code: want SKINS_NOT_WIRED, got %s", er.Error.Code)
	}
}

func TestListSkinsHandler_RequiresAuth(t *testing.T) {
	st, err := store.Open(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	srv := New(newTestConfig(), st, nil, nil, nil)
	reg := skin.New()
	_ = reg.Register(skin.BuiltInDefault())
	srv.SetSkinRegistry(reg)

	// Unauthenticated request — no cookie, no bearer.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/skins", nil)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK {
		t.Errorf("unauthenticated status=%d body=%s; want non-200", rr.Code, rr.Body.String())
	}
}

// v1.13.1: Test admin PATCH validation for AllowedUserSkins against installed skins

func TestUpdateOrgCapabilities_AllowedUserSkins_UnknownSkin_Rejected(t *testing.T) {
	st, err := store.Open(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	srv := New(newTestConfig(), st, nil, nil, nil)

	reg := skin.New()
	if err := reg.Register(skin.BuiltInDefault()); err != nil {
		t.Fatalf("register basement-default: %v", err)
	}
	srv.SetSkinRegistry(reg)

	reqBody, _ := json.Marshal(store.OrgCapabilities{
		UserOverridableSkin: true,
		AllowedUserSkins:    []string{"nonexistent-skin"},
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/system", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	addAdminCookie(req)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d body=%s; want 400 for unknown skin in AllowedUserSkins", rr.Code, rr.Body.String())
	}
}

func TestUpdateOrgCapabilities_AllowedUserSkins_ValidSkins_Accepted(t *testing.T) {
	st, err := store.Open(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	srv := New(newTestConfig(), st, nil, nil, nil)

	reg := skin.New()
	defaultSkin := skin.BuiltInDefault()
	customSkin := defaultSkin
	customSkin.Name = "custom-skin"
	if err := reg.Register(defaultSkin); err != nil {
		t.Fatalf("register basement-default: %v", err)
	}
	if err := reg.Register(customSkin); err != nil {
		t.Fatalf("register custom-skin: %v", err)
	}
	srv.SetSkinRegistry(reg)

	reqBody, _ := json.Marshal(store.OrgCapabilities{
		UserOverridableSkin: true,
		AllowedUserSkins:    []string{"basement-default", "custom-skin"},
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/system", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	addAdminCookie(req)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status=%d body=%s; want 200 for valid AllowedUserSkins", rr.Code, rr.Body.String())
	}
}
