package aws_s3

import (
	"context"
	"errors"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// ObjectLockSupport reports whether this driver advertises Object
// Lock (v1.10.0c). AWS S3 supports the full Object Lock surface
// natively; the FE's settings card is unconditionally available on
// AWS-backed buckets via this capability flag.
func (d *driver) ObjectLockSupport() bool {
	return true
}

// GetObjectLockConfig maps the S3 GetObjectLockConfiguration response
// onto our domain shape. S3 returns ObjectLockConfigurationNotFoundError
// on buckets that have never had Object Lock enabled — we normalize
// that to {Enabled: false} so the FE can render the off-state UI
// without an error banner.
func (d *driver) GetObjectLockConfig(ctx context.Context, bucket string) (*driverpkg.ObjectLockConfig, error) {
	out, err := d.s3Client.client.GetObjectLockConfiguration(ctx, &s3.GetObjectLockConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		// "ObjectLockConfigurationNotFoundError" is S3's "never
		// enabled" sentinel — surface as {Enabled: false} rather
		// than an error so the FE doesn't have to special-case it.
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "ObjectLockConfigurationNotFoundError" {
			return &driverpkg.ObjectLockConfig{Enabled: false}, nil
		}
		return nil, wrapAWSObjectLockErr("GetObjectLockConfig", err)
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
		// S3's DefaultRetention exposes Days OR Years, never both;
		// we convert to a wall-clock RetainUntilDate so the FE
		// renders one consistent shape across buckets. Days = 24h,
		// Years = 365d (S3's own convention).
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
// Refuses to flip Enabled from true → false (S3 has no contract for
// turning Object Lock OFF on a bucket that's had it on) — the API
// layer also rejects this shape at the wire boundary, but the driver
// stays defensive in case a direct caller skips the gate.
//
// The DefaultRetention is converted from RetainUntilDate back to Days
// (rounded up) for the S3 wire shape — S3's DefaultRetention is
// expressed in Days/Years offsets, not absolute timestamps.
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
		// Convert wall-clock RetainUntilDate back to Days (S3's
		// wire shape). Round up so a 1.5-day request becomes 2
		// days rather than 1 (operator's "at least 2 days"
		// expectation, not S3's truncation).
		days := int32(0)
		if !cfg.DefaultRetention.RetainUntilDate.IsZero() {
			delta := time.Until(cfg.DefaultRetention.RetainUntilDate)
			daysF := delta.Hours() / 24.0
			// Round up — operator wants AT LEAST N days.
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
		return wrapAWSObjectLockErr("PutObjectLockConfig", err)
	}
	return nil
}

// GetObjectRetention reads the per-version retention. Empty mode +
// zero time is a valid response (the object has no retention).
// versionID is required because the per-object Object Lock surface
// always operates on a specific version row.
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
		// "NoSuchObjectLockConfiguration" means this specific
		// object has no retention set — return nil + nil rather
		// than an error so the FE renders "no retention" cleanly.
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchObjectLockConfiguration" {
			return nil, nil
		}
		return nil, wrapAWSObjectLockErr("GetObjectRetention", err)
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

// PutObjectRetention writes the per-version retention. bypassGovernance
// is honoured only when the EXISTING retention is GOVERNANCE — S3
// rejects the bypass flag on COMPLIANCE retentions regardless.
//
// NOTE: this driver method does NOT pre-fetch the existing retention
// to decide whether to send the bypass header — that would race the
// PUT under concurrent updates. We forward the caller's intent and
// let S3 do the authoritative check; the API layer's audit captures
// the bypass flag so a compliance auditor can see "operator tried to
// bypass COMPLIANCE and got 403".
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
		return wrapAWSObjectLockErr("PutObjectRetention", err)
	}
	return nil
}

// GetObjectLegalHold reads the per-version legal-hold flag. A bucket
// with Object Lock enabled but no per-object legal hold set returns
// the OFF state (false) cleanly.
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
		// "NoSuchObjectLockConfiguration" = no legal hold ever set
		// on this object. Return false rather than an error.
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchObjectLockConfiguration" {
			return false, nil
		}
		return false, wrapAWSObjectLockErr("GetObjectLegalHold", err)
	}
	if out.LegalHold == nil {
		return false, nil
	}
	return out.LegalHold.Status == types.ObjectLockLegalHoldStatusOn, nil
}

// PutObjectLegalHold toggles the per-version legal-hold flag.
// Independent of retention — a legal hold can be set or cleared
// regardless of any active retention, and persists past the
// retain-until date.
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
		return wrapAWSObjectLockErr("PutObjectLegalHold", err)
	}
	return nil
}

// wrapAWSObjectLockErr maps S3 SDK Object Lock errors to driver
// sentinels. Mirrors wrapAWSVersioningErr — same NoSuchBucket /
// AccessDenied mapping, plus the InvalidRetentionPeriod /
// InvalidWriteOffset / ObjectLockConfigurationNotFoundError shapes
// the Object Lock surface adds on top.
func wrapAWSObjectLockErr(op string, err error) error {
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
			// S3 returns InvalidRequest for compliance-mode
			// "you can't reduce this retention" rejections. Surface
			// as ErrConflict so the API layer maps to 409, which
			// telegraphs "the state is the obstacle, not the
			// request shape".
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
