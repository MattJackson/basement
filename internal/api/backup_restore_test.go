package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	stdsync "sync"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/backup"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// restoreDriver is a focused in-memory driver used by the restore
// tests. Holds a bucket -> key -> bytes map and answers
// ListObjects/StatObject/StreamObject/PutObjectStream from it.
// Everything else returns zero values — the restore engine never
// reaches them.
//
// Concurrency: every accessor takes the same mu. The restore engine
// fans out across copyConcurrency workers so a naive map without a
// lock races under -race.
type restoreDriver struct {
	driverID string
	mu       stdsync.Mutex
	data     map[string]map[string][]byte // bucket -> key -> body
}

func newRestoreDriver(id string) *restoreDriver {
	return &restoreDriver{
		driverID: id,
		data:     map[string]map[string][]byte{},
	}
}

func (d *restoreDriver) put(bucket, key string, body []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.data[bucket] == nil {
		d.data[bucket] = map[string][]byte{}
	}
	d.data[bucket][key] = body
}

func (d *restoreDriver) Capabilities(_ context.Context) (driver.Caps, error) {
	// Different driverID per side so ServerSideCopy isn't taken — the
	// restore engine falls through to streaming, which is what we
	// want to exercise here. The detail driver-name flag does double
	// duty as the cache key in production so the test ID is unique.
	return driver.Caps{Driver: d.driverID}, nil
}
func (d *restoreDriver) HealthCheck(_ context.Context) (driver.HealthReport, error) {
	return driver.HealthReport{Status: "healthy"}, nil
}
func (d *restoreDriver) ListNodes(_ context.Context) ([]driver.Node, error)   { return nil, nil }
func (d *restoreDriver) GetLayout(_ context.Context) (driver.Layout, error)    { return driver.Layout{}, nil }
func (d *restoreDriver) StageLayout(_ context.Context, _ driver.LayoutChange) (driver.LayoutDiff, error) {
	return driver.LayoutDiff{}, nil
}
func (d *restoreDriver) ApplyLayout(_ context.Context) error  { return nil }
func (d *restoreDriver) RevertLayout(_ context.Context) error { return nil }
func (d *restoreDriver) ListBuckets(_ context.Context) ([]driver.Bucket, error) {
	return nil, nil
}
func (d *restoreDriver) GetBucket(_ context.Context, id string) (driver.Bucket, error) {
	return driver.Bucket{ID: id}, nil
}
func (d *restoreDriver) CreateBucket(_ context.Context, _ driver.BucketSpec) (driver.Bucket, error) {
	return driver.Bucket{}, nil
}
func (d *restoreDriver) UpdateBucket(_ context.Context, _ string, _ driver.BucketUpdate) (driver.Bucket, error) {
	return driver.Bucket{}, nil
}
func (d *restoreDriver) DeleteBucket(_ context.Context, _ string) error { return nil }
func (d *restoreDriver) ListKeys(_ context.Context) ([]driver.Key, error) { return nil, nil }
func (d *restoreDriver) GetKey(_ context.Context, _ string) (driver.Key, error) {
	return driver.Key{}, nil
}
func (d *restoreDriver) CreateKey(_ context.Context, _ driver.KeySpec) (driver.Key, error) {
	return driver.Key{}, nil
}
func (d *restoreDriver) UpdateKeyPermissions(_ context.Context, _ string, _ []driver.BucketPermission) error {
	return nil
}
func (d *restoreDriver) DeleteKey(_ context.Context, _ string) error { return nil }

