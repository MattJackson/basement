// Package webdav: os.FileInfo adapters that map basement primitives
// (regions, buckets, S3 objects) onto the os.FileInfo interface the
// golang.org/x/net/webdav package consumes for PROPFIND responses.
//
// All three adapters share a thin struct because os.FileInfo is six
// methods of which only Name + Size + ModTime + IsDir matter for
// WebDAV; Mode + Sys are constant per kind.

package webdav

import (
	"context"
	"mime"
	"os"
	"path/filepath"
	"time"

	wdav "golang.org/x/net/webdav"
)

// nodeInfo is the single os.FileInfo implementation used for every
// basement node webdav can surface: regions, buckets, folder prefixes,
// and S3 objects. The kind drives Mode() (dir vs regular file).
//
// nodeInfo implements webdav.ContentTyper + webdav.ETager so the
// upstream findContentType / findETag never re-open the file to sniff
// bytes — which would force an extra S3 round-trip per PROPFIND child
// and trip my readFile's "Seek back after Read" limitation.
type nodeInfo struct {
	name    string
	size    int64
	modTime time.Time
	isDir   bool

	// etag rides along when we have it (e.g. from a StatObject), so
	// the ETag prop reflects the backend's real fingerprint instead
	// of the default size+modTime hash. Empty falls back to the
	// upstream default.
	etag string
	// contentType is the MIME type, when known. Empty falls back to
	// extension-based detection (no file read).
	contentType string
}

func (n *nodeInfo) Name() string       { return n.name }
func (n *nodeInfo) Size() int64        { return n.size }
func (n *nodeInfo) ModTime() time.Time { return n.modTime }
func (n *nodeInfo) IsDir() bool        { return n.isDir }
func (n *nodeInfo) Sys() any           { return nil }

// Mode returns 0755 for directories, 0644 for files. WebDAV does not
// rely on the unix permission bits beyond the dir flag, but Finder
// and Explorer both inspect them when rendering — sane defaults keep
// them out of "read-only filesystem" warnings.
func (n *nodeInfo) Mode() os.FileMode {
	if n.isDir {
		return os.ModeDir | 0o755
	}
	return 0o644
}

// newDirInfo builds a directory-flavoured nodeInfo. modTime is the
// zero time for synthetic entries (regions list, bucket list) because
// neither has a meaningful modification timestamp; the WebDAV spec
// allows zero modTime in that case.
func newDirInfo(name string, modTime time.Time) *nodeInfo {
	return &nodeInfo{name: name, isDir: true, modTime: modTime}
}

// newFileInfo builds a regular-file nodeInfo for an S3 object.
func newFileInfo(name string, size int64, modTime time.Time) *nodeInfo {
	return &nodeInfo{name: name, size: size, modTime: modTime}
}

// ContentType implements webdav.ContentTyper. When the backend gave
// us a content type, return it verbatim. Otherwise sniff by file
// extension via mime.TypeByExtension — no bytes read. Returns
// webdav.ErrNotImplemented when neither path yields a type, letting
// the upstream code fall back to its byte-sniff path (which our
// readFile permits via a fresh stream on each OpenFile).
func (n *nodeInfo) ContentType(_ context.Context) (string, error) {
	if n.contentType != "" {
		return n.contentType, nil
	}
	if ct := mime.TypeByExtension(filepath.Ext(n.name)); ct != "" {
		return ct, nil
	}
	return "", wdav.ErrNotImplemented
}

// ETag implements webdav.ETager. Returns the backend ETag when set;
// otherwise reports not-implemented so the upstream default (size +
// modTime hash) lands.
func (n *nodeInfo) ETag(_ context.Context) (string, error) {
	if n.etag == "" {
		return "", wdav.ErrNotImplemented
	}
	return `"` + n.etag + `"`, nil
}
