// Package webdav: golang.org/x/net/webdav.FileSystem implementation
// that maps WebDAV paths onto basement's storage surface via
// gateway.Backend (v1.9.0c refactor).
//
// Path namespace under the /webdav/ prefix:
//
//   /              → user root; PROPFIND lists the user's region aliases
//   /{alias}       → a region; PROPFIND lists buckets reachable via the
//                    region (with the Garage admin bridge applied in
//                    the Backend impl when relevant)
//   /{alias}/{bk}  → a bucket; PROPFIND lists immediate children
//                    (objects + sub-folders) via Backend.ListObjects
//                    with delimiter="/"
//   /{alias}/{bk}/{key} → an S3 object; GET / PUT / DELETE / HEAD apply
//
// The FileSystem is per-request: a fresh one is built inside the
// Gateway for each verb so authentication + the per-user lookups
// happen once and stay scoped to the request lifetime. There is no
// shared mutable state, so concurrent verbs from the same client are
// safe.

package webdav

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	wdav "golang.org/x/net/webdav"

	"github.com/mattjackson/basement/internal/gateway"
)

// fs implements golang.org/x/net/webdav.FileSystem on top of
// gateway.Backend. Per-request; never reused across requests.
type fs struct {
	ctx     context.Context
	uctx    *gateway.UserContext
	backend gateway.Backend

	// cached per-call lookups — webdav verbs often call Stat then
	// OpenFile back-to-back; caching shaves a region-resolve and an
	// S3 round-trip off the second call.
	cachedRegions []gateway.Region
}

func newFS(ctx context.Context, uctx *gateway.UserContext, backend gateway.Backend) *fs {
	return &fs{
		ctx:     ctx,
		uctx:    uctx,
		backend: backend,
	}
}

// resolved is the parsed shape of a WebDAV path. Exactly one of the
// boolean flags is true (or all three are false meaning "root").
type resolved struct {
	isRoot   bool
	isRegion bool
	isBucket bool
	isObject bool

	region    gateway.Region
	bucket    string
	objectKey string // empty for "bucket root", trailing "/" for folder
}

// parsePath splits a webdav-relative path (already stripped of the
// /webdav/ prefix by Gateway.Prefix) into one of the four kinds
// above. Leading and trailing slashes are normalised away; an empty
// path or "/" maps to root.
func (f *fs) parsePath(name string) (resolved, error) {
	clean := strings.Trim(path.Clean("/"+name), "/")
	if clean == "" || clean == "." {
		return resolved{isRoot: true}, nil
	}

	parts := strings.SplitN(clean, "/", 3)
	alias := parts[0]
	region, err := f.resolveRegion(alias)
	if err != nil {
		return resolved{}, err
	}

	if len(parts) == 1 {
		return resolved{isRegion: true, region: region}, nil
	}
	bucket := parts[1]
	if len(parts) == 2 {
		return resolved{isBucket: true, region: region, bucket: bucket}, nil
	}
	// Preserve the trailing slash so callers can distinguish "folder
	// marker" (zero-byte object with /) from a regular file. WebDAV
	// MKCOL on a non-root path lands here with isObject + trailing /.
	key := parts[2]
	if strings.HasSuffix(name, "/") && !strings.HasSuffix(key, "/") {
		key += "/"
	}
	return resolved{isObject: true, region: region, bucket: bucket, objectKey: key}, nil
}

// resolveRegion finds the Region whose Alias matches `alias` for
// the current user. v1.2.0d allows multiple regions with the same
// alias if they point at different endpoints; we take the first
// match (mirrors the precedence the rest of the user-tier code follows).
func (f *fs) resolveRegion(alias string) (gateway.Region, error) {
	regions, err := f.listRegions()
	if err != nil {
		return gateway.Region{}, err
	}
	for _, r := range regions {
		if r.Alias == alias {
			return r, nil
		}
	}
	return gateway.Region{}, os.ErrNotExist
}

