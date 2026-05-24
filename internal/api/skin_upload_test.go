// Package api: Tests for skin upload and management endpoints (v1.13.0b).

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/skin"
)

// withChiParam injects a chi URL param into the request context. The
// skin handlers call chi.URLParam(r, "id") and the tests bypass the
// router, so without this they all 400 on missing-id. Helper added
// v1.11.0.33 (test infra fix).
func withChiParam(req *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestUploadSkinHandler_ValidFile tests successful skin upload.
func TestUploadSkinHandler_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{DataDir: tmpDir}
	srv := setupTestServerWithConfig(cfg, t)

	testSkin := skin.BuiltInHighContrast()
	testSkin.Name = "test-valid-file-upload"
	skinData, err := json.Marshal(testSkin)
	if err != nil {
		t.Fatalf("Failed to marshal test skin: %v", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "test-skin.basement-skin.json")
	part.Write(skinData)
	writer.Close()

	req := createAuthRequest(http.MethodPost, "/api/v1/admin/skins/upload")
	req.Body = io.NopCloser(body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rw := httptest.NewRecorder()

	srv.uploadSkinHandler(rw, req)

	if rw.Code != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(rw.Body)
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusCreated, rw.Code, string(bodyBytes))
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response JSON: %v", err)
	}

	if resp["success"] != true {
		t.Error("Expected success=true in response")
	}
}

// TestUploadSkinHandler_InvalidExtension tests rejection of non-json files.
func TestUploadSkinHandler_InvalidExtension(t *testing.T) {
	srv := setupTestServerWithConfig(&config.Config{DataDir: t.TempDir()}, t)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "test.txt")
	part.Write([]byte("not a skin"))
	writer.Close()

	req := createAuthRequest(http.MethodPost, "/api/v1/admin/skins/upload")
	req.Body = io.NopCloser(body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rw := httptest.NewRecorder()

	srv.uploadSkinHandler(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid extension, got %d",
			http.StatusBadRequest, rw.Code)
	}
}

// TestUploadSkinHandler_InvalidJSON tests rejection of malformed JSON.
func TestUploadSkinHandler_InvalidJSON(t *testing.T) {
	srv := setupTestServerWithConfig(&config.Config{DataDir: t.TempDir()}, t)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "test.basement-skin.json")
	part.Write([]byte("{ invalid json }"))
	writer.Close()

	req := createAuthRequest(http.MethodPost, "/api/v1/admin/skins/upload")
	req.Body = io.NopCloser(body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rw := httptest.NewRecorder()

	srv.uploadSkinHandler(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for invalid JSON, got %d",
			http.StatusBadRequest, rw.Code)
	}
}

// TestUploadSkinHandler_DuplicateName tests rejection of duplicate skin names.
func TestUploadSkinHandler_DuplicateName(t *testing.T) {
	tmpDir := t.TempDir()
	srv := setupTestServerWithConfig(&config.Config{DataDir: tmpDir}, t)

	// First upload succeeds
	testSkin := skin.BuiltInHighContrast()
	testSkin.Name = "test-duplicate-name-check"
	skinData, _ := json.Marshal(testSkin)
	body1 := &bytes.Buffer{}
	writer1 := multipart.NewWriter(body1)
	part1, _ := writer1.CreateFormFile("file", "test-skin.basement-skin.json")
	part1.Write(skinData)
	writer1.Close()

	req1 := createAuthRequest(http.MethodPost, "/api/v1/admin/skins/upload")
	req1.Body = io.NopCloser(body1)
	req1.Header.Set("Content-Type", writer1.FormDataContentType())
	rw1 := httptest.NewRecorder()
	srv.uploadSkinHandler(rw1, req1)

	if rw1.Code != http.StatusCreated {
		t.Fatalf("First upload should succeed: got %d", rw1.Code)
	}

	// Second upload with same name fails
	body2 := &bytes.Buffer{}
	writer2 := multipart.NewWriter(body2)
	part2, _ := writer2.CreateFormFile("file", "test-skin.basement-skin.json")
	part2.Write(skinData)
	writer2.Close()

	req2 := createAuthRequest(http.MethodPost, "/api/v1/admin/skins/upload")
	req2.Body = io.NopCloser(body2)
	req2.Header.Set("Content-Type", writer2.FormDataContentType())
	rw2 := httptest.NewRecorder()
	srv.uploadSkinHandler(rw2, req2)

	if rw2.Code != http.StatusConflict {
		t.Errorf("Expected status %d for duplicate name, got %d",
			http.StatusConflict, rw2.Code)
	}
}

