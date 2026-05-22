// Package api: backup -> restore runner bridge (v1.5.0c).
//
// Restore is the inverse of a snapshot backup run: copy the contents
// of {Backup.DstBucket}/{slug(Name)}/{timestamp}/ back out to an
// operator-chosen destination. We reuse the sync engine's copy path
// rather than duplicate streaming — the driver-aware ServerSideCopy
// fallback + bounded parallelism are exactly what we want here too.
//
// The restore loop sits next to the backup runner instead of inside
// internal/backup/ for the same reason backup_runner.go does: it
// needs the driver registry and the region resolver, both of which
// only exist on Server. Pure helpers (snapshot prefix math, timestamp
// parse, retention scan) stay in the backup package.
//
// Semantics quick-reference:
//
//   - SnapshotTimestamp="latest"           -> pick newest snapshot at
//                                              request time.
//   - SnapshotTimestamp="2026-05-21_03:00:00" -> exact match, 404 if
//                                              that timestamp no
//                                              longer exists.
//   - OverwriteExisting=false (default)    -> skip any object whose
//                                              key already exists at
//                                              the destination,
//                                              regardless of ETag.
//                                              Counts via
//                                              ObjectsSkipped.
//   - OverwriteExisting=true               -> unconditionally
//                                              re-write every object.
package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	stdsync "sync"
	"time"

	"github.com/mattjackson/basement/internal/backup"
	"github.com/mattjackson/basement/internal/driver"
)

// restoreConcurrency caps the parallel copy workers a single restore
// run will spin up. Matches sync.NewEngine's default fan-out so the
// two side-by-side patterns don't surprise an operator who's read the
// sync engine's docs.
const restoreConcurrency = 4

// errSnapshotNotFound is returned when a non-"latest" SnapshotTimestamp
// has no matching prefix at the destination. Surfaces as a 404 on the
// HTTP path so the wizard can render a clean "snapshot was pruned
// since you opened this page" message.
var errSnapshotNotFound = errors.New("snapshot timestamp not found")