func (f *fs) listRegions() ([]gateway.Region, error) {
	if f.cachedRegions != nil {
		return f.cachedRegions, nil
	}
	rs, err := f.backend.ListRegions(f.ctx, f.uctx)
	if err != nil {
		return nil, fmt.Errorf("list regions: %w", err)
	}
	f.cachedRegions = rs
	return rs, nil
}

// Stat implements webdav.FileSystem. PROPFIND on a path calls Stat
// first and then (for directories at Depth > 0) Readdir on the
// returned File.
func (f *fs) Stat(_ context.Context, name string) (os.FileInfo, error) {
	res, err := f.parsePath(name)
	if err != nil {
		return nil, err
	}
	switch {
	case res.isRoot:
		return newDirInfo("/", time.Time{}), nil
	case res.isRegion:
		return newDirInfo(res.region.Alias, res.region.CreatedAt), nil
	case res.isBucket:
		return newDirInfo(res.bucket, time.Time{}), nil
	case res.isObject:
		// Folder marker: trailing slash. Don't HEAD — synthesise
		// a directory entry. macOS Finder probes "{folder}/" with Stat
		// before drilling in; HEAD-ing a non-existent key would 404.
		if strings.HasSuffix(res.objectKey, "/") {
			return newDirInfo(path.Base(strings.TrimSuffix(res.objectKey, "/")), time.Time{}), nil
		}
		oi, err := f.backend.HeadObject(f.ctx, f.uctx, res.region.ID, res.bucket, res.objectKey)
		if err != nil {
			if errors.Is(err, gateway.ErrNotFound) {
				if f.isProbablyFolder(res.region.ID, res.bucket, res.objectKey) {
					return newDirInfo(path.Base(res.objectKey), time.Time{}), nil
				}
				return nil, os.ErrNotExist
			}
			if f.isProbablyFolder(res.region.ID, res.bucket, res.objectKey) {
				return newDirInfo(path.Base(res.objectKey), time.Time{}), nil
			}
			return nil, err
		}
		info := newFileInfo(path.Base(res.objectKey), oi.Size, oi.LastModified)
		info.etag = strings.TrimSpace(oi.ETag)
		info.contentType = strings.TrimSpace(oi.ContentType)
		return info, nil
	}
	return nil, os.ErrNotExist
}

