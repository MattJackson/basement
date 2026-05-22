package aws_s3

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// awsLifecycleTiers lists the S3 storage classes basement surfaces
// in the lifecycle wizard. INTELLIGENT_TIERING + GLACIER_IR + ONEZONE_IA
// are intentionally omitted from the wizard for v0.9.0i — the four
// here cover the operator-mentioned use cases ("after 30 days, glacier";
// "after 7 days, IA"). Future cycles can extend without an interface
// change since the UI dropdown reads this slice via /lifecycle GET.
var awsLifecycleTiers = []string{
	string(types.TransitionStorageClassStandardIa),
	string(types.TransitionStorageClassGlacier),
	string(types.TransitionStorageClassDeepArchive),
}

// PerBucketStatsAvailable reports whether ListBuckets / GetBucket on
// the user-region tier surfaces Objects + Bytes. v1.4.0a: AWS S3
// returns true. The driver's bucket flows wrap S3 ListObjectsV2 +
// optional CloudWatch / Storage Lens hooks to populate the counters
// when a basement caller asks for them, so the FE can keep the
// columns visible against AWS-backed regions.
func (d *driver) PerBucketStatsAvailable() bool {
	return true
}

// LifecycleSupport reports lifecycle capabilities for AWS S3. All four
// flag sub-axes are supported — S3 is the reference implementation
// the wire types are modeled after.
func (d *driver) LifecycleSupport() driverpkg.LifecycleCapabilities {
	return driverpkg.LifecycleCapabilities{
		Supported:          true,
		Expiration:         true,
		Transition:         true,
		TransitionTiers:    awsLifecycleTiers,
		NoncurrentDays:     true,
		AbortMultipartDays: true,
	}
}

// GetLifecycle fetches the bucket's current lifecycle configuration.
// A bucket with no configured policy returns NoSuchLifecycleConfiguration;
// we treat that as an empty rule set, not an error — the UI's "no rules"
// branch fires on len(rules)==0, and surfacing a 404 here would force
// every screen to special-case that path.
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
		rules = append(rules, awsRuleToDriver(r))
	}
	return rules, nil
}

// PutLifecycle replaces the bucket's lifecycle policy. An empty rules
// slice clears the policy (we DeleteBucketLifecycle when no rules are
// supplied — PutBucketLifecycleConfiguration with an empty Rules slice
// is a validation error in S3).
func (d *driver) PutLifecycle(ctx context.Context, bucketID string, rules []driverpkg.LifecycleRule) error {
	if len(rules) == 0 {
		_, err := d.s3Client.client.DeleteBucketLifecycle(ctx, &s3.DeleteBucketLifecycleInput{
			Bucket: aws.String(bucketID),
		})
		if err != nil {
			return wrapAWSLifecycleErr("PutLifecycle", err)
		}
		return nil
	}

	awsRules := make([]types.LifecycleRule, 0, len(rules))
	for _, r := range rules {
		awsRules = append(awsRules, driverRuleToAWS(r))
	}

	_, err := d.s3Client.client.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(bucketID),
		LifecycleConfiguration: &types.BucketLifecycleConfiguration{
			Rules: awsRules,
		},
	})
	if err != nil {
		return wrapAWSLifecycleErr("PutLifecycle", err)
	}
	return nil
}

// awsRuleToDriver flattens an AWS LifecycleRule into our wire shape.
// We surface the FIRST transition only — multi-transition rules
// (Standard-IA → Glacier → Deep-Archive in one rule) are rare and the
// wizard models each tier as its own rule for clarity. Operators who
// genuinely need chained transitions can write raw JSON via the AWS
// console; the wizard is intentionally opinionated.
func awsRuleToDriver(r types.LifecycleRule) driverpkg.LifecycleRule {
	out := driverpkg.LifecycleRule{
		Status: string(r.Status),
	}
	if r.ID != nil {
		out.ID = *r.ID
	}
	// Prefer Filter.Prefix when present (the modern shape); fall back
	// to the deprecated top-level Prefix for older policies.
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

// driverRuleToAWS inflates a flat driver rule into the AWS SDK shape.
// We always emit Filter.Prefix (not the deprecated top-level Prefix)
// so newly-written rules don't show up as "legacy" in the AWS console.
func driverRuleToAWS(r driverpkg.LifecycleRule) types.LifecycleRule {
	out := types.LifecycleRule{
		Status: types.ExpirationStatus(r.Status),
	}
	if r.ID != "" {
		id := r.ID
		out.ID = &id
	}
	// S3 requires a Filter on every rule; an empty-prefix filter is
	// the "applies to all objects" shape.
	prefix := r.Prefix
	out.Filter = &types.LifecycleRuleFilter{Prefix: &prefix}

	if r.ExpirationDays != nil {
		d := int32(*r.ExpirationDays) //nolint:gosec // bounded by UI 1..36500
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

// wrapAWSLifecycleErr maps S3 SDK errors to driver sentinels.
func wrapAWSLifecycleErr(op string, err error) error {
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
