//go:build integration

// Package garagetest spins up real Garage (v1 or v2) containers for
// driver + federation integration tests. The package compiles only
// under the `integration` build tag so `go test ./...` (the default
// developer + CI unit-test path) is never burdened with the
// testcontainers / Docker dependency. Run via `make integration`.
//
// The package speaks to Garage's admin API directly (raw HTTP) for
// bootstrap rather than going through internal/drivers/garage so the
// driver under test never participates in its own setup. That keeps
// "did the driver wire up correctly?" honest — every assertion runs
// against the same admin surface that observed the bootstrap.
//
// Bug-class coverage (v1.11.0.8 cycle):
//   - v1.11.0.1 — admin-only driver (no S3 creds) must build + ListBuckets
//   - v1.11.0.2 — per-cluster handlers must round-trip bucket IDs
//   - v1.11.0.5 BUG02 — AllowBucketKey then GetKey must surface grant
//   - v1.11.0.4 — federation engine must replicate brand-new objects
package garagetest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Image names. Pinned to the exact versions the v1.11.0.8 cycle spec
// names so a future Garage release can't silently change the test
// surface.
const (
	imageV1 = "dxflrs/garage:v1.0.1"
	imageV2 = "dxflrs/garage:v2.0.0"
)

// Version selects which Garage generation to spin up. Use Bootstrap
// rather than referencing these constants directly.
type Version string

const (
	// V1 is Garage 1.0.1, served via /v1/* admin paths.
	V1 Version = "v1"
	// V2 is Garage 2.0.0, served via /v2/* admin paths.
	V2 Version = "v2"
)

// Cluster is a live, bootstrapped Garage cluster. Returned by
// Bootstrap; torn down automatically via t.Cleanup.
//
// AdminURL is "http://<host>:<port>" (no path suffix); callers append
// /v1/... or /v2/... as appropriate. AdminToken is the bearer token
// the bootstrap config baked in.
//
// S3Endpoint is the S3-compatible data plane URL. Tests that want to
// drive the data path (presign, multipart, PutObjectStream) wire a
// driver with this endpoint + a created key's plaintext credentials.
type Cluster struct {
	Version    Version
	AdminURL   string
	AdminToken string
	S3Endpoint string
	S3Region   string

	t         *testing.T
	container testcontainers.Container
}