// OpenFile is the workhorse: returns a webdav.File backed by the
// resolved node. For directories we always return a directoryFile
// that lazy-loads its children on Readdir. For objects, the flag
// determines read-vs-write — webdav passes os.O_RDONLY for GET, and
// os.O_WRONLY|os.O_CREATE|os.O_TRUNC for PUT.
func (f *fs) OpenFile(_ context.Context, name string, flag int, _ os.FileMode) (wdav.File, error) {
	res, err := f.parsePath(name)
	if err != nil {
		return nil, err
	}

	wantWrite := flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) != 0
	if wantWrite {
		if !res.isObject {
			return nil, os.ErrPermission
		}
		if strings.HasSuffix(res.objectKey, "/") {
			return nil, os.ErrPermission
		}
		return newWriteFile(f.ctx, f.backend, f.uctx, res.region.ID, res.bucket, res.objectKey), nil
	}

	switch {
	case res.isRoot:
		regions, err := f.listRegions()
		if err != nil {
			return nil, err
		}
		children := make([]os.FileInfo, 0, len(regions))
		for _, r := range regions {
			children = append(children, newDirInfo(r.Alias, r.CreatedAt))
		}
		return newDirFile("/", children), nil
	case res.isRegion:
		buckets, err := f.backend.ListBuckets(f.ctx, f.uctx, res.region.ID)
		if err != nil {
			return nil, err
		}
		children := make([]os.FileInfo, 0, len(buckets))
		for _, b := range buckets {
			label := b.ID
			if len(b.Aliases) > 0 {
				label = b.Aliases[0]
			}
			children = append(children, newDirInfo(label, b.Created))
		}
		return newDirFile(res.region.Alias, children), nil
	case res.isBucket:
		return f.openListing(res, "")
	case res.isObject:
		if strings.HasSuffix(res.objectKey, "/") {
			return f.openListing(res, res.objectKey)
		}
		stream, meta, err := f.backend.GetObject(f.ctx, f.uctx, res.region.ID, res.bucket, res.objectKey)
		if err == nil {
			return newReadFile(res.objectKey, stream, meta.Size), nil
		}
		if errors.Is(err, gateway.ErrNotFound) {
			if f.isProbablyFolder(res.region.ID, res.bucket, res.objectKey) {
				prefix := res.objectKey
				if !strings.HasSuffix(prefix, "/") {
					prefix += "/"
				}
				return f.openListing(res, prefix)
			}
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	return nil, os.ErrNotExist
}

// openListing performs a Backend.ListObjects (delimiter="/") under
// prefix and returns a directoryFile whose Readdir surfaces the
// response. The prefix is the FULL S3 prefix (already includes any
// leading folder path).
func (f *fs) openListing(res resolved, prefix string) (wdav.File, error) {
	page, err := f.backend.ListObjects(f.ctx, f.uctx, res.region.ID, res.bucket, prefix, "/", "", 1000)
	if err != nil {
		return nil, err
	}
	children := make([]os.FileInfo, 0, len(page.Objects)+len(page.CommonPrefixes))
	for _, p := range page.CommonPrefixes {
		label := strings.TrimSuffix(strings.TrimPrefix(p, prefix), "/")
		if label == "" {
			continue
		}
		children = append(children, newDirInfo(label, time.Time{}))
	}
	for _, obj := range page.Objects {
		// Skip the folder-marker object that some clients (us, on
		// MKCOL) drop to mark a sub-directory. Naming convention:
		// the object key equals the prefix itself with a trailing /.
		if obj.Key == prefix && strings.HasSuffix(obj.Key, "/") {
			continue
		}
		label := strings.TrimPrefix(obj.Key, prefix)
		if label == "" {
			continue
		}
		children = append(children, newFileInfo(label, obj.Size, obj.LastModified))
	}
	name := res.bucket
	if prefix != "" {
		name = path.Base(strings.TrimSuffix(prefix, "/"))
	}
	return newDirFile(name, children), nil
}

// Mkdir implements webdav.FileSystem. Two modes:
//
//   - /{alias}/{newbucket} → Backend.CreateBucket on the region.
//   - /{alias}/{bucket}/path/to/folder → drop a zero-byte object
//     keyed at "path/to/folder/" as the folder marker.
//
// At /{alias} or shallower, Mkdir is refused (operator can't create
// regions via WebDAV — that's a /api/v1 op).
func (f *fs) Mkdir(_ context.Context, name string, _ os.FileMode) error {
	res, err := f.parsePath(name)
	if err != nil {
		return err
	}
	switch {
	case res.isRoot, res.isRegion:
		return os.ErrPermission
	case res.isBucket:
		return f.backend.CreateBucket(f.ctx, f.uctx, res.region.ID, res.bucket)
	case res.isObject:
		key := res.objectKey
		if !strings.HasSuffix(key, "/") {
			key += "/"
		}
		return f.backend.PutObject(f.ctx, f.uctx, res.region.ID, res.bucket, key, strings.NewReader(""), 0, "application/x-directory")
	}
	return os.ErrInvalid
}

// RemoveAll implements webdav.FileSystem. Objects are DeleteObject;
// folder prefixes recursively delete every key under prefix; buckets
// DeleteBucket (which usually requires empty on the backend).
func (f *fs) RemoveAll(_ context.Context, name string) error {
	res, err := f.parsePath(name)
	if err != nil {
		return err
	}
	switch {
	case res.isRoot, res.isRegion:
		return os.ErrPermission
	case res.isBucket:
		return f.backend.DeleteBucket(f.ctx, f.uctx, res.region.ID, res.bucket)
	case res.isObject:
		if strings.HasSuffix(res.objectKey, "/") {
			return f.deletePrefix(res.region.ID, res.bucket, res.objectKey)
		}
		// Try delete-as-file first; on 404 fall back to delete-as-
		// prefix so a Finder request that elides the trailing slash
		// still works.
		if err := f.backend.DeleteObject(f.ctx, f.uctx, res.region.ID, res.bucket, res.objectKey); err == nil {
			return nil
		} else if !errors.Is(err, gateway.ErrNotFound) {
			return err
		}
		return f.deletePrefix(res.region.ID, res.bucket, res.objectKey+"/")
	}
	return os.ErrInvalid
}

// deletePrefix recursively deletes every key under prefix using
// Backend.ListObjects + Backend.DeleteObject.
func (f *fs) deletePrefix(regionID, bucket, prefix string) error {
	token := ""
	for {
		page, err := f.backend.ListObjects(f.ctx, f.uctx, regionID, bucket, prefix, "", token, 1000)
		if err != nil {
			return err
		}
		for _, obj := range page.Objects {
			if err := f.backend.DeleteObject(f.ctx, f.uctx, regionID, bucket, obj.Key); err != nil && !errors.Is(err, gateway.ErrNotFound) {
				return err
			}
		}
		if !page.IsTruncated || page.NextContinuation == "" {
			return nil
		}
		token = page.NextContinuation
	}
}

// Rename implements webdav.FileSystem via Backend.CopyObject +
// DeleteObject. Cross-bucket renames within the same region work;
// cross-region renames are refused (would require a download/upload
// bridge the cycle excludes). Folder renames refused for the same
// reason.
func (f *fs) Rename(_ context.Context, oldName, newName string) error {
	src, err := f.parsePath(oldName)
	if err != nil {
		return err
	}
	dst, err := f.parsePath(newName)
	if err != nil {
		return err
	}
	if !src.isObject || !dst.isObject {
		return os.ErrPermission
	}
	if src.region.ID != dst.region.ID {
		return os.ErrPermission
	}
	if strings.HasSuffix(src.objectKey, "/") || strings.HasSuffix(dst.objectKey, "/") {
		return os.ErrPermission
	}
	if err := f.backend.CopyObject(f.ctx, f.uctx, src.region.ID, src.bucket, src.objectKey, dst.region.ID, dst.bucket, dst.objectKey); err != nil {
		return err
	}
	return f.backend.DeleteObject(f.ctx, f.uctx, src.region.ID, src.bucket, src.objectKey)
}

// isProbablyFolder returns true if the key has any children under it
// when listed as a prefix. Used as a fallback when HeadObject 404s
// but the path may still be a directory marker.
func (f *fs) isProbablyFolder(regionID, bucket, key string) bool {
	prefix := key
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	page, err := f.backend.ListObjects(f.ctx, f.uctx, regionID, bucket, prefix, "/", "", 1)
	if err != nil {
		return false
	}
	return len(page.Objects) > 0 || len(page.CommonPrefixes) > 0
}

// --- file types ---------------------------------------------------

// readFile is a webdav.File backed by a Backend.GetObject stream.
type readFile struct {
	name   string
	body   io.ReadCloser
	size   int64
	offset int64
}

func newReadFile(name string, body io.ReadCloser, size int64) *readFile {
	return &readFile{name: name, body: body, size: size}
}

func (rf *readFile) Read(p []byte) (int, error) {
	n, err := rf.body.Read(p)
	rf.offset += int64(n)
	return n, err
}

func (rf *readFile) Write(p []byte) (int, error) {
	return 0, os.ErrPermission
}

func (rf *readFile) Close() error {
	if rf.body == nil {
		return nil
	}
	return rf.body.Close()
}

// Seek supports the (0, SeekStart) and (0, SeekEnd) cases WebDAV
// clients commonly use to probe size; anything else returns an
// error rather than silently misreading the stream.
func (rf *readFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		if offset == rf.offset {
			return rf.offset, nil
		}
		if offset == 0 {
			return rf.offset, os.ErrInvalid
		}
		return rf.offset, os.ErrInvalid
	case io.SeekCurrent:
		if offset == 0 {
			return rf.offset, nil
		}
		return rf.offset, os.ErrInvalid
	case io.SeekEnd:
		if offset == 0 {
			return rf.size, nil
		}
		return rf.offset, os.ErrInvalid
	}
	return rf.offset, os.ErrInvalid
}