// TestGetSkinPolicyHandler tests getting skin policy.
func TestGetSkinPolicyHandler(t *testing.T) {
	tmpDir := t.TempDir()
	srv := setupTestServerWithConfig(&config.Config{DataDir: tmpDir}, t)

	dataDir := filepath.Join(tmpDir, "skins")
	os.MkdirAll(dataDir, 0755)

	policyPath := filepath.Join(dataDir, "test-skin.policy.json")
	policy := SkinPolicy{Public: true, CORSOrigin: "https://example.com"}
	_ = os.WriteFile(policyPath, []byte(""), 0644) // Create file first
	
	jsonBytes, _ := json.MarshalIndent(policy, "", "  ")
	os.WriteFile(policyPath, jsonBytes, 0644)

	req := withChiParam(createAuthRequest(http.MethodGet, "/api/v1/admin/skins/test-skin/policy"), "id", "test-skin")
	rw := httptest.NewRecorder()

	srv.getSkinPolicyHandler(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rw.Code)
	}

	var resp SkinPolicy
	json.Unmarshal(rw.Body.Bytes(), &resp)
	if !resp.Public || resp.CORSOrigin != "https://example.com" {
		t.Error("Policy not returned correctly")
	}
}

// TestUpdateSkinPolicyHandler tests updating skin policy.
func TestUpdateSkinPolicyHandler(t *testing.T) {
	tmpDir := t.TempDir()
	srv := setupTestServerWithConfig(&config.Config{DataDir: tmpDir}, t)

	dataDir := filepath.Join(tmpDir, "skins")
	os.MkdirAll(dataDir, 0755)

	policyPath := filepath.Join(dataDir, "test-skin.policy.json")
	initialPolicy := SkinPolicy{Public: false}
	_ = os.WriteFile(policyPath, []byte(""), 0644) // Create file first
	
	jsonBytes, _ := json.MarshalIndent(initialPolicy, "", "  ")
	os.WriteFile(policyPath, jsonBytes, 0644)

	newPolicy := SkinPolicy{Public: true, CORSOrigin: "https://new.com"}
	policyJSON, _ := json.Marshal(newPolicy)

	body := bytes.NewReader(policyJSON)
	req := withChiParam(createAuthRequest(http.MethodPut, "/api/v1/admin/skins/test-skin/policy"), "id", "test-skin")
	req.Body = io.NopCloser(body)
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	srv.updateSkinPolicyHandler(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rw.Code)
	}

	var resp SkinPolicy
	json.Unmarshal(rw.Body.Bytes(), &resp)
	if !resp.Public || resp.CORSOrigin != "https://new.com" {
		t.Error("Policy not updated correctly")
	}

	_ = policyJSON // Suppress unused warning
}

