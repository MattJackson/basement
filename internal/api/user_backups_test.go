package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/backup"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// newBackupTestServer wires a Server with a real backup store +
// scheduler so the handler tests exercise the same code path as
// production. Returns the Server + a cleanup that wipes the temp
// dir. Pass userToken alongside as the JWT for the request cookies.
func newBackupTestServer(t *testing.T) *Server {
	t.Helper()
	dataDir := t.TempDir()
	cfg := newTestConfig()
	cfg.DataDir = dataDir
	st, _ := store.Open(dataDir, 90*24*time.Hour)
	srv := New(cfg, st, &testMockConnectionStore{}, nil, nil)

	bs, err := backup.NewFileStore(dataDir)
	if err != nil {
		t.Fatalf("backup.NewFileStore: %v", err)
	}
	// Wire a no-op runner so Trigger doesn't blow up trying to
	// dial real drivers. Handler tests assert plumbing, not the
	// engine bridge — backup_runner_test.go owns that.
	sched := backup.NewScheduler(bs, backup.RunnerFunc(func(_ context.Context, _ backup.Backup) backup.BackupResult {
		return backup.BackupResult{Success: true}
	}), nil)
	srv.SetBackups(bs, sched)
	t.Cleanup(func() {
		sched.Stop()
		os.RemoveAll(dataDir)
	})
	return srv
}