// Bootstrap spins up a single-node Garage cluster of the requested
// version, waits for the admin API to listen, assigns + applies a
// single-node layout, and returns a ready-to-use Cluster.
//
// The cluster is torn down via t.Cleanup; callers do not need to
// arrange teardown explicitly.
//
// Skips the test (not fails) when Docker is unreachable so a
// developer running `go test -tags=integration ./...` on a laptop
// without Docker gets a clean "skipped" rather than a noisy error.
func Bootstrap(t *testing.T, version Version) *Cluster {
	t.Helper()

	image, ok := map[Version]string{V1: imageV1, V2: imageV2}[version]
	if !ok {
		t.Fatalf("garagetest: unknown version %q", version)
	}

	// Generate per-run secrets so parallel tests / re-runs don't share
	// state via the data dir. rpc_secret must be 64 hex chars (32 bytes);
	// admin_token is opaque.
	rpcSecret := mustHex(t, 32)
	adminToken := mustHex(t, 16)

	cfg := renderConfig(version, rpcSecret, adminToken)

	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image: image,
		ExposedPorts: []string{
			"3900/tcp", // S3 API
			"3901/tcp", // RPC
			"3902/tcp", // S3 web (unused but published for completeness)
			"3903/tcp", // Admin API
		},
		Files: []testcontainers.ContainerFile{
			{
				Reader:            strings.NewReader(cfg),
				ContainerFilePath: "/etc/garage.toml",
				FileMode:          0o644,
			},
		},
		Env: map[string]string{
			"GARAGE_CONFIG_FILE": "/etc/garage.toml",
		},
		WaitingFor: wait.ForListeningPort("3903/tcp").WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		// Common case: Docker not running on the host. Skip rather than
		// fail so `go test -tags=integration ./...` on a non-Docker box
		// produces a clean result.
		if isDockerUnavailable(err) {
			t.Skipf("garagetest: Docker unavailable (%v) — skipping integration test", err)
		}
		t.Fatalf("garagetest: failed to start Garage %s container: %v", version, err)
	}
	t.Cleanup(func() {
		// 30s ceiling on teardown — a stuck container shouldn't wedge CI.
		tctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := container.Terminate(tctx); err != nil {
			t.Logf("garagetest: container terminate failed: %v", err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("garagetest: container.Host: %v", err)
	}
	adminPort, err := container.MappedPort(ctx, "3903/tcp")
	if err != nil {
		t.Fatalf("garagetest: container.MappedPort(3903): %v", err)
	}
	s3Port, err := container.MappedPort(ctx, "3900/tcp")
	if err != nil {
		t.Fatalf("garagetest: container.MappedPort(3900): %v", err)
	}

	cl := &Cluster{
		Version:    version,
		AdminURL:   fmt.Sprintf("http://%s:%s", host, adminPort.Port()),
		AdminToken: adminToken,
		S3Endpoint: fmt.Sprintf("http://%s:%s", host, s3Port.Port()),
		S3Region:   "garage",
		t:          t,
		container:  container,
	}

	// Wait for the admin token gate to accept calls. Garage opens the
	// port before it's fully ready to authorise; poll GetClusterStatus
	// until we get a 2xx OR a timeout.
	if err := cl.waitForAdminReady(ctx, 30*time.Second); err != nil {
		t.Fatalf("garagetest: admin API never became ready: %v", err)
	}

	// Bootstrap single-node layout. The admin API exposes the calls we
	// need; we route raw HTTP through the cluster's own admin surface
	// so the driver under test isn't part of its own bootstrap.
	if err := cl.bootstrapLayout(ctx); err != nil {
		t.Fatalf("garagetest: layout bootstrap failed: %v", err)
	}

	return cl
}

// AdminConfig returns the map a driver.Config wants to talk to this
// cluster on the admin tier (admin_url + admin_token only — the
// admin-only "ADR-0001" connection shape that v1.11.0.1 fixed).
func (c *Cluster) AdminConfig() map[string]string {
	return map[string]string{
		"admin_url":   c.AdminURL,
		"admin_token": c.AdminToken,
	}
}

// FullConfig returns the map a driver.Config wants to talk to this
// cluster including S3 data plane creds. Use after CreateKey + a
// successful AllowBucketKey to drive the data path with a working
// key/secret pair.
//
// Carries the SigV4 region label as `region`; Garage v2's SigV4
// validation rejects mismatched region scope, so the value here
// must match what the cluster's `s3_api.s3_region` config block
// declares (see renderConfig).
func (c *Cluster) FullConfig(accessKey, secretKey string) map[string]string {
	return map[string]string{
		"admin_url":     c.AdminURL,
		"admin_token":   c.AdminToken,
		"s3_endpoint":   c.S3Endpoint,
		"access_key_id": accessKey,
		"secret_key":    secretKey,
		"region":        c.S3Region,
	}
}

// CreateBucketDirect creates a bucket via the admin API directly,
// bypassing the driver under test. Used by tests that want to seed
// state without depending on the code path they're about to assert
// against.
func (c *Cluster) CreateBucketDirect(ctx context.Context, alias string) (id string, err error) {
	path, body := c.createBucketRequest(alias)
	var resp struct {
		ID string `json:"id"`
	}
	if err := c.adminDo(ctx, "POST", path, body, &resp); err != nil {
		return "", err
	}
	return resp.ID, nil
}

// GetBucketDirect fetches a bucket's GetBucketInfo response directly
// from the admin API. Returned ID is the cluster's actual ID for the
// alias — tests compare this against the driver's claim.
func (c *Cluster) GetBucketDirect(ctx context.Context, alias string) (id string, err error) {
	path := c.getBucketByAliasPath(alias)
	var resp struct {
		ID string `json:"id"`
	}
	if err := c.adminDo(ctx, "GET", path, nil, &resp); err != nil {
		return "", err
	}
	return resp.ID, nil
}

// GetKeyDirect fetches a key's GetKeyInfo response directly from the
// admin API. Returned struct mirrors the shape both v1 and v2 share at
// the fields the federation/driver tests assert against.
func (c *Cluster) GetKeyDirect(ctx context.Context, id string) (KeyInfoDirect, error) {
	path := c.getKeyByIDPath(id)
	var ki KeyInfoDirect
	switch c.Version {
	case V2:
		var raw struct {
			AccessKeyID string `json:"accessKeyId"`
			Name        string `json:"name"`
			Buckets     []struct {
				ID          string `json:"id"`
				Permissions struct {
					Read  bool `json:"read"`
					Write bool `json:"write"`
					Owner bool `json:"owner"`
				} `json:"permissions"`
			} `json:"buckets"`
		}
		if err := c.adminDo(ctx, "GET", path, nil, &raw); err != nil {
			return ki, err
		}
		ki.ID = raw.AccessKeyID
		ki.Name = raw.Name
		for _, b := range raw.Buckets {
			ki.Buckets = append(ki.Buckets, BucketGrantDirect{
				BucketID: b.ID,
				Read:     b.Permissions.Read,
				Write:    b.Permissions.Write,
				Owner:    b.Permissions.Owner,
			})
		}
	case V1:
		var raw struct {
			AccessKeyID string `json:"accessKeyId"`
			Name        string `json:"name"`
			Buckets     []struct {
				ID          string `json:"id"`
				Permissions struct {
					Read  bool `json:"read"`
					Write bool `json:"write"`
					Owner bool `json:"owner"`
				} `json:"permissions"`
			} `json:"buckets"`
		}
		if err := c.adminDo(ctx, "GET", path, nil, &raw); err != nil {
			return ki, err
		}
		ki.ID = raw.AccessKeyID
		ki.Name = raw.Name
		for _, b := range raw.Buckets {
			ki.Buckets = append(ki.Buckets, BucketGrantDirect{
				BucketID: b.ID,
				Read:     b.Permissions.Read,
				Write:    b.Permissions.Write,
				Owner:    b.Permissions.Owner,
			})
		}
	}
	return ki, nil
}

// KeyInfoDirect is the admin-API view of a key. Test assertions
// compare driver-returned grants against this so a future driver bug
// that silently zeroes a grant (the v1.11.0.5 BUG02 class) fails loud.
type KeyInfoDirect struct {
	ID      string
	Name    string
	Buckets []BucketGrantDirect
}

// BucketGrantDirect mirrors the cluster's view of a key's permissions
// on a single bucket.
type BucketGrantDirect struct {
	BucketID string
	Read     bool
	Write    bool
	Owner    bool
}

// adminDo runs an HTTP request against the cluster's admin API,
// authenticated with the bootstrap bearer token. Used internally
// for bootstrap + by the *Direct helpers; not part of the public
// surface tests should reach for.
func (c *Cluster) adminDo(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.AdminURL+path, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.AdminToken)
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 20 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("admin %s %s: %d %s", method, path, resp.StatusCode, string(respBody))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("unmarshal %s: %w", path, err)
		}
	}
	return nil
}

