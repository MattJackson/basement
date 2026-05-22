// Package api: tests for POST /user/regions/bulk (v1.3.0d).
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUserRegionsBulk_HappyPath_CreatesAllRows(t *testing.T) {
	mock := newRegionMockDriver()
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	body := map[string]interface{}{
		"regions": []map[string]string{
			{
				"alias": "work", "endpoint": "https://s3.work.example.com",
				"accessKeyId": "GKwork", "secretKey": "secret-work", "region": "us-east-1",
			},
			{
				"alias": "home", "endpoint": "https://s3.home.example.com",
				"accessKeyId": "GKhome", "secretKey": "secret-home", "region": "us-east-1",
			},
			{
				"alias": "lab", "endpoint": "https://s3.lab.example.com",
				"accessKeyId": "GKlab", "secretKey": "secret-lab", "region": "us-east-1",
			},
		},
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/regions/bulk", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(req))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var resp userRegionsBulkResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Created) != 3 {
		t.Errorf("expected 3 created, got %d (errors=%v)", len(resp.Created), resp.Errors)
	}
	if len(resp.Errors) != 0 {
		t.Errorf("expected no errors, got %v", resp.Errors)
	}
	// No plaintext secrets in response
	raw := rr.Body.String()
	if bytes.Contains([]byte(raw), []byte("secret-work")) {
		t.Errorf("response leaked plaintext secret: %s", raw)
	}
}

func TestUserRegionsBulk_PerRowError_DoesNotAbortBatch(t *testing.T) {
	mock := newRegionMockDriver()
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	body := map[string]interface{}{
		"regions": []map[string]string{
			{
				"alias": "ok1", "endpoint": "https://s3.one.example.com",
				"accessKeyId": "GK1", "secretKey": "s1",
			},
			{
				// Missing alias — INVALID_REQUEST
				"alias": "", "endpoint": "https://s3.two.example.com",
				"accessKeyId": "GK2", "secretKey": "s2",
			},
			{
				// Malformed endpoint — INVALID_ENDPOINT
				"alias": "bad-ep", "endpoint": "not-a-url",
				"accessKeyId": "GK3", "secretKey": "s3",
			},
			{
				"alias": "ok2", "endpoint": "https://s3.four.example.com",
				"accessKeyId": "GK4", "secretKey": "s4",
			},
		},
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/regions/bulk", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(req))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var resp userRegionsBulkResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp.Created) != 2 {
		t.Errorf("expected 2 created, got %d (errors=%v)", len(resp.Created), resp.Errors)
	}
	if len(resp.Errors) != 2 {
		t.Fatalf("expected 2 errors, got %d (%v)", len(resp.Errors), resp.Errors)
	}
	// Verify index + error code mapping
	want := map[int]string{1: "INVALID_REQUEST", 2: "INVALID_ENDPOINT"}
	for _, e := range resp.Errors {
		w, ok := want[e.Index]
		if !ok {
			t.Errorf("unexpected error index %d: %+v", e.Index, e)
			continue
		}
		if e.Error != w {
			t.Errorf("index %d: error=%q, want %q", e.Index, e.Error, w)
		}
	}
}

func TestUserRegionsBulk_DuplicateDetected(t *testing.T) {
	mock := newRegionMockDriver()
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	// Two rows with identical (alias, endpoint) → first wins, second
	// fails with DUPLICATE_REGION but doesn't abort.
	body := map[string]interface{}{
		"regions": []map[string]string{
			{
				"alias": "home", "endpoint": "https://s3.dup.example.com",
				"accessKeyId": "GK1", "secretKey": "s1",
			},
			{
				"alias": "home", "endpoint": "https://s3.dup.example.com",
				"accessKeyId": "GK2", "secretKey": "s2",
			},
			{
				"alias": "other", "endpoint": "https://s3.dup.example.com",
				"accessKeyId": "GK3", "secretKey": "s3",
			},
		},
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/regions/bulk", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(req))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var resp userRegionsBulkResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp.Created) != 2 {
		t.Errorf("expected 2 created, got %d", len(resp.Created))
	}
	if len(resp.Errors) != 1 {
		t.Fatalf("expected 1 duplicate error, got %d", len(resp.Errors))
	}
	if resp.Errors[0].Error != "DUPLICATE_REGION" {
		t.Errorf("expected DUPLICATE_REGION, got %q", resp.Errors[0].Error)
	}
	if resp.Errors[0].Index != 1 {
		t.Errorf("expected duplicate at index 1, got %d", resp.Errors[0].Index)
	}
}

func TestUserRegionsBulk_EmptyArray_Rejected(t *testing.T) {
	mock := newRegionMockDriver()
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	body, _ := json.Marshal(map[string]interface{}{"regions": []interface{}{}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/regions/bulk", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(req))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty regions, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestUserRegionsBulk_HonorsAddressingStyle(t *testing.T) {
	mock := newRegionMockDriver()
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	body := map[string]interface{}{
		"regions": []map[string]string{
			{
				"alias": "aws", "endpoint": "https://s3.us-east-1.amazonaws.com",
				"accessKeyId": "GKaws", "secretKey": "saws",
				"addressingStyle": "virtual_host",
			},
			{
				"alias": "lan", "endpoint": "http://10.0.0.5:9000",
				"accessKeyId": "GKlan", "secretKey": "slan",
				// default → path
			},
		},
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/regions/bulk", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(req))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp userRegionsBulkResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp.Created) != 2 {
		t.Fatalf("expected 2 created, got %d (errors=%v)", len(resp.Created), resp.Errors)
	}
	// Match by alias
	for _, c := range resp.Created {
		switch c.Alias {
		case "aws":
			if c.AddressingStyle != "virtual_host" {
				t.Errorf("aws row: expected virtual_host, got %q", c.AddressingStyle)
			}
		case "lan":
			if c.AddressingStyle != "path" {
				t.Errorf("lan row: expected path default, got %q", c.AddressingStyle)
			}
		}
	}
}