func (rf *readFile) Readdir(count int) ([]os.FileInfo, error) {
	return nil, os.ErrInvalid
}

func (rf *readFile) Stat() (os.FileInfo, error) {
	return newFileInfo(rf.name, rf.size, time.Time{}), nil
}

// writeFile buffers PUT body bytes in memory then Backend.PutObjects
// them on Close.
type writeFile struct {
	ctx      context.Context
	backend  gateway.Backend
	uctx     *gateway.UserContext
	regionID string
	bucket   string
	key      string
	buf      []byte
	closed   bool
}

func newWriteFile(ctx context.Context, backend gateway.Backend, uctx *gateway.UserContext, regionID, bucket, key string) *writeFile {
	return &writeFile{
		ctx:      ctx,
		backend:  backend,
		uctx:     uctx,
		regionID: regionID,
		bucket:   bucket,
		key:      key,
	}
}

func (wf *writeFile) Write(p []byte) (int, error) {
	if wf.closed {
		return 0, os.ErrClosed
	}
	wf.buf = append(wf.buf, p...)
	return len(p), nil
}

func (wf *writeFile) Read(p []byte) (int, error) { return 0, os.ErrPermission }
func (wf *writeFile) Seek(int64, int) (int64, error) {
	if !wf.closed && len(wf.buf) == 0 {
		return 0, nil
	}
	return 0, os.ErrInvalid
}

