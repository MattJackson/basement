package metrics

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// fakeConns implements connectionLister for the scheduler test.
type fakeConns struct {
	conns []store.Connection
	err   error
}

func (f *fakeConns) List(_ context.Context) ([]store.Connection, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.conns, nil
}

// fakeReg implements driverProvider. Returns a configured driver
// (or err) per connection ID.
type fakeReg struct {
	mu       sync.Mutex
	drivers  map[string]driver.Driver
	buildErr map[string]error
}

func (f *fakeReg) For(_ context.Context, connID string) (driver.Driver, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.buildErr[connID]; ok {
		return nil, err
	}
	if d, ok := f.drivers[connID]; ok {
		return d, nil
	}
	return nil, errors.New("no fake driver for " + connID)
}

// schedulerStubDriver returns a fixed set of buckets on ListBuckets.
type schedulerStubDriver struct {
	buckets []driver.Bucket
	listErr error
}

func (d *schedulerStubDriver) Capabilities(_ context.Context) (driver.Caps, error) {
	return driver.Caps{}, nil
}
func (d *schedulerStubDriver) HealthCheck(_ context.Context) (driver.HealthReport, error) {
	return driver.HealthReport{Status: "healthy"}, nil
}
func (d *schedulerStubDriver) ListNodes(_ context.Context) ([]driver.Node, error) { return nil, nil }
func (d *schedulerStubDriver) GetLayout(_ context.Context) (driver.Layout, error) {
	return driver.Layout{}, nil
}
func (d *schedulerStubDriver) StageLayout(_ context.Context, _ driver.LayoutChange) (driver.LayoutDiff, error) {
	return driver.LayoutDiff{}, nil
}
func (d *schedulerStubDriver) ApplyLayout(_ context.Context) error  { return nil }
func (d *schedulerStubDriver) RevertLayout(_ context.Context) error { return nil }

func (d *schedulerStubDriver) ListBuckets(_ context.Context) ([]driver.Bucket, error) {
	if d.listErr != nil {
		return nil, d.listErr
	}
	return d.buckets, nil
}

func (d *schedulerStubDriver) GetBucket(_ context.Context, _ string) (driver.Bucket, error) {
	return driver.Bucket{}, nil
}
func (d *schedulerStubDriver) CreateBucket(_ context.Context, _ driver.BucketSpec) (driver.Bucket, error) {
	return driver.Bucket{}, nil
}
func (d *schedulerStubDriver) UpdateBucket(_ context.Context, _ string, _ driver.BucketUpdate) (driver.Bucket, error) {
	return driver.Bucket{}, nil
}
func (d *schedulerStubDriver) DeleteBucket(_ context.Context, _ string) error { return nil }

func (d *schedulerStubDriver) ListKeys(_ context.Context) ([]driver.Key, error) { return nil, nil }
func (d *schedulerStubDriver) GetKey(_ context.Context, _ string) (driver.Key, error) {
	return driver.Key{}, nil
}
func (d *schedulerStubDriver) CreateKey(_ context.Context, _ driver.KeySpec) (driver.Key, error) {
	return driver.Key{}, nil
}
func (d *schedulerStubDriver) UpdateKeyPermissions(_ context.Context, _ string, _ []driver.BucketPermission) error {
	return nil
}
func (d *schedulerStubDriver) DeleteKey(_ context.Context, _ string) error { return nil }

