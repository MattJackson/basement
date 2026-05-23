package minio

import (
	"context"
	"errors"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// ObjectLockSupport reports Object Lock capability (v1.10.0c). MinIO
// implements the full S3 Object Lock surface, so the FE's settings
// card is unconditionally available against MinIO-backed buckets —
// same posture as AWS S3.
func (d *driver) ObjectLockSupport() bool {
	return true
}

// GetObjectLockConfig mirrors the aws_s3 implementation — MinIO
// speaks the same S3 Object Lock API verbatim. A
// "ObjectLockConfigurationNotFoundError" from the backend means
// "never enabled", surfaced as {Enabled: false}.
func (d *driver) GetObjectLockConfig(ctx context.Context, bucket string) (*driverpkg.ObjectLockConfig, error) {
	out, err := d.s3Client.client.GetObjectLockConfiguration(ctx, &s3.GetObjectLockConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "ObjectLockConfigurationNotFoundError" {
			return &driverpkg.ObjectLockConfig{Enabled: false}, nil
		}
		return nil, wrapMinioObjectLockErr("GetObjectLockConfig", err)
	}

	cfg := &driverpkg.ObjectLockConfig{}
	if out.ObjectLockConfiguration != nil &&
		out.ObjectLockConfiguration.ObjectLockEnabled == types.ObjectLockEnabledEnabled {
		cfg.Enabled = true
	}
	if out.ObjectLockConfiguration != nil &&
		out.ObjectLockConfiguration.Rule != nil &&
		out.ObjectLockConfiguration.Rule.DefaultRetention != nil {
		dr := out.ObjectLockConfiguration.Rule.DefaultRetention
		ret := &driverpkg.ObjectLockRetention{
			Mode: driverpkg.ObjectLockMode(dr.Mode),
		}
		if dr.Days != nil && *dr.Days > 0 {
			ret.RetainUntilDate = time.Now().UTC().Add(time.Duration(*dr.Days) * 24 * time.Hour)
		} else if dr.Years != nil && *dr.Years > 0 {
			ret.RetainUntilDate = time.Now().UTC().Add(time.Duration(*dr.Years) * 365 * 24 * time.Hour)
		}
		cfg.DefaultRetention = ret
	}
	return cfg, nil
}

// PutObjectLockConfig writes the bucket-level Object Lock config.
// Same one-way-only constraint as the aws_s3 implementation —
// flipping Enabled from true → false is rejected up front.
func (d *driver) PutObjectLockConfig(ctx context.Context, bucket string, cfg driverpkg.ObjectLockConfig) error {
	if !cfg.Enabled {
		return &driverpkg.Error{
			Op:      "PutObjectLockConfig",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: "Object Lock cannot be disabled on a bucket once enabled (S3 contract)",
		}
	}

	input := &s3.PutObjectLockConfigurationInput{
		Bucket: aws.String(bucket),
		ObjectLockConfiguration: &types.ObjectLockConfiguration{
			ObjectLockEnabled: types.ObjectLockEnabledEnabled,
		},
	}

	if cfg.DefaultRetention != nil {
		if cfg.DefaultRetention.Mode != driverpkg.ObjectLockGovernance &&
			cfg.DefaultRetention.Mode != driverpkg.ObjectLockCompliance {
			return &driverpkg.Error{
				Op:      "PutObjectLockConfig",
				Driver:  driverName,
				Err:     driverpkg.ErrInvalid,
				Message: "default retention mode must be GOVERNANCE or COMPLIANCE",
			}
		}
		days := int32(0)
		if !cfg.DefaultRetention.RetainUntilDate.IsZero() {
			delta := time.Until(cfg.DefaultRetention.RetainUntilDate)
			daysF := delta.Hours() / 24.0
			if daysF > 0 {
				days = int32(daysF + 0.999)
			}
		}
		if days < 1 {
			days = 1
		}
		input.ObjectLockConfiguration.Rule = &types.ObjectLockRule{
			DefaultRetention: &types.DefaultRetention{
				Mode: types.ObjectLockRetentionMode(cfg.DefaultRetention.Mode),
				Days: aws.Int32(days),
			},
		}
	}

	_, err := d.s3Client.client.PutObjectLockConfiguration(ctx, input)
	if err != nil {
		return wrapMinioObjectLockErr("PutObjectLockConfig", err)
	}
	return nil
}

// GetObjectRetention reads the per-version retention.
func (d *driver) GetObjectRetention(ctx context.Context, bucket, key, versionID string) (*driverpkg.ObjectLockRetention, error) {
	if versionID == "" {
		return nil, &driverpkg.Error{
			Op:      "GetObjectRetention",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: "versionID required",
		}
	}
	out, err := d.s3Client.client.GetObjectRetention(ctx, &s3.GetObjectRetentionInput{
		Bucket:    aws.String(bucket),
		Key:       aws.String(key),
		VersionId: aws.String(versionID),
	})
	if err != nil {
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchObjectLockConfiguration" {
			return nil, nil
		}
		return nil, wrapMinioObjectLockErr("GetObjectRetention", err)
	}
	if out.Retention == nil {
		return nil, nil
	}
	ret := &driverpkg.ObjectLockRetention{
		Mode: driverpkg.ObjectLockMode(out.Retention.Mode),
	}
	if out.Retention.RetainUntilDate != nil {
		ret.RetainUntilDate = *out.Retention.RetainUntilDate
	}
	return ret, nil
}