func (d *restoreDriver) ListObjects(_ context.Context, bucket, prefix, _ /*continuation*/ string, delimiter string, _ int) (driver.ObjectPage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	page := driver.ObjectPage{}
	b, ok := d.data[bucket]
	if !ok {
		return page, nil
	}
	seenPrefix := map[string]bool{}
	for k, v := range b {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if delimiter != "" {
			// Group by delimiter — matches the snapshot-root walk in
			// listSnapshotTimestamps.
			rest := strings.TrimPrefix(k, prefix)
			if idx := strings.Index(rest, delimiter); idx >= 0 {
				common := prefix + rest[:idx+len(delimiter)]
				if !seenPrefix[common] {
					seenPrefix[common] = true
					page.CommonPrefixes = append(page.CommonPrefixes, common)
				}
				continue
			}
		}
		page.Objects = append(page.Objects, driver.ObjectInfo{
			Key:  k,
			Size: int64(len(v)),
		})
	}
	return page, nil
}

func (d *restoreDriver) StatObject(_ context.Context, bucket, key string) (driver.ObjectInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	body, ok := d.data[bucket][key]
	if !ok {
		return driver.ObjectInfo{}, errors.New("not found")
	}
	return driver.ObjectInfo{Key: key, Size: int64(len(body))}, nil
}

func (d *restoreDriver) PresignGet(_ context.Context, _, _ string, _ time.Duration) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, nil
}
func (d *restoreDriver) PresignPut(_ context.Context, _, _ string, _ time.Duration, _ string) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, nil
}
func (d *restoreDriver) DeleteObject(_ context.Context, bucket, key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.data[bucket], key)
	return nil
}
func (d *restoreDriver) StreamObject(_ context.Context, bucket, key, _ string) (driver.StreamResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	body, ok := d.data[bucket][key]
	if !ok {
		return driver.StreamResult{}, errors.New("not found")
	}
	return driver.StreamResult{
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}, nil
}
func (d *restoreDriver) PutObjectStream(_ context.Context, bucket, key string, reader io.Reader, _ string, _ int64) (driver.PutResult, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return driver.PutResult{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.data[bucket] == nil {
		d.data[bucket] = map[string][]byte{}
	}
	d.data[bucket][key] = body
	return driver.PutResult{}, nil
}
func (d *restoreDriver) ServerSideCopy(_ context.Context, _, _, _, _ string) error {
	// Tests intentionally use different driverIDs so the engine never
	// chooses this path; the implementation here is defensive only.
	return errors.New("unsupported in test")
}
func (d *restoreDriver) CreateMultipart(_ context.Context, _, _, _ string) (driver.MultipartUpload, error) {
	return driver.MultipartUpload{}, nil
}
func (d *restoreDriver) PresignUploadPart(_ context.Context, _ driver.MultipartUpload, _ int) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, nil
}
func (d *restoreDriver) CompleteMultipart(_ context.Context, _ driver.MultipartUpload, _ []driver.CompletedPart) error {
	return nil
}
func (d *restoreDriver) AbortMultipart(_ context.Context, _ driver.MultipartUpload) error {
	return nil
}
func (d *restoreDriver) LifecycleSupport() driver.LifecycleCapabilities {
	return driver.LifecycleCapabilities{Supported: false}
}
func (d *restoreDriver) GetLifecycle(_ context.Context, _ string) ([]driver.LifecycleRule, error) {
	return nil, nil
}
func (d *restoreDriver) PutLifecycle(_ context.Context, _ string, _ []driver.LifecycleRule) error {
	return nil
}
func (d *restoreDriver) PerBucketStatsAvailable() bool { return false }
func (d *restoreDriver) ScrubSupport() driver.ScrubCapability {
	return driver.ScrubCapability{Supported: false}
}
func (d *restoreDriver) ScrubState(_ context.Context) (driver.ScrubState, error) {
	return driver.ScrubState{}, driver.ErrUnsupported
}
func (d *restoreDriver) StartScrub(_ context.Context) error { return driver.ErrUnsupported }

// restoreDriverCounter monotonically increments so each test gets
// unique driver-factory names. driver.Register panics on duplicate
// registration; isolation per test means the registry inside a test
// always sees the matching test's restoreDriver instance rather than
// a stale closure from an earlier test in the same process.
var restoreDriverCounter atomicCounter