func (d *schedulerStubDriver) ListObjects(_ context.Context, _, _, _, _ string, _ int) (driver.ObjectPage, error) {
	return driver.ObjectPage{}, nil
}
func (d *schedulerStubDriver) StatObject(_ context.Context, _, _ string) (driver.ObjectInfo, error) {
	return driver.ObjectInfo{}, nil
}
func (d *schedulerStubDriver) PresignGet(_ context.Context, _, _ string, _ time.Duration) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, nil
}
func (d *schedulerStubDriver) PresignPut(_ context.Context, _, _ string, _ time.Duration, _ string) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, nil
}
func (d *schedulerStubDriver) DeleteObject(_ context.Context, _, _ string) error { return nil }
func (d *schedulerStubDriver) CreateMultipart(_ context.Context, _, _, _ string) (driver.MultipartUpload, error) {
	return driver.MultipartUpload{}, nil
}
func (d *schedulerStubDriver) PresignUploadPart(_ context.Context, _ driver.MultipartUpload, _ int) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, nil
}
func (d *schedulerStubDriver) CompleteMultipart(_ context.Context, _ driver.MultipartUpload, _ []driver.CompletedPart) error {
	return nil
}
func (d *schedulerStubDriver) AbortMultipart(_ context.Context, _ driver.MultipartUpload) error {
	return nil
}
func (d *schedulerStubDriver) StreamObject(_ context.Context, _, _, _ string) (driver.StreamResult, error) {
	return driver.StreamResult{}, nil
}
func (d *schedulerStubDriver) PutObjectStream(_ context.Context, _, _ string, _ io.Reader, _ string, _ int64) (driver.PutResult, error) {
	return driver.PutResult{}, nil
}
func (d *schedulerStubDriver) ServerSideCopy(_ context.Context, _, _, _, _ string) error {
	return nil
}
func (d *schedulerStubDriver) LifecycleSupport() driver.LifecycleCapabilities {
	return driver.LifecycleCapabilities{}
}
func (d *schedulerStubDriver) GetLifecycle(_ context.Context, _ string) ([]driver.LifecycleRule, error) {
	return nil, nil
}
func (d *schedulerStubDriver) PutLifecycle(_ context.Context, _ string, _ []driver.LifecycleRule) error {
	return nil
}
func (d *schedulerStubDriver) PerBucketStatsAvailable() bool { return false }

// v1.4.0c SCRUB.MAINT — stubs report unsupported.
func (d *schedulerStubDriver) ScrubSupport() driver.ScrubCapability {
	return driver.ScrubCapability{Supported: false}
}
func (d *schedulerStubDriver) ScrubState(_ context.Context) (driver.ScrubState, error) {
	return driver.ScrubState{}, driver.ErrUnsupported
}
func (d *schedulerStubDriver) StartScrub(_ context.Context) error { return driver.ErrUnsupported }

// v1.10.0a versioning — stubs report unsupported.
func (d *schedulerStubDriver) VersioningSupport() bool { return false }
func (d *schedulerStubDriver) GetVersioningStatus(_ context.Context, _ string) (driver.VersioningStatus, error) {
	return driver.VersioningDisabled, driver.ErrUnsupported
}
func (d *schedulerStubDriver) EnableVersioning(_ context.Context, _ string) error {
	return driver.ErrUnsupported
}
func (d *schedulerStubDriver) SuspendVersioning(_ context.Context, _ string) error {
	return driver.ErrUnsupported
}
func (d *schedulerStubDriver) ListObjectVersions(_ context.Context, _, _, _ string, _ int) ([]driver.ObjectVersion, string, error) {
	return nil, "", driver.ErrUnsupported
}
func (d *schedulerStubDriver) GetObjectVersion(_ context.Context, _, _, _ string) (driver.StreamResult, error) {
	return driver.StreamResult{}, driver.ErrUnsupported
}
func (d *schedulerStubDriver) DeleteObjectVersion(_ context.Context, _, _, _ string) error {
	return driver.ErrUnsupported
}

// TestRunOneCycle_HappyPath drives one cycle across two clusters
// with two buckets each and asserts four snapshots were recorded.
func TestRunOneCycle_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	rec := NewFileRecorder(tmp)
	defer rec.Close()

	conns := &fakeConns{conns: []store.Connection{
		{ID: "ca", Label: "alpha"},
		{ID: "cb", Label: "beta"},
	}}
	reg := &fakeReg{
		drivers: map[string]driver.Driver{
			"ca": &schedulerStubDriver{buckets: []driver.Bucket{
				{ID: "b1", Aliases: []string{"alpha-photos"}, Bytes: 1000, Objects: 10},
				{ID: "b2", Aliases: []string{"alpha-logs"}, Bytes: 2000, Objects: 20},
			}},
			"cb": &schedulerStubDriver{buckets: []driver.Bucket{
				{ID: "b3", Aliases: []string{"beta-backup"}, Bytes: 3000, Objects: 30},
				{ID: "b4", Bytes: 4000, Objects: 40},
			}},
		},
	}

	runOneCycle(context.Background(), SchedulerConfig{
		Conns:    conns,
		Reg:      reg,
		Recorder: rec,
	})

	snaps, err := rec.Query(time.Now().Add(-time.Hour), time.Now().Add(time.Hour), Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(snaps) != 4 {
		t.Errorf("expected 4 snapshots, got %d", len(snaps))
	}

	// Spot-check that aliases were carried through.
	foundAlias := false
	for _, s := range snaps {
		if s.BucketAlias == "alpha-photos" && s.Bytes == 1000 {
			foundAlias = true
		}
	}
	if !foundAlias {
		t.Errorf("expected alpha-photos snapshot with Bytes=1000; got %+v", snaps)
	}
}