// TestActivateSkinHandler tests activating a skin.
func TestActivateSkinHandler(t *testing.T) {
	srv := setupTestServerWithConfig(&config.Config{DataDir: t.TempDir()}, t)

	req := withChiParam(createAuthRequest(http.MethodPut, "/api/v1/admin/skins/basement-default/activate"), "id", "basement-default")
	rw := httptest.NewRecorder()

	srv.activateSkinHandler(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, rw.Code, rw.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(rw.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("Expected success=true in response")
	}
}

// TestDeleteSkinHandler tests deleting a skin.
func TestDeleteSkinHandler(t *testing.T) {
	tmpDir := t.TempDir()
	srv := setupTestServerWithConfig(&config.Config{DataDir: tmpDir}, t)

	dataDir := filepath.Join(tmpDir, "skins")
	os.MkdirAll(dataDir, 0755)

	// Create a test skin file
	skinPath := filepath.Join(dataDir, "test-skin.basement-skin.json")
	skinData, _ := json.Marshal(skin.BuiltInMinimal())
	os.WriteFile(skinPath, skinData, 0644)

	req := withChiParam(createAuthRequest(http.MethodDelete, "/api/v1/admin/skins/test-skin"), "id", "test-skin")
	rw := httptest.NewRecorder()

	srv.deleteSkinHandler(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rw.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(rw.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Error("Expected success=true in response")
	}

	// Verify file was deleted
	if _, err := os.Stat(skinPath); !os.IsNotExist(err) {
		t.Error("Skin file should have been deleted")
	}
}

// TestListAdminSkinsHandler tests listing all skins with policy info.
func TestListAdminSkinsHandler(t *testing.T) {
	srv := setupTestServerWithConfig(&config.Config{DataDir: t.TempDir()}, t)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/skins")
	rw := httptest.NewRecorder()

	srv.listAdminSkinsHandler(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rw.Code)
	}

	var resp []map[string]interface{}
	json.Unmarshal(rw.Body.Bytes(), &resp)
	
	// Should include at least basement-default and built-in skins
	if len(resp) < 5 {
		t.Errorf("Expected at least 5 skins (default + 4 built-in), got %d", len(resp))
	}
}

// TestValidateSkinJSON tests the skin validation function.
func TestValidateSkinJSON(t *testing.T) {
	validSkin := skin.BuiltInHighContrast()
	skinData, _ := json.Marshal(validSkin)

	got, err := validateSkinJSON(skinData)
	if err != nil {
		t.Fatalf("validateSkinJSON failed: %v", err)
	}
	if got.Name != validSkin.Name {
		t.Errorf("Name mismatch: got %s, want %s", got.Name, validSkin.Name)
	}

	// Test invalid JSON
	_, err = validateSkinJSON([]byte("{ invalid }"))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}

	// Test missing palette
	invalidSkin := map[string]interface{}{
		"name": "test",
		"palette": map[string]interface{}{},
	}
	skinData, _ = json.Marshal(invalidSkin)
	_, err = validateSkinJSON(skinData)
	if err == nil {
		t.Error("Expected error for missing palette")
	}

	// Test invalid density
	invalidSkin["palette"] = map[string]interface{}{
		"light":  map[string]string{"primary": "100 50% 50%"},
		"dark":   map[string]string{"primary": "200 60% 40%"},
	}
	invalidSkin["density"] = "invalid-density"
	skinData, _ = json.Marshal(invalidSkin)
	_, err = validateSkinJSON(skinData)
	if err == nil {
		t.Error("Expected error for invalid density")
	}
}

// TestActivateSkinHandler_NotFound tests activating non-existent skin.
func TestActivateSkinHandler_NotFound(t *testing.T) {
	srv := setupTestServerWithConfig(&config.Config{DataDir: t.TempDir()}, t)

	req := withChiParam(createAuthRequest(http.MethodPut, "/api/v1/admin/skins/nonexistent/activate"), "id", "nonexistent")
	rw := httptest.NewRecorder()

	srv.activateSkinHandler(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Errorf("Expected status %d for non-existent skin, got %d",
			http.StatusNotFound, rw.Code)
	}
}

// TestDeleteSkinHandler_NotFound tests deleting non-existent skin.
func TestDeleteSkinHandler_NotFound(t *testing.T) {
	srv := setupTestServerWithConfig(&config.Config{DataDir: t.TempDir()}, t)

	req := withChiParam(createAuthRequest(http.MethodDelete, "/api/v1/admin/skins/nonexistent"), "id", "nonexistent")
	rw := httptest.NewRecorder()

	srv.deleteSkinHandler(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Errorf("Expected status %d for non-existent skin, got %d",
			http.StatusNotFound, rw.Code)
	}
}