// waitForAdminReady polls a cheap admin endpoint until it returns 2xx
// or the deadline elapses. Garage opens the port before its admin
// token check is wired so tests that race the bootstrap see 401s
// without this gate.
func (c *Cluster) waitForAdminReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	probe := c.statusPath()
	var lastErr error
	for time.Now().Before(deadline) {
		ctx2, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := c.adminDo(ctx2, "GET", probe, nil, &json.RawMessage{})
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = errors.New("unknown")
	}
	return fmt.Errorf("timed out waiting for admin: %w", lastErr)
}

// bootstrapLayout stages + applies a single-node layout. Garage
// containers boot with an unassigned node — without this the data
// plane refuses every write with "no nodes in layout".
//
// The v1 + v2 admin APIs are similar enough that this function
// dispatches on Version and does the right thing for each.
func (c *Cluster) bootstrapLayout(ctx context.Context) error {
	switch c.Version {
	case V2:
		return c.bootstrapLayoutV2(ctx)
	case V1:
		return c.bootstrapLayoutV1(ctx)
	default:
		return fmt.Errorf("unknown version %q", c.Version)
	}
}

func (c *Cluster) bootstrapLayoutV2(ctx context.Context) error {
	// 1. Discover the node ID.
	var status struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	if err := c.adminDo(ctx, "GET", "/v2/GetClusterStatus", nil, &status); err != nil {
		return fmt.Errorf("get status: %w", err)
	}
	if len(status.Nodes) == 0 {
		return errors.New("no nodes in cluster status")
	}
	nodeID := status.Nodes[0].ID

	// 2. Stage the role for our single node.
	capacity := int64(1 << 30) // 1 GiB; arbitrary, just needs to be non-zero.
	stageReq := map[string]any{
		"nodeId": nodeID,
		"storageNode": map[string]any{
			"id":       nodeID,
			"zone":     "test",
			"tags":     []string{},
			"capacity": capacity,
		},
	}
	if err := c.adminDo(ctx, "POST", "/v2/UpdateClusterLayout", stageReq, nil); err != nil {
		return fmt.Errorf("stage layout: %w", err)
	}

	// 3. Get current version so we can apply v+1.
	var layout struct {
		Version int64 `json:"version"`
	}
	if err := c.adminDo(ctx, "GET", "/v2/GetClusterLayout", nil, &layout); err != nil {
		return fmt.Errorf("get layout: %w", err)
	}

	applyReq := map[string]any{"version": layout.Version + 1}
	if err := c.adminDo(ctx, "POST", "/v2/ApplyClusterLayout", applyReq, nil); err != nil {
		return fmt.Errorf("apply layout: %w", err)
	}

	// 4. Wait until the cluster reports healthy.
	return c.waitForHealthy(ctx, 30*time.Second)
}