// runRestore executes a single restore and returns the outcome. The
// HTTP handler runs this synchronously — restores are usually
// quicker than a full backup (smaller, no schedule fan-out) and the
// wizard wants the summary inline. For genuinely huge restores the
// client can hit the same endpoint and tolerate the long response.
func (s *Server) runRestore(ctx context.Context, b backup.Backup, req backup.RestoreRequest) (backup.RestoreResult, error) {
	started := time.Now().UTC()
	result := backup.RestoreResult{StartedAt: started}

	if b.ResolveMode() != backup.BackupModeSnapshot {
		result.CompletedAt = time.Now().UTC()
		return result, fmt.Errorf("restore requires a snapshot-mode backup; this backup is mirror-mode")
	}

	// Source = the backup's destination bucket. Destination = the
	// operator-supplied target. Resolve both via the same region
	// bridge the runner uses so a UserRegion.ID is accepted on
	// either side.
	srcConn, err := s.resolveBackupConn(ctx, b.OwnerUserID, b.DstRegionID)
	if err != nil {
		result.CompletedAt = time.Now().UTC()
		result.Errors = []string{fmt.Sprintf("resolve source: %v", err)}
		return result, err
	}
	dstRegion := strings.TrimSpace(req.DstRegionID)
	if dstRegion == "" {
		// Empty destination region -> restore in place (back to the
		// original source). The wizard usually pre-fills this so the
		// fallback is mainly a safety net for API callers.
		dstRegion = b.SrcRegionID
	}
	dstBucket := strings.TrimSpace(req.DstBucket)
	if dstBucket == "" {
		dstBucket = b.SrcBucket
	}
	dstConn, err := s.resolveBackupConn(ctx, b.OwnerUserID, dstRegion)
	if err != nil {
		result.CompletedAt = time.Now().UTC()
		result.Errors = []string{fmt.Sprintf("resolve destination: %v", err)}
		return result, err
	}

	if s.reg == nil {
		result.CompletedAt = time.Now().UTC()
		result.Errors = []string{"driver registry is not wired"}
		return result, fmt.Errorf("driver registry not wired")
	}
	srcDrv, err := s.reg.For(ctx, srcConn)
	if err != nil {
		result.CompletedAt = time.Now().UTC()
		result.Errors = []string{fmt.Sprintf("open source driver: %v", err)}
		return result, err
	}
	dstDrv, err := s.reg.For(ctx, dstConn)
	if err != nil {
		result.CompletedAt = time.Now().UTC()
		result.Errors = []string{fmt.Sprintf("open destination driver: %v", err)}
		return result, err
	}

	// Resolve the snapshot timestamp (literal "latest" -> newest on
	// disk). On-disk parse is the authoritative check — if the
	// requested timestamp doesn't show up under the snapshot root,
	// we surface a not-found error rather than silently producing an
	// empty restore.
	ts, err := s.resolveRestoreTimestamp(ctx, b, srcDrv, req.SnapshotTimestamp)
	if err != nil {
		result.CompletedAt = time.Now().UTC()
		result.Errors = []string{err.Error()}
		return result, err
	}
	result.ResolvedSnapshot = ts.UTC().Format(backup.SnapshotTimestampLayout)

	srcPrefix := backup.SnapshotPrefix(b.Name, ts)
	dstPrefix := strings.TrimSpace(req.DstPrefix)

	copied, skipped, bytesCopied, errs := s.copyRestoreTree(ctx, srcDrv, dstDrv, b.DstBucket, srcPrefix, dstBucket, dstPrefix, req.OverwriteExisting)
	result.ObjectsCopied = copied
	result.ObjectsSkipped = skipped
	result.BytesCopied = bytesCopied
	result.Errors = errs
	result.CompletedAt = time.Now().UTC()
	result.Success = len(errs) == 0
	return result, nil
}

// resolveRestoreTimestamp picks the snapshot to read from. "latest"
// resolves to the newest timestamp under the backup's snapshot root;
// any other value must round-trip through ParseSnapshotTimestamp AND
// match an actual on-disk prefix, otherwise we surface
// errSnapshotNotFound.
func (s *Server) resolveRestoreTimestamp(ctx context.Context, b backup.Backup, srcDrv driver.Driver, requested string) (time.Time, error) {
	root := backup.SnapshotRoot(b.Name)
	timestamps, err := listSnapshotTimestamps(ctx, srcDrv, b.DstBucket, root)
	if err != nil {
		return time.Time{}, fmt.Errorf("list snapshots: %w", err)
	}
	if len(timestamps) == 0 {
		return time.Time{}, fmt.Errorf("no snapshots available for backup %q", b.Name)
	}

	requested = strings.TrimSpace(requested)
	if requested == "" || requested == backup.RestoreLatest {
		// Newest first.
		newest := timestamps[0]
		for _, ts := range timestamps[1:] {
			if ts.After(newest) {
				newest = ts
			}
		}
		return newest, nil
	}

	want, ok := backup.ParseSnapshotTimestamp(requested)
	if !ok {
		return time.Time{}, fmt.Errorf("invalid snapshot timestamp %q (expected layout %s or %q)", requested, backup.SnapshotTimestampLayout, backup.RestoreLatest)
	}
	for _, ts := range timestamps {
		if ts.Equal(want) {
			return ts, nil
		}
	}
	return time.Time{}, errSnapshotNotFound
}