// TestRunOneCycle_PartialFailureSurvives ensures one bad cluster
// (driver build error) doesn't stop the rest of the cycle.
func TestRunOneCycle_PartialFailureSurvives(t *testing.T) {
	tmp := t.TempDir()
	rec := NewFileRecorder(tmp)
	defer rec.Close()

	conns := &fakeConns{conns: []store.Connection{
		{ID: "good", Label: "good"},
		{ID: "bad", Label: "bad"},
	}}
	reg := &fakeReg{
		drivers: map[string]driver.Driver{
			"good": &schedulerStubDriver{buckets: []driver.Bucket{
				{ID: "b1", Bytes: 100},
			}},
		},
		buildErr: map[string]error{
			"bad": errors.New("forced build error"),
		},
	}

	runOneCycle(context.Background(), SchedulerConfig{
		Conns:    conns,
		Reg:      reg,
		Recorder: rec,
	})

	snaps, err := rec.Query(time.Now().Add(-time.Hour), time.Now().Add(time.Hour), Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(snaps) != 1 {
		t.Errorf("expected 1 snapshot (good cluster only), got %d", len(snaps))
	}
}

// TestRunOneCycle_ListBucketsError logs and skips, doesn't crash.
func TestRunOneCycle_ListBucketsError(t *testing.T) {
	tmp := t.TempDir()
	rec := NewFileRecorder(tmp)
	defer rec.Close()

	conns := &fakeConns{conns: []store.Connection{
		{ID: "ca", Label: "alpha"},
	}}
	reg := &fakeReg{
		drivers: map[string]driver.Driver{
			"ca": &schedulerStubDriver{listErr: errors.New("backend down")},
		},
	}

	// Should not panic.
	runOneCycle(context.Background(), SchedulerConfig{
		Conns:    conns,
		Reg:      reg,
		Recorder: rec,
	})

	snaps, _ := rec.Query(time.Now().Add(-time.Hour), time.Now().Add(time.Hour), Filter{})
	if len(snaps) != 0 {
		t.Errorf("expected 0 snapshots (ListBuckets errored), got %d", len(snaps))
	}
}

// TestRunScheduler_ContextCancelStops verifies the long-running
// loop exits promptly when its context is cancelled.
func TestRunScheduler_ContextCancelStops(t *testing.T) {
	tmp := t.TempDir()
	rec := NewFileRecorder(tmp)
	defer rec.Close()

	conns := &fakeConns{conns: []store.Connection{{ID: "ca"}}}
	reg := &fakeReg{
		drivers: map[string]driver.Driver{
			"ca": &schedulerStubDriver{buckets: []driver.Bucket{{ID: "b1", Bytes: 1}}},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunScheduler(ctx, SchedulerConfig{
			Conns:    conns,
			Reg:      reg,
			Recorder: rec,
			Interval: time.Hour, // would otherwise block forever
		})
		close(done)
	}()

	// Give the goroutine time to run its immediate first cycle.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Returned promptly.
	case <-time.After(2 * time.Second):
		t.Fatal("RunScheduler did not exit within 2s of cancel")
	}

	// Immediate first cycle should have produced 1 snapshot.
	snaps, _ := rec.Query(time.Now().Add(-time.Hour), time.Now().Add(time.Hour), Filter{})
	if len(snaps) < 1 {
		t.Errorf("expected at least 1 snapshot from immediate first cycle, got %d", len(snaps))
	}
}