func (c *Cluster) bootstrapLayoutV1(ctx context.Context) error {
	// 1. Discover the node ID via /v1/status.
	var status struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	if err := c.adminDo(ctx, "GET", "/v1/status", nil, &status); err != nil {
		return fmt.Errorf("get status: %w", err)
	}
	if len(status.Nodes) == 0 {
		return errors.New("no nodes in cluster status")
	}
	nodeID := status.Nodes[0].ID

	// 2. Stage role. v1 /v1/layout POST takes a map of nodeId -> role.
	stageReq := map[string]any{
		nodeID: map[string]any{
			"zone":     "test",
			"capacity": int64(1 << 30),
			"tags":     []string{},
		},
	}
	if err := c.adminDo(ctx, "POST", "/v1/layout", stageReq, nil); err != nil {
		return fmt.Errorf("stage layout: %w", err)
	}

	// 3. Read current version + apply v+1.
	var layout struct {
		Version int64 `json:"version"`
	}
	if err := c.adminDo(ctx, "GET", "/v1/layout", nil, &layout); err != nil {
		return fmt.Errorf("get layout: %w", err)
	}
	applyReq := map[string]any{"version": layout.Version + 1}
	if err := c.adminDo(ctx, "POST", "/v1/layout/apply", applyReq, nil); err != nil {
		return fmt.Errorf("apply layout: %w", err)
	}

	return c.waitForHealthy(ctx, 30*time.Second)
}

// waitForHealthy polls cluster health until status == "healthy" or
// the deadline elapses. ApplyClusterLayout returns immediately but
// the partition table needs a moment to settle before writes succeed.
func (c *Cluster) waitForHealthy(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	path := c.healthPath()
	for time.Now().Before(deadline) {
		var resp struct {
			Status string `json:"status"`
		}
		if err := c.adminDo(ctx, "GET", path, nil, &resp); err == nil {
			if resp.Status == "healthy" {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("cluster never reached healthy state")
}

// Per-version path helpers — keeps the dispatch one place.

func (c *Cluster) statusPath() string {
	if c.Version == V2 {
		return "/v2/GetClusterStatus"
	}
	return "/v1/status"
}

func (c *Cluster) healthPath() string {
	if c.Version == V2 {
		return "/v2/GetClusterHealth"
	}
	return "/v1/health"
}

func (c *Cluster) createBucketRequest(alias string) (string, any) {
	if c.Version == V2 {
		return "/v2/CreateBucket", map[string]any{"globalAlias": alias}
	}
	return "/v1/bucket", map[string]any{"globalAlias": alias}
}

func (c *Cluster) getBucketByAliasPath(alias string) string {
	if c.Version == V2 {
		return "/v2/GetBucketInfo?globalAlias=" + alias
	}
	return "/v1/bucket?globalAlias=" + alias
}

func (c *Cluster) getKeyByIDPath(id string) string {
	if c.Version == V2 {
		return "/v2/GetKeyInfo?id=" + id
	}
	return "/v1/key?id=" + id
}

// renderConfig produces the garage.toml the container boots from.
// Single-node, replication_factor=1, sqlite metadata, ephemeral data
// directories under /tmp. The admin token is the bearer the test
// will use; rpc_secret is unused in single-node mode but Garage
// refuses to start without one.
func renderConfig(version Version, rpcSecret, adminToken string) string {
	// Garage v2 uses replication_factor; v1.0.1 supports it too as an
	// alias for the older replication_mode. Use replication_factor on
	// both for simplicity.
	return fmt.Sprintf(`
metadata_dir = "/tmp/garage-meta"
data_dir = "/tmp/garage-data"

db_engine = "sqlite"

replication_factor = 1

rpc_bind_addr = "[::]:3901"
rpc_public_addr = "127.0.0.1:3901"
rpc_secret = %q

[s3_api]
s3_region = "garage"
api_bind_addr = "[::]:3900"
root_domain = ".s3.garage"

[s3_web]
bind_addr = "[::]:3902"
root_domain = ".web.garage"
index = "index.html"

[admin]
api_bind_addr = "[::]:3903"
admin_token = %q
metrics_token = ""
`, rpcSecret, adminToken)
}

// mustHex generates n random bytes and returns them as a lowercase
// hex string. Fails the test on error (rand.Reader exhausted is a
// host catastrophe, not a test-recoverable failure).
func mustHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b)
}

// isDockerUnavailable inspects the error message from
// testcontainers.GenericContainer for the canonical "no Docker"
// signatures. Used to convert hard failures into Skip on a developer
// laptop without Docker — CI always has Docker so a real failure
// surfaces there.
func isDockerUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	signatures := []string{
		"Cannot connect to the Docker daemon",
		"docker: not found",
		"Cannot connect to the Docker daemon at",
		"connect: no such file or directory",
		"is the docker daemon running",
		"docker daemon",
		"Could not connect to Docker",
		"failed to create container",
		"rootless Docker not found",
	}
	low := strings.ToLower(msg)
	for _, sig := range signatures {
		if strings.Contains(low, strings.ToLower(sig)) {
			return true
		}
	}
	return false
}
