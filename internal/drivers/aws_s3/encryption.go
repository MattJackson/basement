package aws_s3

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// SSESupport reports the two server-side encryption capability bits
// (v1.10.0d). AWS S3 supports the full surface: SSE-S3 (AES256, S3
// manages the data key end-to-end) AND SSE-KMS (operator supplies the
// KMS key ARN, S3 calls KMS for every encrypt/decrypt unless
// BucketKey is on). So both bits return true.
func (d *driver) SSESupport() (bool, bool) {
	return true, true
}

// GetBucketEncryption reads the bucket's default encryption
// configuration. S3 returns "ServerSideEncryptionConfigurationNotFoundError"
// on buckets that have never had encryption set — we normalize that
// to {Enabled: false} so the FE can render the off-state UI without
// an error banner.
//
// Multi-rule configurations are theoretically supported by S3 but in
// practice only the first rule is honoured; we surface the first
// rule's ApplyServerSideEncryptionByDefault block as the canonical
// state.
func (d *driver) GetBucketEncryption(ctx context.Context, bucket string) (*driverpkg.BucketEncryption, error) {
	out, err := d.s3Client.client.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		// "ServerSideEncryptionConfigurationNotFoundError" is S3's
		// "never set" sentinel — surface as {Enabled: false} rather
		// than an error so the FE doesn't have to special-case it.
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "ServerSideEncryptionConfigurationNotFoundError" {
			return &driverpkg.BucketEncryption{Enabled: false}, nil
		}
		return nil, wrapAWSEncryptionErr("GetBucketEncryption", err)
	}

	enc := &driverpkg.BucketEncryption{}
	if out.ServerSideEncryptionConfiguration == nil ||
		len(out.ServerSideEncryptionConfiguration.Rules) == 0 {
		return enc, nil
	}
	// S3's response may contain multiple rules; only the first one is
	// honoured per AWS docs. Take it as authoritative.
	rule := out.ServerSideEncryptionConfiguration.Rules[0]
	if rule.ApplyServerSideEncryptionByDefault != nil {
		enc.Enabled = true
		def := rule.ApplyServerSideEncryptionByDefault
		enc.Algorithm = driverpkg.SSEAlgorithm(def.SSEAlgorithm)
		if def.KMSMasterKeyID != nil {
			enc.KMSKeyID = *def.KMSMasterKeyID
		}
	}
	if rule.BucketKeyEnabled != nil {
		enc.BucketKey = *rule.BucketKeyEnabled
	}
	return enc, nil
}

// PutBucketEncryption writes the bucket-level default encryption
// configuration. Validates the algorithm shape up front so a
// malformed request surfaces as ErrInvalid rather than a confusing
// 400 from S3.
//
// SSE-KMS requires a non-empty KMSKeyID — the API layer rejects this
// at the wire boundary too, but the driver stays defensive.
func (d *driver) PutBucketEncryption(ctx context.Context, bucket string, enc driverpkg.BucketEncryption) error {
	if !enc.Enabled {
		return &driverpkg.Error{
			Op:      "PutBucketEncryption",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: "PutBucketEncryption requires Enabled=true; use DeleteBucketEncryption to clear",
		}
	}
	if enc.Algorithm != driverpkg.SSEAlgorithmAES256 &&
		enc.Algorithm != driverpkg.SSEAlgorithmKMS {
		return &driverpkg.Error{
			Op:      "PutBucketEncryption",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: `algorithm must be "AES256" or "aws:kms"`,
		}
	}
	if enc.Algorithm == driverpkg.SSEAlgorithmKMS && enc.KMSKeyID == "" {
		return &driverpkg.Error{
			Op:      "PutBucketEncryption",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: "kmsKeyId required when algorithm is aws:kms",
		}
	}

	def := &types.ServerSideEncryptionByDefault{
		SSEAlgorithm: types.ServerSideEncryption(enc.Algorithm),
	}
	if enc.Algorithm == driverpkg.SSEAlgorithmKMS {
		def.KMSMasterKeyID = aws.String(enc.KMSKeyID)
	}
	rule := types.ServerSideEncryptionRule{
		ApplyServerSideEncryptionByDefault: def,
	}
	if enc.BucketKey {
		// Only meaningful for SSE-KMS — S3 silently ignores it on
		// SSE-S3 buckets, so we set it unconditionally when requested
		// rather than gating on Algorithm.
		rule.BucketKeyEnabled = aws.Bool(true)
	}

	_, err := d.s3Client.client.PutBucketEncryption(ctx, &s3.PutBucketEncryptionInput{
		Bucket: aws.String(bucket),
		ServerSideEncryptionConfiguration: &types.ServerSideEncryptionConfiguration{
			Rules: []types.ServerSideEncryptionRule{rule},
		},
	})
	if err != nil {
		return wrapAWSEncryptionErr("PutBucketEncryption", err)
	}
	return nil
}

// DeleteBucketEncryption removes the bucket-level configuration
// entirely. Distinct from a PUT with Enabled=false — S3's wire shape
// is a dedicated DeleteBucketEncryption call. Idempotent on buckets
// that have no configuration set (the S3 SDK swallows the not-found
// case under most circumstances; we map any leftover error through
// the standard wrapper).
func (d *driver) DeleteBucketEncryption(ctx context.Context, bucket string) error {
	_, err := d.s3Client.client.DeleteBucketEncryption(ctx, &s3.DeleteBucketEncryptionInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		// Treat "never configured" as success — idempotent contract.
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "ServerSideEncryptionConfigurationNotFoundError" {
			return nil
		}
		return wrapAWSEncryptionErr("DeleteBucketEncryption", err)
	}
	return nil
}

// wrapAWSEncryptionErr maps S3 SDK encryption errors to driver
// sentinels. Mirrors wrapAWSObjectLockErr — same NoSuchBucket /
// AccessDenied mapping, plus InvalidArgument for malformed KMS key
// ARNs and KMS.KMSInvalidStateException-style errors which S3 returns
// when the configured KMS key has been disabled.
func wrapAWSEncryptionErr(op string, err error) error {
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
		case "AccessDenied", "Forbidden":
			return &driverpkg.Error{
				Op:      op,
				Driver:  driverName,
				Err:     driverpkg.ErrPermissionDenied,
				Message: err.Error(),
			}
		case "InvalidArgument", "MalformedXML":
			return &driverpkg.Error{
				Op:      op,
				Driver:  driverName,
				Err:     driverpkg.ErrInvalid,
				Message: err.Error(),
			}
		case "KMSInvalidStateException", "KMSDisabledException", "KMSAccessDeniedException":
			// The configured KMS key has been disabled or the
			// backend lost access to it — surface as ErrConflict so
			// the API maps to 409 and the FE can suggest "check
			// your KMS key state".
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
