package aws_s3

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// VersioningSupport reports whether this driver advertises versioning
// (v1.10.0a). AWS S3 supports the full versioning surface natively;
// the FE's versioning toggle is unconditionally available on
// AWS-backed buckets via this capability flag.
func (d *driver) VersioningSupport() bool {
	return true
}

// GetVersioningStatus maps the S3 GetBucketVersioning response onto
// our three-value enum. S3 returns an empty Status string on buckets
// that have never had versioning enabled — we surface that as
// VersioningDisabled so the FE can render the right toggle state
// (off, with the affordance to turn on) rather than "unknown".
func (d *driver) GetVersioningStatus(ctx context.Context, bucket string) (driverpkg.VersioningStatus, error) {
	out, err := d.s3Client.client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return driverpkg.VersioningDisabled, wrapAWSVersioningErr("GetVersioningStatus", err)
	}

	switch out.Status {
	case types.BucketVersioningStatusEnabled:
		return driverpkg.VersioningEnabled, nil
	case types.BucketVersioningStatusSuspended:
		return driverpkg.VersioningSuspended, nil
	default:
		// Empty string from S3 — bucket has never been versioned.
		return driverpkg.VersioningDisabled, nil
	}
}

// EnableVersioning flips the bucket to Status=Enabled. Idempotent:
// calling on an already-enabled bucket is a no-op on the S3 side.
func (d *driver) EnableVersioning(ctx context.Context, bucket string) error {
	return d.putVersioning(ctx, bucket, types.BucketVersioningStatusEnabled)
}

// SuspendVersioning flips the bucket to Status=Suspended. Existing
// versions are retained but new writes do not create new versions.
// Idempotent on already-suspended buckets.
//
// NOTE: S3 does not support "un-suspend back to never-enabled" —
// once a bucket has been enabled, the only valid states are Enabled
// and Suspended. To stop versioning entirely, callers must suspend
// AND then DeleteObjectVersion every historical version.
func (d *driver) SuspendVersioning(ctx context.Context, bucket string) error {
	return d.putVersioning(ctx, bucket, types.BucketVersioningStatusSuspended)
}

// putVersioning is the shared body for Enable/Suspend. The two are
// distinct interface methods (rather than one Set(status) method) so
// the audit log records the operator's intent verbatim — flipping
// from enabled to suspended is a meaningfully different event from
// re-asserting enabled.
func (d *driver) putVersioning(ctx context.Context, bucket string, status types.BucketVersioningStatus) error {
	op := "EnableVersioning"
	if status == types.BucketVersioningStatusSuspended {
		op = "SuspendVersioning"
	}

	_, err := d.s3Client.client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(bucket),
		VersioningConfiguration: &types.VersioningConfiguration{
			Status: status,
		},
	})
	if err != nil {
		return wrapAWSVersioningErr(op, err)
	}
	return nil
}

// ListObjectVersions wraps S3's ListObjectVersions, fusing the two
// result arrays (Versions + DeleteMarkers) into a single
// chronologically-ordered slice the UI can render as one history
// table. S3 returns them separately because they're different XML
// element types — the driver collapses that detail.
//
// versionIDMarker is the continuation token (NextVersionIdMarker)
// returned by the previous page; empty for the first call. The
// second return value is the new marker — empty when the result is
// not truncated.
//
// NOTE: S3's pagination uses TWO markers (KeyMarker +
// VersionIdMarker) that must be supplied together. We collapse them
// into a single opaque marker on the wire: format
// "{keyMarker}|{versionIdMarker}". Callers treat it as an opaque
// blob; the driver splits it on parse and recomposes on emit. This
// keeps the API surface single-marker (per cycle spec) while
// preserving S3's pagination contract.
func (d *driver) ListObjectVersions(ctx context.Context, bucket, prefix, versionIDMarker string, limit int) ([]driverpkg.ObjectVersion, string, error) {
	input := &s3.ListObjectVersionsInput{
		Bucket: aws.String(bucket),
	}
	if prefix != "" {
		input.Prefix = aws.String(prefix)
	}
	if limit > 0 {
		input.MaxKeys = aws.Int32(int32(limit)) //nolint:gosec // bounded by API caller
	}
	if versionIDMarker != "" {
		k, v := splitVersionMarker(versionIDMarker)
		if k != "" {
			input.KeyMarker = aws.String(k)
		}
		if v != "" {
			input.VersionIdMarker = aws.String(v)
		}
	}

	out, err := d.s3Client.client.ListObjectVersions(ctx, input)
	if err != nil {
		return nil, "", wrapAWSVersioningErr("ListObjectVersions", err)
	}

	versions := make([]driverpkg.ObjectVersion, 0, len(out.Versions)+len(out.DeleteMarkers))
	for _, v := range out.Versions {
		ver := driverpkg.ObjectVersion{
			VersionID:      aws.ToString(v.VersionId),
			Key:            aws.ToString(v.Key),
			Size:           aws.ToInt64(v.Size),
			ETag:           aws.ToString(v.ETag),
			IsLatest:       aws.ToBool(v.IsLatest),
			IsDeleteMarker: false,
		}
		if v.LastModified != nil {
			ver.LastModified = *v.LastModified
		}
		versions = append(versions, ver)
	}
	for _, dm := range out.DeleteMarkers {
		ver := driverpkg.ObjectVersion{
			VersionID:      aws.ToString(dm.VersionId),
			Key:            aws.ToString(dm.Key),
			IsLatest:       aws.ToBool(dm.IsLatest),
			IsDeleteMarker: true,
		}
		if dm.LastModified != nil {
			ver.LastModified = *dm.LastModified
		}
		versions = append(versions, ver)
	}

	next := ""
	if out.IsTruncated != nil && *out.IsTruncated {
		next = joinVersionMarker(aws.ToString(out.NextKeyMarker), aws.ToString(out.NextVersionIdMarker))
	}
	return versions, next, nil
}