// TestMethodNotSupported tests that only valid HTTP methods are accepted.
func TestMethodNotSupported(t *testing.T) {
	srv := setupTestServerWithConfig(&config.Config{DataDir: t.TempDir()}, t)

	// Test upload endpoint rejects non-POST methods
	req := createAuthRequest(http.MethodGet, "/api/v1/admin/skins/upload")
	rw := httptest.NewRecorder()
	srv.uploadSkinHandler(rw, req)
	if rw.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 for GET on upload endpoint")
	}

	// Test activate rejects non-PUT methods
	req = createAuthRequest(http.MethodDelete, "/api/v1/admin/skins/test/activate")
	rw = httptest.NewRecorder()
	srv.activateSkinHandler(rw, req)
	if rw.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 for DELETE on activate endpoint")
	}

	// Test delete rejects non-DELETE methods
	req = createAuthRequest(http.MethodGet, "/api/v1/admin/skins/test")
	rw = httptest.NewRecorder()
	srv.deleteSkinHandler(rw, req)
	if rw.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 for GET on delete endpoint")
	}

	// Test policy endpoints reject non-GET/PUT methods
	req = createAuthRequest(http.MethodPost, "/api/v1/admin/skins/test/policy")
	rw = httptest.NewRecorder()
	srv.getSkinPolicyHandler(rw, req)
	if rw.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 for POST on get policy endpoint")
	}
}

// TestSkinNameRegex validates the skin name regex pattern.
func TestSkinNameRegex(t *testing.T) {
	validNames := []string{
		"test",
		"a-b-c",
		"basement-95",
		"my-skin-name",
	}

	invalidNames := []string{
		"",           // empty
		"A-B-C",      // uppercase
		"skin with spaces",
		"-starts-dash",
		"ends-with-",
	}

	for _, name := range validNames {
		if !skinNameRegex.MatchString(name) {
			t.Errorf("Expected %q to be valid skin name", name)
		}
	}

	for _, name := range invalidNames {
		if skinNameRegex.MatchString(name) {
			t.Errorf("Expected %q to be invalid skin name", name)
		}
	}
}

// TestSkinUploadWithPolicy tests uploading a skin with policy options.
func TestSkinUploadWithPolicy(t *testing.T) {
	tmpDir := t.TempDir()
	srv := setupTestServerWithConfig(&config.Config{DataDir: tmpDir}, t)

	testSkin := skin.BuiltInMinimal()
	testSkin.Name = "test-skin-with-policy"
	skinData, _ := json.Marshal(testSkin)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "policy-test.basement-skin.json")
	part.Write(skinData)
	writer.WriteField("policy.public", "false")
	writer.WriteField("policy.corsOrigin", "https://trusted.com")
	writer.Close()

	req := createAuthRequest(http.MethodPost, "/api/v1/admin/skins/upload")
	req.Body = io.NopCloser(body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rw := httptest.NewRecorder()

	srv.uploadSkinHandler(rw, req)

	if rw.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusCreated, rw.Code, rw.Body.String())
	}

	// Verify policy was saved — filename keyed by the skin's Name field,
	// not the form-upload filename
	dataDir := filepath.Join(tmpDir, "skins")
	policyPath := filepath.Join(dataDir, testSkin.Name+".policy.json")
	
	var policy SkinPolicy
	if data, err := os.ReadFile(policyPath); err == nil {
		json.Unmarshal(data, &policy)
		if policy.Public != false || policy.CORSOrigin != "https://trusted.com" {
			t.Error("Policy not saved correctly")
		}
	} else {
		t.Error("Policy file not created")
	}
}

// setupTestServerWithConfig creates a test server with given config.
func setupTestServerWithConfig(cfg *config.Config, t *testing.T) *Server {
	srv := New(cfg, nil, nil, nil, nil)
	
	// Register built-in skins for testing
	skinRegistry := skin.New()
	skin.RegisterBuiltInSkins(skinRegistry)
	srv.SetSkinRegistry(skinRegistry)

	return srv
}