// atomicCounter is a goroutine-safe incrementing int. We avoid the
// sync/atomic dance for one counter by funneling through a mutex —
// only test setup ever calls Next.
type atomicCounter struct {
	mu stdsync.Mutex
	n  int
}

func (c *atomicCounter) Next() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return c.n
}

// newRestoreTestServer wires a Server with the backup subsystem +
// a registry that hands out our restoreDriver for any connection ID.
// We register a one-shot driver factory so registry.For builds the
// in-memory driver out of a stored Connection record. Each test
// scope gets a unique factory name so the global driver.Register
// map never sees a duplicate.
func newRestoreTestServer(t *testing.T) (*Server, *restoreDriver, *restoreDriver) {
	t.Helper()
	dataDir := t.TempDir()
	cfg := newTestConfig()
	cfg.DataDir = dataDir
	st, _ := store.Open(dataDir, 90*24*time.Hour)

	// Two driver instances: one source-side (where snapshots live),
	// one dest-side (where the restore writes to). The mock factory
	// matches "src-conn" / "dst-conn" by name.
	src := newRestoreDriver("restore-src")
	dst := newRestoreDriver("restore-dst")

	// Per-test unique factory names so driver.Register stays happy
	// across re-runs and parallel suites.
	idx := restoreDriverCounter.Next()
	srcName := fmt.Sprintf("restore-mock-src-%d", idx)
	dstName := fmt.Sprintf("restore-mock-dst-%d", idx)

	conns := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "src-conn", Driver: srcName, Config: map[string]string{}},
			{ID: "dst-conn", Driver: dstName, Config: map[string]string{}},
		},
	}
	driver.Register(srcName, func(_ driver.Config) (driver.Driver, error) { return src, nil })
	driver.Register(dstName, func(_ driver.Config) (driver.Driver, error) { return dst, nil })
	reg := driver.NewRegistry(conns)
	srv := New(cfg, st, conns, nil, reg)
	// Wire a real FileLogger so the audit_emit calls in the handler
	// land somewhere we can query in the audit assertion test.
	srv.SetAuditLogger(audit.NewFileLogger(dataDir))

	bs, err := backup.NewFileStore(dataDir)
	if err != nil {
		t.Fatalf("backup.NewFileStore: %v", err)
	}
	sched := backup.NewScheduler(bs, backup.RunnerFunc(func(_ context.Context, _ backup.Backup) backup.BackupResult {
		return backup.BackupResult{Success: true}
	}), nil)
	srv.SetBackups(bs, sched)
	t.Cleanup(func() {
		sched.Stop()
	})
	return srv, src, dst
}

// helper: insert a snapshot tree at the right on-disk layout.
func seedSnapshot(d *restoreDriver, bucket, backupName string, ts time.Time, files map[string]string) {
	prefix := backup.SnapshotPrefix(backupName, ts)
	for k, v := range files {
		d.put(bucket, prefix+k, []byte(v))
	}
}

// helper: store a backup record.
func createBackupRecord(t *testing.T, srv *Server, ownerID, name string) backup.Backup {
	t.Helper()
	rec, err := srv.backups.Create(context.Background(), backup.Backup{
		OwnerUserID: ownerID,
		Name:        name,
		SrcRegionID: "src-conn",
		SrcBucket:   "source-bucket",
		DstRegionID: "src-conn",
		DstBucket:   "backup-bucket",
		Schedule:    backup.ScheduleManual,
		Mode:        backup.BackupModeSnapshot,
		Retention:   backup.DefaultRetention(),
	})
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}
	return rec
}

