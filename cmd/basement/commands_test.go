// Package main — commands_test.go runs each subcommand against a
// stub httptest server, asserting both the wire request shape and
// the rendered stdout output. Tests share a tiny harness (cmdHarness)
// that wires the cobra root to a captured buffer + a profile pointing
// at the test server.
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type harness struct {
	srv     *httptest.Server
	stdout  *bytes.Buffer
	stderr  *bytes.Buffer
	cfgPath string
}

// newHarness sets up a fresh temp config + httptest server. The
// returned harness installs a profile "default" pointing at the
// server. Tests pass their own mux handler in.
func newHarness(t *testing.T, handler http.Handler) *harness {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	t.Setenv("BASEMENT_CONFIG", cfgPath)
	t.Setenv("BASEMENT_SECRET_KEY", "")
	t.Setenv("BASEMENT_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	cfg := &Config{Profiles: map[string]Profile{
		"default": {
			Endpoint:        srv.URL,
			AccessKeyID:     "BMNTtest0000000000000000",
			SecretKey:       "testsecret",
			CurrentRegionID: "rid-default",
		},
	}}
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	return &harness{
		srv:     srv,
		stdout:  &bytes.Buffer{},
		stderr:  &bytes.Buffer{},
		cfgPath: cfgPath,
	}
}

// run invokes the root command with the supplied args, capturing
// stdout/stderr into the harness buffers. The persistent flag state
// is reset back to zero between calls so tests don't leak state.
func (h *harness) run(t *testing.T, args ...string) error {
	t.Helper()
	// Reset flag-bound globals — cobra reads them via the persistent
	// flag pointers in main.go.
	ctx.profileFlag = ""
	ctx.regionFlag = ""
	ctx.outputFormatFlag = "table"

	root := newRootCmd()
	root.SetOut(h.stdout)
	root.SetErr(h.stderr)
	root.SetArgs(args)
	return root.Execute()
}

func TestVersionCommand(t *testing.T) {
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("version command must not call the server (got %s %s)", r.Method, r.URL.Path)
	}))
	if err := h.run(t, "version"); err != nil {
		t.Fatalf("version: %v", err)
	}
	if !strings.Contains(h.stdout.String(), "basement") {
		t.Errorf("expected version output to contain 'basement', got: %s", h.stdout.String())
	}
}

func TestLoginWritesConfig(t *testing.T) {
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	t.Setenv("BASEMENT_CONFIG", cfgPath)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("BASEMENT_PROFILE", "")
	t.Setenv("BASEMENT_SECRET_KEY", "")

	h := &harness{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, cfgPath: cfgPath}
	if err := h.run(t,
		"login",
		"--endpoint", "https://example.test",
		"--key", "BMNTtest",
		"--secret", "supersecret",
	); err != nil {
		t.Fatalf("login: %v", err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	p, ok := cfg.Profiles["default"]
	if !ok {
		t.Fatal("default profile not written")
	}
	if p.Endpoint != "https://example.test" || p.AccessKeyID != "BMNTtest" || p.SecretKey != "supersecret" {
		t.Errorf("profile = %+v", p)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("config file missing: %v", err)
	}
}

func TestRegionsList(t *testing.T) {
	var gotAuth, gotPath string
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"r1","alias":"home","endpoint":"https://h.test","region":"us-east-1","accessKeyId":"AK"}
		]`))
	}))
	if err := h.run(t, "regions", "list"); err != nil {
		t.Fatalf("regions list: %v", err)
	}
	if gotPath != "/api/v1/user/regions" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Errorf("missing bearer header: %q", gotAuth)
	}
	if !strings.Contains(h.stdout.String(), "home") {
		t.Errorf("expected alias in output: %s", h.stdout.String())
	}
}

func TestRegionsAdd(t *testing.T) {
	var gotBody map[string]any
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"new-rid","alias":"work","endpoint":"https://w.test"}`))
	}))
	if err := h.run(t, "regions", "add",
		"work", "https://w.test", "AKID", "SECRET", "us-west-2"); err != nil {
		t.Fatalf("regions add: %v", err)
	}
	if gotBody["alias"] != "work" || gotBody["endpoint"] != "https://w.test" ||
		gotBody["accessKeyId"] != "AKID" || gotBody["secretKey"] != "SECRET" ||
		gotBody["region"] != "us-west-2" {
		t.Errorf("create body = %#v", gotBody)
	}
}