func (wf *writeFile) Close() error {
	if wf.closed {
		return nil
	}
	wf.closed = true
	reader := strings.NewReader(string(wf.buf))
	err := wf.backend.PutObject(wf.ctx, wf.uctx, wf.regionID, wf.bucket, wf.key, reader, int64(len(wf.buf)), "application/octet-stream")
	wf.buf = nil
	return err
}

func (wf *writeFile) Readdir(count int) ([]os.FileInfo, error) {
	return nil, os.ErrInvalid
}

func (wf *writeFile) Stat() (os.FileInfo, error) {
	return newFileInfo(path.Base(wf.key), int64(len(wf.buf)), time.Time{}), nil
}

// dirFile implements wdav.File over an in-memory child list. PROPFIND
// at depth=1 walks this via Readdir; the slice is populated up-front
// in OpenFile so a flaky backend at read-time doesn't surface a half-
// rendered listing.
type dirFile struct {
	name     string
	children []os.FileInfo
	offset   int
}

func newDirFile(name string, children []os.FileInfo) *dirFile {
	return &dirFile{name: name, children: children}
}

func (d *dirFile) Close() error                    { return nil }
func (d *dirFile) Read([]byte) (int, error)        { return 0, os.ErrInvalid }
func (d *dirFile) Write([]byte) (int, error)       { return 0, os.ErrPermission }
func (d *dirFile) Seek(int64, int) (int64, error)  { return 0, os.ErrInvalid }
func (d *dirFile) Stat() (os.FileInfo, error) {
	return newDirInfo(d.name, time.Time{}), nil
}

// Readdir returns up to count entries, advancing the internal cursor
// per the os.File contract. count<=0 returns the remainder.
func (d *dirFile) Readdir(count int) ([]os.FileInfo, error) {
	if d.offset >= len(d.children) {
		if count <= 0 {
			return nil, nil
		}
		return nil, io.EOF
	}
	if count <= 0 {
		rest := d.children[d.offset:]
		d.offset = len(d.children)
		return rest, nil
	}
	end := d.offset + count
	if end > len(d.children) {
		end = len(d.children)
	}
	out := d.children[d.offset:end]
	d.offset = end
	return out, nil
}