// TestRestore_Latest_CopiesAllObjects asserts the happy path: a
// snapshot-mode backup with one snapshot on disk, restored with
// SnapshotTimestamp="latest" + a fresh destination, copies every
// file across.
func TestRestore_Latest_CopiesAllObjects(t *testing.T) {
	srv, src, dst := newRestoreTestServer(t)
	rec := createBackupRecord(t, srv, "matthew", "lsi-to-cheshire")
	ts := time.Date(2026, 5, 21, 3, 0, 0, 0, time.UTC)
	seedSnapshot(src, rec.DstBucket, rec.Name, ts, map[string]string{
		"photos/a.jpg":  "alpha",
		"photos/b.jpg":  "beta",
		"docs/spec.md": "gamma",
	})

	body, _ := json.Marshal(backup.RestoreRequest{
		SnapshotTimestamp: backup.RestoreLatest,
		DstRegionID:       "dst-conn",
		DstBucket:         "restored-bucket",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/backups/"+rec.ID+"/restore", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var result backup.RestoreResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected Success=true, got %+v", result)
	}
	if result.ObjectsCopied != 3 {
		t.Errorf("ObjectsCopied = %d, want 3", result.ObjectsCopied)
	}
	if result.ObjectsSkipped != 0 {
		t.Errorf("ObjectsSkipped = %d, want 0", result.ObjectsSkipped)
	}
	if result.ResolvedSnapshot != ts.Format(backup.SnapshotTimestampLayout) {
		t.Errorf("ResolvedSnapshot = %q, want %q", result.ResolvedSnapshot, ts.Format(backup.SnapshotTimestampLayout))
	}
	if got, want := len(dst.data["restored-bucket"]), 3; got != want {
		t.Errorf("dst object count = %d, want %d", got, want)
	}
	if string(dst.data["restored-bucket"]["photos/a.jpg"]) != "alpha" {
		t.Errorf("photos/a.jpg contents mismatch: %q", dst.data["restored-bucket"]["photos/a.jpg"])
	}
}