func TestRegionsDelete(t *testing.T) {
	var gotMethod, gotPath string
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := h.run(t, "regions", "delete", "rid-zap"); err != nil {
		t.Fatalf("regions delete: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/v1/user/regions/rid-zap" {
		t.Errorf("%s %s", gotMethod, gotPath)
	}
}

func TestBucketsList(t *testing.T) {
	var gotPath string
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"buckets":[{"id":"b1","aliases":["photos"],"objects":42,"bytes":1024}],
			"perBucketStatsAvailable":true
		}`))
	}))
	if err := h.run(t, "buckets", "list"); err != nil {
		t.Fatalf("buckets list: %v", err)
	}
	wantPath := "/api/v1/user/regions/rid-default/buckets"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	out := h.stdout.String()
	if !strings.Contains(out, "photos") || !strings.Contains(out, "42") {
		t.Errorf("expected photos + 42 in output: %s", out)
	}
}

func TestBucketsListRegionFlagOverride(t *testing.T) {
	var gotPath string
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"buckets":[],"perBucketStatsAvailable":false}`))
	}))
	if err := h.run(t, "buckets", "list", "--region", "rid-override"); err != nil {
		t.Fatalf("buckets list: %v", err)
	}
	if gotPath != "/api/v1/user/regions/rid-override/buckets" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestObjectsList(t *testing.T) {
	var gotPath, gotQuery string
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"objects":[{"key":"a.txt","size":100}],"isTruncated":false}`))
	}))
	if err := h.run(t, "objects", "list", "mybucket", "--prefix", "docs/"); err != nil {
		t.Fatalf("objects list: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/buckets/mybucket/objects") {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "prefix=docs%2F") && !strings.Contains(gotQuery, "prefix=docs/") {
		t.Errorf("query = %q", gotQuery)
	}
	if !strings.Contains(h.stdout.String(), "a.txt") {
		t.Errorf("expected a.txt in output: %s", h.stdout.String())
	}
}

func TestObjectsDelete(t *testing.T) {
	var gotMethod, gotPath string
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := h.run(t, "objects", "delete", "mybucket", "docs/file.txt"); err != nil {
		t.Fatalf("objects delete: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q", gotMethod)
	}
	// Path: /api/v1/user/regions/rid-default/buckets/mybucket/objects/docs%2Ffile.txt
	if !strings.Contains(gotPath, "/buckets/mybucket/objects/") {
		t.Errorf("path = %q", gotPath)
	}
}

func TestObjectsGetStreamsThroughPresign(t *testing.T) {
	// Object data server (serves the file body when the CLI follows
	// the presigned URL).
	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("HELLO"))
	}))
	defer dataSrv.Close()

	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/presign-get") {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"url":     dataSrv.URL + "/blob",
			"expires": "2099-01-01T00:00:00Z",
			"method":  "GET",
		})
	}))

	outFile := filepath.Join(t.TempDir(), "out.bin")
	if err := h.run(t, "objects", "get", "mybucket", "hello.txt", "--output", outFile); err != nil {
		t.Fatalf("objects get: %v", err)
	}
	body, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(body) != "HELLO" {
		t.Errorf("downloaded body = %q", string(body))
	}
}

func TestObjectsPutStreamsThroughPresign(t *testing.T) {
	var gotPutBody []byte
	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		gotPutBody = buf[:n]
		w.WriteHeader(http.StatusOK)
	}))
	defer dataSrv.Close()

	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/presign-put") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"url":     dataSrv.URL + "/blob",
			"expires": "2099-01-01T00:00:00Z",
			"method":  "PUT",
		})
	}))

	srcFile := filepath.Join(t.TempDir(), "upload.bin")
	if err := os.WriteFile(srcFile, []byte("WORLD"), 0o600); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := h.run(t, "objects", "put", "mybucket", "hi.txt", srcFile); err != nil {
		t.Fatalf("objects put: %v", err)
	}
	if string(gotPutBody) != "WORLD" {
		t.Errorf("uploaded body = %q", string(gotPutBody))
	}
}

func TestKeysList(t *testing.T) {
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/service-accounts" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"id":"sa1","name":"ci","accessKeyId":"AK","createdAt":"2026-05-01T00:00:00Z"}]`))
	}))
	if err := h.run(t, "keys", "list"); err != nil {
		t.Fatalf("keys list: %v", err)
	}
	if !strings.Contains(h.stdout.String(), "ci") {
		t.Errorf("expected sa name in output: %s", h.stdout.String())
	}
}