// PutObjectRetention writes the per-version retention. Same
// bypassGovernance forwarding semantics as the aws_s3 implementation
// — we don't pre-fetch existing state; S3 is the authoritative gate.
func (d *driver) PutObjectRetention(ctx context.Context, bucket, key, versionID string, retention driverpkg.ObjectLockRetention, bypassGovernance bool) error {
	if versionID == "" {
		return &driverpkg.Error{
			Op:      "PutObjectRetention",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: "versionID required",
		}
	}
	if retention.Mode != driverpkg.ObjectLockGovernance &&
		retention.Mode != driverpkg.ObjectLockCompliance {
		return &driverpkg.Error{
			Op:      "PutObjectRetention",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: "retention mode must be GOVERNANCE or COMPLIANCE",
		}
	}
	if retention.RetainUntilDate.IsZero() {
		return &driverpkg.Error{
			Op:      "PutObjectRetention",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: "retainUntilDate required",
		}
	}

	input := &s3.PutObjectRetentionInput{
		Bucket:    aws.String(bucket),
		Key:       aws.String(key),
		VersionId: aws.String(versionID),
		Retention: &types.ObjectLockRetention{
			Mode:            types.ObjectLockRetentionMode(retention.Mode),
			RetainUntilDate: aws.Time(retention.RetainUntilDate),
		},
	}
	if bypassGovernance {
		input.BypassGovernanceRetention = aws.Bool(true)
	}

	_, err := d.s3Client.client.PutObjectRetention(ctx, input)
	if err != nil {
		return wrapMinioObjectLockErr("PutObjectRetention", err)
	}
	return nil
}

// GetObjectLegalHold reads the per-version legal-hold flag.
func (d *driver) GetObjectLegalHold(ctx context.Context, bucket, key, versionID string) (bool, error) {
	if versionID == "" {
		return false, &driverpkg.Error{
			Op:      "GetObjectLegalHold",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: "versionID required",
		}
	}
	out, err := d.s3Client.client.GetObjectLegalHold(ctx, &s3.GetObjectLegalHoldInput{
		Bucket:    aws.String(bucket),
		Key:       aws.String(key),
		VersionId: aws.String(versionID),
	})
	if err != nil {
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchObjectLockConfiguration" {
			return false, nil
		}
		return false, wrapMinioObjectLockErr("GetObjectLegalHold", err)
	}
	if out.LegalHold == nil {
		return false, nil
	}
	return out.LegalHold.Status == types.ObjectLockLegalHoldStatusOn, nil
}

// PutObjectLegalHold toggles the per-version legal-hold flag.
func (d *driver) PutObjectLegalHold(ctx context.Context, bucket, key, versionID string, on bool) error {
	if versionID == "" {
		return &driverpkg.Error{
			Op:      "PutObjectLegalHold",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: "versionID required",
		}
	}
	status := types.ObjectLockLegalHoldStatusOff
	if on {
		status = types.ObjectLockLegalHoldStatusOn
	}
	_, err := d.s3Client.client.PutObjectLegalHold(ctx, &s3.PutObjectLegalHoldInput{
		Bucket:    aws.String(bucket),
		Key:       aws.String(key),
		VersionId: aws.String(versionID),
		LegalHold: &types.ObjectLockLegalHold{Status: status},
	})
	if err != nil {
		return wrapMinioObjectLockErr("PutObjectLegalHold", err)
	}
	return nil
}

// wrapMinioObjectLockErr mirrors wrapAWSObjectLockErr — same S3 error
// codes, same driver-sentinel mapping. Duplicated rather than shared
// because each driver wraps with its own driverName.
func wrapMinioObjectLockErr(op string, err error) error {
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
		case "InvalidRetentionPeriod", "InvalidArgument":
			return &driverpkg.Error{
				Op:      op,
				Driver:  driverName,
				Err:     driverpkg.ErrInvalid,
				Message: err.Error(),
			}
		case "InvalidRequest":
			return &driverpkg.Error{
				Op:      op,
				Driver:  driverName,
				Err:     driverpkg.ErrConflict,
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
