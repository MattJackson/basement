//go:build integration

// End-to-end integration test for the federation replication engine
// against TWO real Garage v2 clusters. Spun up via
// internal/drivers/garagetest; run only under `go test -tags=integration`.
//
// This is the regression net for v1.11.0.4 (federation no-op
// replication bug). The shipped engine would tick, log "tick start",
// but never actually replicate objects uploaded within a tick of the
// LastSync watermark because of a precision mismatch between
// second-rounded source mtimes (Garage v2, S3) and nanosecond-precision
// LastSync. The unit test in engine_test.go pins the diff-computation
// logic with a fakeDriver; this test proves the WHOLE STACK
// (driver -> federation adapter -> engine -> replica driver) does
// the right thing against real Garage.
//
// Why two real clusters: the v1.11.0.4 bug only surfaced because the
// real Garage v2 driver returns object mtimes truncated to the second.
// The fakeDriver in engine_test.go records nanosecond-precision
// LastModified, so the unit suite missed the precision-loss interaction
// for two full release cycles. testcontainers + a real driver pair
// catches this class of "interface contract is honoured but underlying
// data shape differs from the fake" bugs the unit suite can't.

package federation_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/driver"
	_ "github.com/mattjackson/basement/internal/drivers/garage" // register the "garage" driver in the registry
	"github.com/mattjackson/basement/internal/drivers/garagetest"
	"github.com/mattjackson/basement/internal/federation"
)

// TestFederation_Integration_Replication is the v1.11.0.4 regression
// net at the end-to-end layer. Spins up two Garage v2 clusters, wires
// them as a primary + replica federation, uploads an object to the
// primary, and asserts the engine replicates it to the replica within
// a few ticks.
//
// The bug shape it catches:
//  1. Engine boots, fires "first tick" against an empty primary,
//     records LastSync = T (nanosecond precision).
//  2. Operator uploads object O to primary; Garage v2 records
//     O.LastModified = T' where T' is whole-second truncated.
//  3. If T' <= T (likely when the upload landed sub-second after the
//     boot tick), the engine's LastSync prefilter skips O forever.
//
// The fix (LastSyncSlack = 2s) keeps the steady-state filter useful
// while permitting whole-second mtimes within the slack window.
func TestFederation_Integration_Replication(t *testing.T) {
	// Spin up two Garage v2 clusters via testcontainers.
	primary := garagetest.Bootstrap(t, garagetest.V2)
	replica := garagetest.Bootstrap(t, garagetest.V2)

	ctx := context.Background()

	// On each cluster: create a bucket, mint a key, grant the key
	// owner+read+write on its bucket so the S3-tier driver can drive
	// objects through (the engine writes to the replica, so the
	// replica's key needs Write too).
	_, primaryBucket, primaryKey, primarySecret := setupClusterAdmin(t, ctx, primary, "fed-primary")
	_, replicaBucket, replicaKey, replicaSecret := setupClusterAdmin(t, ctx, replica, "fed-replica")

	// Build the S3-credentialed drivers the federation engine will use
	// for the data plane (both as primary stream source and replica
	// writer). These must be wired with full creds so PutObjectStream
	// + StreamObject succeed.
	primaryDrv, err := driver.Open("garage", primary.FullConfig(primaryKey, primarySecret))
	if err != nil {
		t.Fatalf("driver.Open(garage) for primary: %v", err)
	}
	replicaDrv, err := driver.Open("garage", replica.FullConfig(replicaKey, replicaSecret))
	if err != nil {
		t.Fatalf("driver.Open(garage) for replica: %v", err)
	}

	resolver := newStaticResolver(map[string]federation.ReplicationClient{
		"primary-region": newReplicationClient(primaryDrv),
		"replica-region": newReplicationClient(replicaDrv),
	})

	// Open an empty federated-bucket store under a per-test temp dir
	// so test runs never collide.
	store, err := federation.Open(t.TempDir())
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}

	// Create the FederatedBucket: primary -> replica, continuous mode.
	fb := federation.FederatedBucket{
		OwnerUserID: "test-owner",
		Name:        "integration-test",
		Primary: federation.ReplicaTarget{
			RegionID: "primary-region",
			Bucket:   primaryBucket,
		},
		Replicas: []federation.ReplicaTarget{
			{RegionID: "replica-region", Bucket: replicaBucket},
		},
		Policy: federation.DefaultPolicy(),
	}
	saved, err := store.Create(ctx, fb)
	if err != nil {
		t.Fatalf("store.Create: %v", err)
	}

	// Build the engine with a sub-second tick so we don't spend 30s
	// of CI time waiting on the default 10s cadence. SetTickInterval
	// must happen before Start.
	silentLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := federation.NewEngine(store, resolver, nil, silentLogger)
	engine.SetTickInterval(500 * time.Millisecond)
	engine.SetWorkers(2)
	engine.Start(ctx)
	t.Cleanup(engine.Stop)

	// Give the boot tick time to fire against the (empty) primary so
	// LastSync is set BEFORE we upload the object. Without this gap
	// the bug doesn't repro — we want the precision mismatch to bite.
	time.Sleep(1500 * time.Millisecond)

	// Upload an object directly via the primary's S3-credentialed
	// driver. Use a deterministic body so failure messages are easy
	// to read.
	body := []byte("federation-integration-test-payload")
	if _, err := primaryDrv.PutObjectStream(ctx, primaryBucket, "hello.txt", bytes.NewReader(body), "text/plain", int64(len(body))); err != nil {
		t.Fatalf("PutObjectStream primary: %v", err)
	}

	// Nudge the engine to re-tick immediately. The bug's failure mode
	// is "all subsequent ticks are no-ops" — so a single TriggerNow
	// either succeeds (engine fixed) or fails-forever (engine broken).
	if err := engine.TriggerNow(saved.ID); err != nil {
		t.Fatalf("TriggerNow: %v", err)
	}

	// Poll up to 15s for the object to appear on the replica. Each
	// tick is 500ms so this allows ~30 ticks of headroom — generous
	// for CI variance.
	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		stat, err := replicaDrv.StatObject(ctx, replicaBucket, "hello.txt")
		if err == nil && stat.Size == int64(len(body)) {
			// Replication propagated — sanity-check by reading the body
			// back and comparing bytes.
			stream, serr := replicaDrv.StreamObject(ctx, replicaBucket, "hello.txt", "")
			if serr == nil {
				got, _ := io.ReadAll(stream.Body)
				_ = stream.Body.Close()
				if string(got) != string(body) {
					t.Fatalf("replica object body mismatch: got %q, want %q", got, body)
				}
			}
			return // success — engine replicated end-to-end.
		}
		lastErr = err
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("federation engine never replicated object to replica within 15s — v1.11.0.4 regression (last StatObject err: %v)", lastErr)
}