// TestRestore_LatestPicksNewest verifies the "latest" resolver picks
// the most-recent timestamp among several snapshots on disk.
func TestRestore_LatestPicksNewest(t *testing.T) {
	srv, src, dst := newRestoreTestServer(t)
	rec := createBackupRecord(t, srv, "matthew", "lsi")
	older := time.Date(2026, 5, 20, 3, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 21, 3, 0, 0, 0, time.UTC)
	seedSnapshot(src, rec.DstBucket, rec.Name, older, map[string]string{"x.txt": "OLD"})
	seedSnapshot(src, rec.DstBucket, rec.Name, newer, map[string]string{"x.txt": "NEW"})

	body, _ := json.Marshal(backup.RestoreRequest{
		SnapshotTimestamp: backup.RestoreLatest,
		DstRegionID:       "dst-conn",
		DstBucket:         "restored",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/backups/"+rec.ID+"/restore", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := string(dst.data["restored"]["x.txt"]); got != "NEW" {
		t.Errorf("restored x.txt = %q, want %q (latest snapshot)", got, "NEW")
	}
	var result backup.RestoreResult
	_ = json.Unmarshal(rr.Body.Bytes(), &result)
	if result.ResolvedSnapshot != newer.Format(backup.SnapshotTimestampLayout) {
		t.Errorf("ResolvedSnapshot = %q, want newest %q", result.ResolvedSnapshot, newer.Format(backup.SnapshotTimestampLayout))
	}
}

// TestRestore_OverwriteFalseSkipsExisting verifies that
// OverwriteExisting=false skips objects whose keys already exist at
// the destination — and that the skipped count is reported.
func TestRestore_OverwriteFalseSkipsExisting(t *testing.T) {
	srv, src, dst := newRestoreTestServer(t)
	rec := createBackupRecord(t, srv, "matthew", "lsi")
	ts := time.Date(2026, 5, 21, 3, 0, 0, 0, time.UTC)
	seedSnapshot(src, rec.DstBucket, rec.Name, ts, map[string]string{
		"a.txt": "FROM_SNAPSHOT",
		"b.txt": "FROM_SNAPSHOT",
	})
	// Pre-populate dst with one of the keys; the restore must skip it.
	dst.put("restored", "a.txt", []byte("EXISTING"))

	body, _ := json.Marshal(backup.RestoreRequest{
		SnapshotTimestamp: backup.RestoreLatest,
		DstRegionID:       "dst-conn",
		DstBucket:         "restored",
		OverwriteExisting: false,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/backups/"+rec.ID+"/restore", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var result backup.RestoreResult
	_ = json.Unmarshal(rr.Body.Bytes(), &result)
	if result.ObjectsCopied != 1 {
		t.Errorf("ObjectsCopied = %d, want 1 (b.txt)", result.ObjectsCopied)
	}
	if result.ObjectsSkipped != 1 {
		t.Errorf("ObjectsSkipped = %d, want 1 (a.txt was already present)", result.ObjectsSkipped)
	}
	if got := string(dst.data["restored"]["a.txt"]); got != "EXISTING" {
		t.Errorf("a.txt was unexpectedly overwritten: got %q", got)
	}
	if got := string(dst.data["restored"]["b.txt"]); got != "FROM_SNAPSHOT" {
		t.Errorf("b.txt not restored: got %q", got)
	}
}

// TestRestore_OverwriteTrueReplacesExisting verifies that
// OverwriteExisting=true unconditionally replaces destination objects.
func TestRestore_OverwriteTrueReplacesExisting(t *testing.T) {
	srv, src, dst := newRestoreTestServer(t)
	rec := createBackupRecord(t, srv, "matthew", "lsi")
	ts := time.Date(2026, 5, 21, 3, 0, 0, 0, time.UTC)
	seedSnapshot(src, rec.DstBucket, rec.Name, ts, map[string]string{
		"a.txt": "FROM_SNAPSHOT",
	})
	dst.put("restored", "a.txt", []byte("EXISTING"))

	body, _ := json.Marshal(backup.RestoreRequest{
		SnapshotTimestamp: backup.RestoreLatest,
		DstRegionID:       "dst-conn",
		DstBucket:         "restored",
		OverwriteExisting: true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/backups/"+rec.ID+"/restore", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := string(dst.data["restored"]["a.txt"]); got != "FROM_SNAPSHOT" {
		t.Errorf("a.txt was not overwritten: got %q", got)
	}
}

// TestRestore_ExplicitTimestamp verifies a specific (non-"latest")
// timestamp resolves to that exact snapshot.
func TestRestore_ExplicitTimestamp(t *testing.T) {
	srv, src, dst := newRestoreTestServer(t)
	rec := createBackupRecord(t, srv, "matthew", "lsi")
	older := time.Date(2026, 5, 20, 3, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 21, 3, 0, 0, 0, time.UTC)
	seedSnapshot(src, rec.DstBucket, rec.Name, older, map[string]string{"x.txt": "OLD"})
	seedSnapshot(src, rec.DstBucket, rec.Name, newer, map[string]string{"x.txt": "NEW"})

	body, _ := json.Marshal(backup.RestoreRequest{
		SnapshotTimestamp: older.Format(backup.SnapshotTimestampLayout),
		DstRegionID:       "dst-conn",
		DstBucket:         "restored",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/backups/"+rec.ID+"/restore", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := string(dst.data["restored"]["x.txt"]); got != "OLD" {
		t.Errorf("expected OLD (explicit timestamp), got %q", got)
	}
}

// TestRestore_SnapshotNotFound asserts a 404 when the requested
// timestamp doesn't exist on disk.
func TestRestore_SnapshotNotFound(t *testing.T) {
	srv, src, _ := newRestoreTestServer(t)
	rec := createBackupRecord(t, srv, "matthew", "lsi")
	ts := time.Date(2026, 5, 21, 3, 0, 0, 0, time.UTC)
	seedSnapshot(src, rec.DstBucket, rec.Name, ts, map[string]string{"x.txt": "X"})

	body, _ := json.Marshal(backup.RestoreRequest{
		SnapshotTimestamp: "1999-01-01_00:00:00", // nothing seeded here
		DstRegionID:       "dst-conn",
		DstBucket:         "restored",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/backups/"+rec.ID+"/restore", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestRestore_MirrorBackupRejected asserts that asking to restore a
// mirror-mode backup returns 400 — only snapshot-mode backups have
// the layout the restore engine needs.
func TestRestore_MirrorBackupRejected(t *testing.T) {
	srv, _, _ := newRestoreTestServer(t)
	mirror, err := srv.backups.Create(context.Background(), backup.Backup{
		OwnerUserID: "matthew",
		Name:        "mirror-bk",
		SrcRegionID: "src-conn", SrcBucket: "source-bucket",
		DstRegionID: "src-conn", DstBucket: "backup-bucket",
		Schedule: backup.ScheduleManual,
		Mode:     backup.BackupModeMirror,
	})
	if err != nil {
		t.Fatalf("create mirror backup: %v", err)
	}

	body, _ := json.Marshal(backup.RestoreRequest{
		SnapshotTimestamp: backup.RestoreLatest,
		DstRegionID:       "dst-conn",
		DstBucket:         "restored",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/backups/"+mirror.ID+"/restore", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestRestore_RequiresAuth asserts the endpoint refuses anonymous
// callers — same gate as every /user route.
func TestRestore_RequiresAuth(t *testing.T) {
	srv, _, _ := newRestoreTestServer(t)
	rec := createBackupRecord(t, srv, "matthew", "lsi")
	body, _ := json.Marshal(backup.RestoreRequest{SnapshotTimestamp: backup.RestoreLatest})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/backups/"+rec.ID+"/restore", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestRestore_OtherUserGets404 asserts a non-owner sees a 404 (not
// 403) — same disclosure-avoidance pattern as the rest of the user
// backup handlers.
func TestRestore_OtherUserGets404(t *testing.T) {
	srv, _, _ := newRestoreTestServer(t)
	rec := createBackupRecord(t, srv, "matthew", "lsi")
	body, _ := json.Marshal(backup.RestoreRequest{SnapshotTimestamp: backup.RestoreLatest})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/backups/"+rec.ID+"/restore", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(userCookie(t, "someone-else"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestRestore_AuditEmitsStartAndComplete asserts the audit log
// records both backup:restore_start and backup:restore_complete on
// a successful run.
func TestRestore_AuditEmitsStartAndComplete(t *testing.T) {
	srv, src, _ := newRestoreTestServer(t)
	rec := createBackupRecord(t, srv, "matthew", "lsi")
	ts := time.Date(2026, 5, 21, 3, 0, 0, 0, time.UTC)
	seedSnapshot(src, rec.DstBucket, rec.Name, ts, map[string]string{"x.txt": "X"})

	body, _ := json.Marshal(backup.RestoreRequest{
		SnapshotTimestamp: backup.RestoreLatest,
		DstRegionID:       "dst-conn",
		DstBucket:         "restored",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/backups/"+rec.ID+"/restore", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Read audit events back via the FileLogger we wired in
	// newRestoreTestServer. Window is from the test's start through
	// "now"; both calls land on the same UTC day so no rotation
	// shenanigans here.
	events, err := srv.audit.Query(time.Now().Add(-1*time.Hour), time.Now().Add(time.Hour), audit.QueryFilter{Limit: 100})
	if err != nil {
		t.Fatalf("audit.Query: %v", err)
	}
	saw := map[string]bool{}
	for _, e := range events {
		if strings.HasPrefix(e.Action, "backup:restore_") {
			saw[e.Action] = true
		}
	}
	if !saw["backup:restore_start"] {
		t.Errorf("expected backup:restore_start audit entry, got %v", saw)
	}
	if !saw["backup:restore_complete"] {
		t.Errorf("expected backup:restore_complete audit entry, got %v", saw)
	}
}

