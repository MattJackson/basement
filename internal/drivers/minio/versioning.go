package minio

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// VersioningSupport reports versioning capability (v1.10.0a). MinIO
// implements the full S3 versioning surface, so the FE's versioning
// toggle is unconditionally available against MinIO-backed buckets
// — same posture as AWS S3.
func (d *driver) VersioningSupport() bool {
	return true
}

// GetVersioningStatus mirrors the aws_s3 implementation — MinIO
// speaks the same S3 versioning API verbatim. An empty Status from
// the backend means "never enabled", surfaced as VersioningDisabled.
func (d *driver) GetVersioningStatus(ctx context.Context, bucket string) (driverpkg.VersioningStatus, error) {
	out, err := d.s3Client.client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return driverpkg.VersioningDisabled, wrapMinioVersioningErr("GetVersioningStatus", err)
	}

	switch out.Status {
	case types.BucketVersioningStatusEnabled:
		return driverpkg.VersioningEnabled, nil
	case types.BucketVersioningStatusSuspended:
		return driverpkg.VersioningSuspended, nil
	default:
		return driverpkg.VersioningDisabled, nil
	}
}

// EnableVersioning flips the bucket to Status=Enabled.
func (d *driver) EnableVersioning(ctx context.Context, bucket string) error {
	return d.putVersioning(ctx, bucket, types.BucketVersioningStatusEnabled)
}

// SuspendVersioning flips the bucket to Status=Suspended. Same
// caveats as aws_s3.SuspendVersioning — existing versions are
// retained, no path back to never-enabled.
func (d *driver) SuspendVersioning(ctx context.Context, bucket string) error {
	return d.putVersioning(ctx, bucket, types.BucketVersioningStatusSuspended)
}

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
		return wrapMinioVersioningErr(op, err)
	}
	return nil
}

// ListObjectVersions mirrors aws_s3 — same S3 API, same single-marker
// continuation shape (keyMarker|versionIdMarker fused into one
// opaque blob the API layer treats as a token).
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
		return nil, "", wrapMinioVersioningErr("ListObjectVersions", err)
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

// GetObjectVersion streams a specific version of a key.
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
		return driverpkg.StreamResult{}, wrapMinioVersioningErr("GetObjectVersion", err)
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

// DeleteObjectVersion permanently removes a single version row.
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
		return wrapMinioVersioningErr("DeleteObjectVersion", err)
	}
	return nil
}

// wrapMinioVersioningErr mirrors aws_s3.wrapAWSVersioningErr — same
// S3 error codes, same driver-sentinel mapping. Duplicated rather
// than shared because each driver wraps with its own driverName.
func wrapMinioVersioningErr(op string, err error) error {
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

// splitVersionMarker / joinVersionMarker — same shape as the aws_s3
// helpers. Kept package-local so each driver owns its own wire
// contract (in case MinIO adds a custom marker grammar later).
func splitVersionMarker(m string) (string, string) {
	for i := 0; i < len(m); i++ {
		if m[i] == '|' {
			return m[:i], m[i+1:]
		}
	}
	return m, ""
}

func joinVersionMarker(k, v string) string {
	return k + "|" + v
}