// copyRestoreTree walks the snapshot prefix and copies every object
// to the destination with bounded parallelism. Returns
// (objectsCopied, objectsSkipped, bytesCopied, errors). Errors are
// captured per-object but the loop keeps going so a single failed
// PUT doesn't strand the rest of the tree — same defensive posture
// as the snapshot prune pass.
func (s *Server) copyRestoreTree(
	ctx context.Context,
	srcDrv driver.Driver,
	dstDrv driver.Driver,
	srcBucket, srcPrefix string,
	dstBucket, dstPrefix string,
	overwrite bool,
) (int64, int64, int64, []string) {
	// Normalise the dst prefix so "" and "foo" both produce the
	// right shape after concatenation. The restore writes to
	// {dstBucket}/{dstPrefix?}/{relative} — dstPrefix may end with
	// or without a slash; we strip a trailing slash for predictable
	// joining.
	dstPrefix = strings.TrimSuffix(dstPrefix, "/")

	sem := make(chan struct{}, restoreConcurrency)
	var wg stdsync.WaitGroup
	var mu stdsync.Mutex
	var copied, skipped, bytesCopied int64
	var errs []string

	addErr := func(msg string) {
		mu.Lock()
		errs = appendBounded(errs, msg)
		mu.Unlock()
	}

	var continuation string
	for {
		page, err := srcDrv.ListObjects(ctx, srcBucket, srcPrefix, continuation, "", 1000)
		if err != nil {
			addErr(fmt.Sprintf("list snapshot: %v", err))
			break
		}
		for _, obj := range page.Objects {
			if obj.IsDir {
				continue
			}
			obj := obj
			relative := strings.TrimPrefix(obj.Key, srcPrefix)
			if relative == "" {
				continue
			}
			dstKey := relative
			if dstPrefix != "" {
				dstKey = dstPrefix + "/" + relative
			}

			// Skip-existing pre-check: do this BEFORE acquiring the
			// semaphore so a destination that's already populated
			// burns no copy slots.
			if !overwrite {
				if _, statErr := dstDrv.StatObject(ctx, dstBucket, dstKey); statErr == nil {
					mu.Lock()
					skipped++
					mu.Unlock()
					continue
				}
			}

			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				if err := copyRestoreObject(ctx, srcDrv, dstDrv, srcBucket, obj.Key, dstBucket, dstKey); err != nil {
					addErr(fmt.Sprintf("copy %q: %v", obj.Key, err))
					return
				}
				mu.Lock()
				copied++
				bytesCopied += obj.Size
				mu.Unlock()
			}()
		}
		if !page.IsTruncated || page.NextContinuation == "" {
			break
		}
		continuation = page.NextContinuation
	}
	wg.Wait()

	return copied, skipped, bytesCopied, errs
}

// copyRestoreObject ports the streaming + ServerSideCopy fallback
// from sync.Engine.copyObject so a restore that lands on the same
// driver as the source still skips the round-trip through the
// basement process. Kept as a small standalone helper so the
// restore engine doesn't take a hard dependency on the sync.Engine
// internals — both paths happen to share the same driver primitives.
func copyRestoreObject(ctx context.Context, srcDrv, dstDrv driver.Driver, srcBucket, srcKey, dstBucket, dstKey string) error {
	srcCaps, err := srcDrv.Capabilities(ctx)
	if err != nil {
		return fmt.Errorf("source capabilities: %w", err)
	}
	dstCaps, err := dstDrv.Capabilities(ctx)
	if err != nil {
		return fmt.Errorf("dest capabilities: %w", err)
	}
	if srcCaps.Driver == dstCaps.Driver && dstCaps.ServerSideCopy {
		if err := dstDrv.ServerSideCopy(ctx, srcBucket, srcKey, dstBucket, dstKey); err == nil {
			return nil
		}
		// Fall through to streaming on ServerSideCopy failure — the
		// sync engine does the same.
	}
	stream, err := srcDrv.StreamObject(ctx, srcBucket, srcKey, "")
	if err != nil {
		return fmt.Errorf("stream source: %w", err)
	}
	defer stream.Body.Close()
	if _, err := dstDrv.PutObjectStream(ctx, dstBucket, dstKey, stream.Body, stream.ContentType, stream.ContentLength); err != nil {
		return fmt.Errorf("put dest: %w", err)
	}
	return nil
}
