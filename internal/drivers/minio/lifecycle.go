package minio

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// minioLifecycleTiers reports the storage classes MinIO recognises in
// lifecycle transitions. Real-world MinIO clusters can target arbitrary
// remote tiers configured via `mc ilm tier add` (e.g. user-defined
// "WARM-TIER" pointing at a Glacier endpoint), but the S3 API itself
// accepts the AWS-standard tier names + any custom strings the
// administrator has registered. For the v0.9.0i wizard we surface the
// AWS-standard set; advanced operators with custom tiers can still
// pass them through the API since the driver doesn't validate the
// tier string client-side.
var minioLifecycleTiers = []string{
	string(types.TransitionStorageClassStandardIa),
	string(types.TransitionStorageClassGlacier),
	string(types.TransitionStorageClassDeepArchive),
}

// PerBucketStatsAvailable reports whether ListBuckets / GetBucket on
// the user-region tier surfaces Objects + Bytes. v1.4.0a: MinIO
// returns true — its admin metrics surface (Prometheus + the
// /minio/v2/metrics/bucket endpoint) carries the counters and the
// driver wraps them into bucket flows when the basement caller
// requests stats. FE keeps Size + Objects columns visible against
// MinIO-backed regions.
func (d *driver) PerBucketStatsAvailable() bool {
	return true
}

// LifecycleSupport reports lifecycle capabilities for MinIO. Mirrors
// the aws_s3 driver — same SDK, same API surface; the tier slice
// could legitimately differ on a per-cluster basis but the wizard's
// canonical-AWS-tiers set covers the operator-facing 80% case.
func (d *driver) LifecycleSupport() driverpkg.LifecycleCapabilities {
	return driverpkg.LifecycleCapabilities{
		Supported:          true,
		Expiration:         true,
		Transition:         true,
		TransitionTiers:    minioLifecycleTiers,
		NoncurrentDays:     true,
		AbortMultipartDays: true,
	}
}

// GetLifecycle mirrors aws_s3.GetLifecycle exactly — MinIO speaks the
// same S3 lifecycle API verbatim. NoSuchLifecycleConfiguration is
// normalised to an empty rule slice for the same UX reason.
func (d *driver) GetLifecycle(ctx context.Context, bucketID string) ([]driverpkg.LifecycleRule, error) {
	out, err := d.s3Client.client.GetBucketLifecycleConfiguration(ctx, &s3.GetBucketLifecycleConfigurationInput{
		Bucket: aws.String(bucketID),
	})
	if err != nil {
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) {
			switch apiErr.ErrorCode() {
			case "NoSuchLifecycleConfiguration":
				return []driverpkg.LifecycleRule{}, nil
			case "NoSuchBucket":
				return nil, &driverpkg.Error{
					Op:      "GetLifecycle",
					Driver:  driverName,
					Err:     driverpkg.ErrNotFound,
					Message: "bucket not found",
				}
			case "AccessDenied":
				return nil, &driverpkg.Error{
					Op:      "GetLifecycle",
					Driver:  driverName,
					Err:     driverpkg.ErrPermissionDenied,
					Message: err.Error(),
				}
			}
		}
		return nil, &driverpkg.Error{
			Op:      "GetLifecycle",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	rules := make([]driverpkg.LifecycleRule, 0, len(out.Rules))
	for _, r := range out.Rules {
		rules = append(rules, minioRuleToDriver(r))
	}
	return rules, nil
}

// PutLifecycle mirrors aws_s3.PutLifecycle. Empty rules slice clears
// the policy via DeleteBucketLifecycle (same as S3).
func (d *driver) PutLifecycle(ctx context.Context, bucketID string, rules []driverpkg.LifecycleRule) error {
	if len(rules) == 0 {
		_, err := d.s3Client.client.DeleteBucketLifecycle(ctx, &s3.DeleteBucketLifecycleInput{
			Bucket: aws.String(bucketID),
		})
		if err != nil {
			return wrapMinIOLifecycleErr("PutLifecycle", err)
		}
		return nil
	}

	awsRules := make([]types.LifecycleRule, 0, len(rules))
	for _, r := range rules {
		awsRules = append(awsRules, driverRuleToMinIO(r))
	}

	_, err := d.s3Client.client.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(bucketID),
		LifecycleConfiguration: &types.BucketLifecycleConfiguration{
			Rules: awsRules,
		},
	})
	if err != nil {
		return wrapMinIOLifecycleErr("PutLifecycle", err)
	}
	return nil
}