// GetObjectVersion streams a specific version of an object. Same
// shape as StreamObject but with VersionId pinned. Returning a
// StreamResult (rather than ObjectInfo + ReadCloser pair) keeps the
// shape symmetric with StreamObject so the API handler can reuse
// the same body-forwarding code.
func (d *driver) GetObjectVersion(ctx context.Context, bucket, key, versionID string) (driverpkg.StreamResult, error) {
	if versionID == "" {
		return driverpkg.StreamResult{}, &driverpkg.Error{
			Op:      "GetObjectVersion",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: "versionID required",
		}
	}

	resp, err := d.s3Client.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket:    aws.String(bucket),
		Key:       aws.String(key),
		VersionId: aws.String(versionID),
	})
	if err != nil {
		return driverpkg.StreamResult{}, wrapAWSVersioningErr("GetObjectVersion", err)
	}

	result := driverpkg.StreamResult{
		Body:          resp.Body,
		ContentType:   aws.ToString(resp.ContentType),
		ContentLength: aws.ToInt64(resp.ContentLength),
		ETag:          aws.ToString(resp.ETag),
	}
	if resp.LastModified != nil {
		result.LastModified = *resp.LastModified
	}
	return result, nil
}

// DeleteObjectVersion permanently removes a single version row. On a
// versioned bucket this is distinct from a plain DeleteObject —
// DeleteObject inserts a delete marker (recorded as a new version
// with IsDeleteMarker=true), whereas DeleteObjectVersion removes a
// specific historical version forever. The UI surfaces this as a
// destructive "delete this version permanently" action distinct
// from the soft "delete the object" path.
func (d *driver) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	if versionID == "" {
		return &driverpkg.Error{
			Op:      "DeleteObjectVersion",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: "versionID required",
		}
	}

	_, err := d.s3Client.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:    aws.String(bucket),
		Key:       aws.String(key),
		VersionId: aws.String(versionID),
	})
	if err != nil {
		return wrapAWSVersioningErr("DeleteObjectVersion", err)
	}
	return nil
}

// wrapAWSVersioningErr maps S3 SDK errors to driver sentinels for
// versioning ops. Mirrors wrapAWSLifecycleErr — same NoSuchBucket /
// AccessDenied mapping rules, plus NoSuchKey / NoSuchVersion for
// the per-version paths.
func wrapAWSVersioningErr(op string, err error) error {
	var apiErr interface{ ErrorCode() string }
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchBucket":
			return &driverpkg.Error{
				Op:      op,
				Driver:  driverName,
				Err:     driverpkg.ErrNotFound,
				Message: "bucket not found",
			}
		case "NoSuchKey", "NoSuchVersion":
			return &driverpkg.Error{
				Op:      op,
				Driver:  driverName,
				Err:     driverpkg.ErrNotFound,
				Message: "object version not found",
			}
		case "AccessDenied", "Forbidden":
			return &driverpkg.Error{
				Op:      op,
				Driver:  driverName,
				Err:     driverpkg.ErrPermissionDenied,
				Message: err.Error(),
			}
		}
	}
	return &driverpkg.Error{
		Op:      op,
		Driver:  driverName,
		Err:     driverpkg.ErrInvalid,
		Message: err.Error(),
	}
}

// splitVersionMarker decomposes the wire-format marker
// "{keyMarker}|{versionIdMarker}" back into S3's two-marker shape.
// First "|" separates the two halves; either side can be empty.
func splitVersionMarker(m string) (string, string) {
	for i := 0; i < len(m); i++ {
		if m[i] == '|' {
			return m[:i], m[i+1:]
		}
	}
	return m, ""
}

// joinVersionMarker composes S3's NextKeyMarker + NextVersionIdMarker
// into the wire-format single-marker shape consumed by
// splitVersionMarker on the next page request.
func joinVersionMarker(k, v string) string {
	return k + "|" + v
}