func TestKeysCreate(t *testing.T) {
	var gotBody map[string]any
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"serviceAccount":{"id":"sa-new","name":"runner","accessKeyId":"AK"},
			"secret":"PLAINTEXT"
		}`))
	}))
	if err := h.run(t, "keys", "create", "runner",
		"--capability", "host:manage_users", "--scope", "host:*"); err != nil {
		t.Fatalf("keys create: %v", err)
	}
	if gotBody["name"] != "runner" {
		t.Errorf("create body = %#v", gotBody)
	}
	caps, ok := gotBody["capabilities"].([]any)
	if !ok || len(caps) != 1 {
		t.Fatalf("capabilities = %#v", gotBody["capabilities"])
	}
	first := caps[0].(map[string]any)
	if first["id"] != "host:manage_users" || first["scope"] != "host:*" {
		t.Errorf("capability = %#v", first)
	}
	if !strings.Contains(h.stdout.String(), "PLAINTEXT") {
		t.Errorf("expected plaintext secret in output: %s", h.stdout.String())
	}
}

func TestKeysCreateRejectsMismatchedFlags(t *testing.T) {
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when flags don't match")
	}))
	err := h.run(t, "keys", "create", "runner",
		"--capability", "host:manage_users", "--capability", "extra:cap",
		"--scope", "host:*")
	if err == nil {
		t.Fatal("expected error for mismatched --capability/--scope counts")
	}
}

func TestKeysRotate(t *testing.T) {
	var gotPath string
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{
			"serviceAccount":{"id":"sa-x","accessKeyId":"AK"},
			"secret":"NEWSECRET"
		}`))
	}))
	if err := h.run(t, "keys", "rotate", "sa-x"); err != nil {
		t.Fatalf("keys rotate: %v", err)
	}
	if gotPath != "/api/v1/admin/service-accounts/sa-x/rotate" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(h.stdout.String(), "NEWSECRET") {
		t.Errorf("expected new secret in output: %s", h.stdout.String())
	}
}

func TestKeysDelete(t *testing.T) {
	var gotMethod, gotPath string
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := h.run(t, "keys", "delete", "sa-zap"); err != nil {
		t.Fatalf("keys delete: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/v1/admin/service-accounts/sa-zap" {
		t.Errorf("%s %s", gotMethod, gotPath)
	}
}

func TestJSONOutputFormat(t *testing.T) {
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"r1","alias":"home","endpoint":"https://h.test","region":"r","accessKeyId":"AK"}]`))
	}))
	if err := h.run(t, "regions", "list", "--output-format", "json"); err != nil {
		t.Fatalf("regions list json: %v", err)
	}
	if !strings.Contains(h.stdout.String(), "\"id\": \"r1\"") {
		t.Errorf("expected indented JSON, got: %s", h.stdout.String())
	}
}

func TestNoSecretFails(t *testing.T) {
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	t.Setenv("BASEMENT_CONFIG", cfgPath)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("BASEMENT_PROFILE", "")
	t.Setenv("BASEMENT_SECRET_KEY", "")

	// Write a profile with no secret.
	cfg := &Config{Profiles: map[string]Profile{
		"default": {Endpoint: "https://x.test", AccessKeyID: "AK"},
	}}
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	h := &harness{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, cfgPath: cfgPath}
	if err := h.run(t, "regions", "list"); err == nil {
		t.Fatal("expected error for missing secret")
	}
}

// Sanity check that pathEscape preserves slashes in keys per the
// server-side chi {key} pattern (the user-region object route uses a
// single segment, not a splat — see internal/api/server.go line 551).
func TestPathEscapePreservesEncoded(t *testing.T) {
	got := pathEscape("docs/2024/q1.txt")
	if !strings.Contains(got, "%2F") {
		t.Errorf("pathEscape = %q (expected slashes encoded)", got)
	}
}