// setupClusterAdmin: on a freshly bootstrapped Garage cluster, create
// a bucket, mint a key, grant the key owner+read+write on the bucket,
// and return (admin driver, bucketID, accessKey, secret).
//
// The returned driver is admin-tier — it talks the admin API for the
// bucket/key setup. Tests that want to drive S3 traffic build a
// second driver from the same cluster's FullConfig with the returned
// key/secret pair.
func setupClusterAdmin(t *testing.T, ctx context.Context, cluster *garagetest.Cluster, alias string) (driver.Driver, string, string, string) {
	t.Helper()

	d, err := driver.Open("garage", cluster.AdminConfig())
	if err != nil {
		t.Fatalf("driver.Open(garage) admin for %s: %v", alias, err)
	}

	bucket, err := d.CreateBucket(ctx, driver.BucketSpec{Alias: alias})
	if err != nil {
		t.Fatalf("CreateBucket %s: %v", alias, err)
	}

	key, err := d.CreateKey(ctx, driver.KeySpec{Name: alias + "-key"})
	if err != nil {
		t.Fatalf("CreateKey %s: %v", alias, err)
	}
	if key.SecretAccessKey == nil {
		t.Fatalf("CreateKey %s: nil secret on response", alias)
	}

	perms := []driver.BucketPermission{{
		BucketID: bucket.ID,
		Read:     true,
		Write:    true,
		Owner:    true,
	}}
	if err := d.UpdateKeyPermissions(ctx, key.ID, perms); err != nil {
		t.Fatalf("UpdateKeyPermissions %s: %v", alias, err)
	}

	return d, bucket.ID, key.ID, *key.SecretAccessKey
}