// minioRuleToDriver / driverRuleToMinIO duplicate the aws_s3 logic.
// Kept in this package (rather than importing) so the two drivers
// stay independently evolvable — MinIO may grow tier-aliasing or
// custom-storage-class support that diverges from AWS.
func minioRuleToDriver(r types.LifecycleRule) driverpkg.LifecycleRule {
	out := driverpkg.LifecycleRule{
		Status: string(r.Status),
	}
	if r.ID != nil {
		out.ID = *r.ID
	}
	if r.Filter != nil && r.Filter.Prefix != nil {
		out.Prefix = *r.Filter.Prefix
	} else if r.Prefix != nil {
		out.Prefix = *r.Prefix
	}
	if r.Expiration != nil && r.Expiration.Days != nil {
		v := int(*r.Expiration.Days)
		out.ExpirationDays = &v
	}
	if len(r.Transitions) > 0 {
		t := r.Transitions[0]
		if t.Days != nil {
			v := int(*t.Days)
			out.TransitionDays = &v
		}
		if string(t.StorageClass) != "" {
			out.TransitionTier = string(t.StorageClass)
		}
	}
	if r.NoncurrentVersionExpiration != nil && r.NoncurrentVersionExpiration.NoncurrentDays != nil {
		v := int(*r.NoncurrentVersionExpiration.NoncurrentDays)
		out.NoncurrentDays = &v
	}
	if r.AbortIncompleteMultipartUpload != nil && r.AbortIncompleteMultipartUpload.DaysAfterInitiation != nil {
		v := int(*r.AbortIncompleteMultipartUpload.DaysAfterInitiation)
		out.AbortMultipartDays = &v
	}
	return out
}

func driverRuleToMinIO(r driverpkg.LifecycleRule) types.LifecycleRule {
	out := types.LifecycleRule{
		Status: types.ExpirationStatus(r.Status),
	}
	if r.ID != "" {
		id := r.ID
		out.ID = &id
	}
	prefix := r.Prefix
	out.Filter = &types.LifecycleRuleFilter{Prefix: &prefix}

	if r.ExpirationDays != nil {
		d := int32(*r.ExpirationDays) //nolint:gosec // bounded by UI
		out.Expiration = &types.LifecycleExpiration{Days: &d}
	}
	if r.TransitionDays != nil || r.TransitionTier != "" {
		t := types.Transition{}
		if r.TransitionDays != nil {
			d := int32(*r.TransitionDays) //nolint:gosec // bounded by UI
			t.Days = &d
		}
		if r.TransitionTier != "" {
			t.StorageClass = types.TransitionStorageClass(r.TransitionTier)
		}
		out.Transitions = []types.Transition{t}
	}
	if r.NoncurrentDays != nil {
		d := int32(*r.NoncurrentDays) //nolint:gosec // bounded by UI
		out.NoncurrentVersionExpiration = &types.NoncurrentVersionExpiration{NoncurrentDays: &d}
	}
	if r.AbortMultipartDays != nil {
		d := int32(*r.AbortMultipartDays) //nolint:gosec // bounded by UI
		out.AbortIncompleteMultipartUpload = &types.AbortIncompleteMultipartUpload{DaysAfterInitiation: &d}
	}
	return out
}

func wrapMinIOLifecycleErr(op string, err error) error {
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
		case "AccessDenied":
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