// userCookie attaches a USER-mode JWT session cookie to the request.
// We mint with auth.IssueToken (USER default) — no admin role is
// needed for /user/backups.
func userCookie(t *testing.T, userID string) *http.Cookie {
	t.Helper()
	token, err := auth.IssueToken(testSecret, userID, "user", false, time.Hour)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	return &http.Cookie{
		Name:     "__Host-basement_session",
		Value:    token,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
}

// TestCreateBackup_NoAuth: unauthenticated POST returns 401.
func TestCreateBackup_NoAuth(t *testing.T) {
	srv := newBackupTestServer(t)
	body := map[string]interface{}{
		"name":        "test",
		"srcRegionId": "r1", "srcBucket": "b1",
		"dstRegionId": "r2", "dstBucket": "b2",
		"schedule": backup.ScheduleManual,
	}
	req := newJSONRequest("/api/v1/user/backups", body)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestCreateBackup_Success: authenticated POST persists and returns
// the new record with a generated ID.
func TestCreateBackup_Success(t *testing.T) {
	srv := newBackupTestServer(t)
	body := map[string]interface{}{
		"name":        "lsi to cheshire",
		"srcRegionId": "r1", "srcBucket": "photos",
		"dstRegionId": "r2", "dstBucket": "photos-backup",
		"schedule": backup.ScheduleManual,
	}
	req := newJSONRequest("/api/v1/user/backups", body)
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got backup.Backup
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID == "" {
		t.Fatalf("expected ID, got empty")
	}
	if got.OwnerUserID != "matthew" {
		t.Fatalf("expected OwnerUserID=matthew, got %q", got.OwnerUserID)
	}
}

// TestCreateBackup_InvalidSchedule: a bad cron expression returns
// 400 INVALID_SCHEDULE before persisting.
func TestCreateBackup_InvalidSchedule(t *testing.T) {
	srv := newBackupTestServer(t)
	body := map[string]interface{}{
		"name":        "bad",
		"srcRegionId": "r1", "srcBucket": "b1",
		"dstRegionId": "r2", "dstBucket": "b2",
		"schedule": "this is not cron",
	}
	req := newJSONRequest("/api/v1/user/backups", body)
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Confirm nothing persisted.
	got, _ := srv.backups.ListForUser(req.Context(), "matthew")
	if len(got) != 0 {
		t.Fatalf("expected nothing persisted on invalid schedule, got %d", len(got))
	}
}

// TestListBackups_OnlyOwn: a backup owned by user A is NOT returned
// to user B's list call.
func TestListBackups_OnlyOwn(t *testing.T) {
	srv := newBackupTestServer(t)
	// Seed one for matthew, one for alice.
	body := map[string]interface{}{
		"name":        "mine",
		"srcRegionId": "r1", "srcBucket": "b1",
		"dstRegionId": "r2", "dstBucket": "b2",
		"schedule": backup.ScheduleManual,
	}
	for _, u := range []string{"matthew", "alice"} {
		req := newJSONRequest("/api/v1/user/backups", body)
		req.AddCookie(userCookie(t, u))
		rr := httptest.NewRecorder()
		srv.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("seed for %s: %d", u, rr.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/backups", nil)
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got []backup.Backup
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if len(got) != 1 {
		t.Fatalf("expected 1 backup for matthew, got %d", len(got))
	}
	if got[0].OwnerUserID != "matthew" {
		t.Fatalf("expected only matthew's backup, got owner=%q", got[0].OwnerUserID)
	}
}

// TestGetBackup_Ownership: accessing someone else's backup returns
// 404 (not 403) to prevent existence leak.
func TestGetBackup_Ownership(t *testing.T) {
	srv := newBackupTestServer(t)

	body := map[string]interface{}{
		"name":        "alice's",
		"srcRegionId": "r1", "srcBucket": "b1",
		"dstRegionId": "r2", "dstBucket": "b2",
		"schedule": backup.ScheduleManual,
	}
	req := newJSONRequest("/api/v1/user/backups", body)
	req.AddCookie(userCookie(t, "alice"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	var created backup.Backup
	_ = json.NewDecoder(rr.Body).Decode(&created)

	// matthew tries to GET alice's backup -> 404.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/user/backups/"+created.ID, nil)
	getReq.AddCookie(userCookie(t, "matthew"))
	getRR := httptest.NewRecorder()
	srv.router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", getRR.Code, getRR.Body.String())
	}
}

// TestRunBackup_KicksOffRunner: POST /run invokes the runner and
// eventually writes a result into history.
func TestRunBackup_KicksOffRunner(t *testing.T) {
	dataDir := t.TempDir()
	cfg := newTestConfig()
	cfg.DataDir = dataDir
	st, _ := store.Open(dataDir, 90*24*time.Hour)
	srv := New(cfg, st, &testMockConnectionStore{}, nil, nil)

	bs, err := backup.NewFileStore(dataDir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	done := make(chan struct{}, 1)
	sched := backup.NewScheduler(bs, backup.RunnerFunc(func(_ context.Context, _ backup.Backup) backup.BackupResult {
		defer func() {
			select {
			case done <- struct{}{}:
			default:
			}
		}()
		return backup.BackupResult{
			StartedAt:     time.Now().UTC(),
			CompletedAt:   time.Now().UTC().Add(time.Second),
			ObjectsCopied: 7,
			Success:       true,
		}
	}), nil)
	srv.SetBackups(bs, sched)
	t.Cleanup(func() { sched.Stop() })

	// Seed a manual backup directly via the store (bypass the
	// handler) so we know the ID up-front.
	created, err := bs.Create(context.Background(), backup.Backup{
		OwnerUserID: "matthew",
		Name:        "test",
		Schedule:    backup.ScheduleManual,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	runReq := httptest.NewRequest(http.MethodPost, "/api/v1/user/backups/"+created.ID+"/run", nil)
	runReq.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, runReq)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rr.Code, rr.Body.String())
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("runner was not invoked within 2s")
	}

	// Give the trigger goroutine a beat to write the result back.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := bs.Get(context.Background(), created.ID)
		if got.LastResult != nil && got.LastResult.ObjectsCopied == 7 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected LastResult.ObjectsCopied=7 within 2s")
}

// TestDeleteBackup_RemovesScheduleEntry: deleting a scheduled backup
// also tears down its cron entry.
func TestDeleteBackup_RemovesScheduleEntry(t *testing.T) {
	srv := newBackupTestServer(t)

	body := map[string]interface{}{
		"name":        "to delete",
		"srcRegionId": "r1", "srcBucket": "b1",
		"dstRegionId": "r2", "dstBucket": "b2",
		"schedule": "0 3 * * *", // daily at 03:00
	}
	req := newJSONRequest("/api/v1/user/backups", body)
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: %d", rr.Code)
	}
	var created backup.Backup
	_ = json.NewDecoder(rr.Body).Decode(&created)
	if srv.backupSched.EntryCount() != 1 {
		t.Fatalf("expected 1 cron entry after create, got %d", srv.backupSched.EntryCount())
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/user/backups/"+created.ID, nil)
	delReq.AddCookie(userCookie(t, "matthew"))
	delRR := httptest.NewRecorder()
	srv.router.ServeHTTP(delRR, delReq)
	if delRR.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", delRR.Code, delRR.Body.String())
	}
	if srv.backupSched.EntryCount() != 0 {
		t.Fatalf("expected cron entry removed after delete, got %d", srv.backupSched.EntryCount())
	}
}

// TestCreateBackup_DefaultMirrorMode: omitting mode + retention on
// the wire produces a Mirror-mode Backup with a zero retention
// policy — the back-compat path for v1.5.0a clients.
func TestCreateBackup_DefaultMirrorMode(t *testing.T) {
	srv := newBackupTestServer(t)
	body := map[string]interface{}{
		"name":        "implicit mirror",
		"srcRegionId": "r1", "srcBucket": "b1",
		"dstRegionId": "r2", "dstBucket": "b2",
		"schedule": backup.ScheduleManual,
	}
	req := newJSONRequest("/api/v1/user/backups", body)
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got backup.Backup
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.ResolveMode() != backup.BackupModeMirror {
		t.Fatalf("expected mode=mirror, got %q", got.Mode)
	}
	if !got.Retention.IsZero() {
		t.Fatalf("expected zero retention on mirror, got %+v", got.Retention)
	}
}

// TestCreateBackup_SnapshotModeDefaultsRetention: explicitly choosing
// snapshot mode without a retention payload backfills the GFS
// default {7,4,12} so the runner never sees a missing policy.
func TestCreateBackup_SnapshotModeDefaultsRetention(t *testing.T) {
	srv := newBackupTestServer(t)
	body := map[string]interface{}{
		"name":        "snapshot default",
		"srcRegionId": "r1", "srcBucket": "b1",
		"dstRegionId": "r2", "dstBucket": "b2",
		"schedule":    backup.ScheduleManual,
		"mode":        "snapshot",
	}
	req := newJSONRequest("/api/v1/user/backups", body)
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got backup.Backup
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Mode != backup.BackupModeSnapshot {
		t.Fatalf("expected mode=snapshot, got %q", got.Mode)
	}
	want := backup.DefaultRetention()
	if got.Retention != want {
		t.Fatalf("expected default retention %+v, got %+v", want, got.Retention)
	}
}

// TestCreateBackup_SnapshotModeCustomRetention: the operator's
// non-zero keep counts come back verbatim.
func TestCreateBackup_SnapshotModeCustomRetention(t *testing.T) {
	srv := newBackupTestServer(t)
	body := map[string]interface{}{
		"name":        "custom retention",
		"srcRegionId": "r1", "srcBucket": "b1",
		"dstRegionId": "r2", "dstBucket": "b2",
		"schedule":    backup.ScheduleManual,
		"mode":        "snapshot",
		"retention":   map[string]int{"keepDaily": 3, "keepWeekly": 2, "keepMonthly": 1},
	}
	req := newJSONRequest("/api/v1/user/backups", body)
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got backup.Backup
	_ = json.NewDecoder(rr.Body).Decode(&got)
	want := backup.RetentionPolicy{KeepDaily: 3, KeepWeekly: 2, KeepMonthly: 1}
	if got.Retention != want {
		t.Fatalf("expected retention %+v, got %+v", want, got.Retention)
	}
}

// TestCreateBackup_InvalidMode: a mode string outside the enum is
// rejected with 400 before persisting.
func TestCreateBackup_InvalidMode(t *testing.T) {
	srv := newBackupTestServer(t)
	body := map[string]interface{}{
		"name":        "bad mode",
		"srcRegionId": "r1", "srcBucket": "b1",
		"dstRegionId": "r2", "dstBucket": "b2",
		"schedule":    backup.ScheduleManual,
		"mode":        "incremental",
	}
	req := newJSONRequest("/api/v1/user/backups", body)
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestCreateBackup_NegativeRetentionRejected: keep-counts must be
// >= 0; a -1 is a client bug we surface eagerly.
func TestCreateBackup_NegativeRetentionRejected(t *testing.T) {
	srv := newBackupTestServer(t)
	body := map[string]interface{}{
		"name":        "negative",
		"srcRegionId": "r1", "srcBucket": "b1",
		"dstRegionId": "r2", "dstBucket": "b2",
		"schedule":    backup.ScheduleManual,
		"mode":        "snapshot",
		"retention":   map[string]int{"keepDaily": -1},
	}
	req := newJSONRequest("/api/v1/user/backups", body)
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestSnapshotsList_UserRegionDestination_FallsBackToRegionDriver
// covers the v1.11.0.6 BUG05 fix: when a snapshot-mode backup's
// destination is a UserRegion (no admin Connection registered at the
// region's endpoint), the handler used to return 500 with the raw
// "region has no admin bridge (endpoint %q)" string from
// resolveBackupConn. The fix falls back to building a region-scoped
// driver from the UserRegion's stored S3 key and listing snapshots
// via that driver — which works because snapshot enumeration is pure
// S3 ListObjects, not an admin op.
//
// Test strategy: wire UserRegions, register a region under the
// caller's userID, point a snapshot backup's DstRegionID at the
// region, then assert the handler returns 200 + the canonical wire
// shape (empty array, since no snapshot prefixes exist yet on the
// in-memory mock driver).
func TestSnapshotsList_UserRegionDestination_FallsBackToRegionDriver(t *testing.T) {
	dataDir := t.TempDir()
	cfg := newTestConfig()
	cfg.DataDir = dataDir
	st, err := store.Open(dataDir, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := st.WireUserRegions(cfg.JWT.Secret); err != nil {
		t.Fatalf("WireUserRegions: %v", err)
	}

	mock := &testMockDriver{}
	mock.listObjectsFunc = func(_ context.Context, _, _, _, _ string, _ int) (driver.ObjectPage, error) {
		// No snapshot prefixes yet — handler should return [].
		return driver.ObjectPage{}, nil
	}

	conns := &testMockConnectionStore{}
	reg := driver.NewRegistry(conns)
	reg.SetUserRegionsStore(st.UserRegions())
	reg.SetRegionDriverBuilder(func(_, _, _, _, _ string) (driver.Driver, error) {
		return mock, nil
	})

	srv := New(cfg, st, conns, nil, reg)
	bs, _ := backup.NewFileStore(dataDir)
	sched := backup.NewScheduler(bs, backup.RunnerFunc(func(_ context.Context, _ backup.Backup) backup.BackupResult {
		return backup.BackupResult{Success: true}
	}), nil)
	srv.SetBackups(bs, sched)
	t.Cleanup(func() {
		sched.Stop()
		os.RemoveAll(dataDir)
	})

	// Create a UserRegion under matthew. The store hands us back the
	// canonical record with its assigned ID — that ID is what the
	// backup's DstRegionID will reference. SecretKeyEnc carries the
	// PLAINTEXT secret on the Create input; the store encrypts it
	// immediately and never holds plaintext beyond the call.
	ur := st.UserRegions()
	region, err := ur.Create(context.Background(), store.UserRegion{
		UserID:       "matthew",
		Alias:        "dst",
		Endpoint:     "https://s3.example.com",
		AccessKeyID:  "AK",
		SecretKeyEnc: []byte("SK"),
	})
	if err != nil {
		t.Fatalf("UserRegions.Create: %v", err)
	}

	created, err := bs.Create(context.Background(), backup.Backup{
		OwnerUserID: "matthew",
		Name:        "user-region-backup",
		Schedule:    backup.ScheduleManual,
		Mode:        backup.BackupModeSnapshot,
		DstRegionID: region.ID,
		DstBucket:   "dst-bucket",
		Retention:   backup.DefaultRetention(),
	})
	if err != nil {
		t.Fatalf("backup.Create: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/backups/"+created.ID+"/snapshots", nil)
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 (region driver fallback), got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if body != "[]\n" && body != "[]" {
		t.Fatalf("expected empty array (no prefixes), got %q", body)
	}
}

// TestSnapshotsList_MirrorReturnsEmpty: a mirror-mode backup never
// has snapshots — the endpoint short-circuits to [] without dialling
// the destination driver. Important: lets the detail page poll the
// endpoint unconditionally without producing 5xx on mirror backups.
func TestSnapshotsList_MirrorReturnsEmpty(t *testing.T) {
	srv := newBackupTestServer(t)
	created, err := srv.backups.Create(context.Background(), backup.Backup{
		OwnerUserID: "matthew",
		Name:        "mirror",
		Schedule:    backup.ScheduleManual,
		Mode:        backup.BackupModeMirror,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/backups/"+created.ID+"/snapshots", nil)
	req.AddCookie(userCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if body != "[]\n" && body != "[]" {
		t.Fatalf("expected empty array, got %q", body)
	}
}