// staticResolver is the integration test's stand-in for the production
// federationwire.NewResolver. Maps regionID -> ReplicationClient with
// no encryption or user-region lookups — the integration test only
// cares about engine behaviour.
func newStaticResolver(by map[string]federation.ReplicationClient) federation.DriverResolver {
	cp := make(map[string]federation.ReplicationClient, len(by))
	for k, v := range by {
		cp[k] = v
	}
	mu := sync.RWMutex{}
	return federation.DriverResolverFunc(func(_ context.Context, _, regionID string) (federation.ReplicationClient, error) {
		mu.RLock()
		defer mu.RUnlock()
		c, ok := cp[regionID]
		if !ok {
			return nil, errors.New("no client for region " + regionID)
		}
		return c, nil
	})
}

// newReplicationClient wraps a driver.Driver as a
// federation.ReplicationClient. Duplicates federationwire.driverAdapter
// because the integration test can't import that package without
// creating cyclic test-build edges under -tags=integration.
func newReplicationClient(d driver.Driver) federation.ReplicationClient {
	return &integrationAdapter{drv: d}
}

type integrationAdapter struct {
	drv driver.Driver
}

func (a *integrationAdapter) Capabilities(ctx context.Context) (federation.Capabilities, error) {
	c, err := a.drv.Capabilities(ctx)
	if err != nil {
		return federation.Capabilities{}, err
	}
	return federation.Capabilities{Driver: c.Driver, ServerSideCopy: c.ServerSideCopy}, nil
}

func (a *integrationAdapter) ListObjects(ctx context.Context, bucket, continuation string, limit int) (federation.ObjectPage, error) {
	page, err := a.drv.ListObjects(ctx, bucket, "", continuation, "", limit)
	if err != nil {
		return federation.ObjectPage{}, err
	}
	out := make([]federation.ObjectInfo, 0, len(page.Objects))
	for _, o := range page.Objects {
		out = append(out, federation.ObjectInfo{
			Key:          o.Key,
			Size:         o.Size,
			ETag:         o.ETag,
			LastModified: o.LastModified,
			IsDir:        o.IsDir,
		})
	}
	return federation.ObjectPage{
		Objects:          out,
		NextContinuation: page.NextContinuation,
		IsTruncated:      page.IsTruncated,
	}, nil
}

func (a *integrationAdapter) StatObject(ctx context.Context, bucket, key string) (federation.ObjectInfo, error) {
	o, err := a.drv.StatObject(ctx, bucket, key)
	if err != nil {
		return federation.ObjectInfo{}, err
	}
	return federation.ObjectInfo{
		Key:          o.Key,
		Size:         o.Size,
		ETag:         o.ETag,
		LastModified: o.LastModified,
		IsDir:        o.IsDir,
	}, nil
}

func (a *integrationAdapter) StreamObject(ctx context.Context, bucket, key string) (federation.StreamResult, error) {
	s, err := a.drv.StreamObject(ctx, bucket, key, "")
	if err != nil {
		return federation.StreamResult{}, err
	}
	return federation.StreamResult{
		Body:          s.Body,
		ContentType:   s.ContentType,
		ContentLength: s.ContentLength,
		ETag:          s.ETag,
		LastModified:  s.LastModified,
	}, nil
}

func (a *integrationAdapter) PutObjectStream(ctx context.Context, bucket, key string, reader io.Reader, contentType string, size int64) error {
	_, err := a.drv.PutObjectStream(ctx, bucket, key, reader, contentType, size)
	return err
}

func (a *integrationAdapter) ServerSideCopy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	return a.drv.ServerSideCopy(ctx, srcBucket, srcKey, dstBucket, dstKey)
}

func (a *integrationAdapter) DeleteObject(ctx context.Context, bucket, key string) error {
	return a.drv.DeleteObject(ctx, bucket, key)
}

// init suppresses noisy slog output unless BASEMENT_INT_TEST_LOG=1 is
// set. Useful so CI logs don't drown in engine-tick debug noise on a
// passing run.
func init() {
	if strings.EqualFold(os.Getenv("BASEMENT_INT_TEST_LOG"), "1") {
		return
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// Compile-time assertion that we still implement ReplicationClient
// after any future interface changes. Catches drift without a runtime
// dependency.
var _ federation.ReplicationClient = (*integrationAdapter)(nil)
