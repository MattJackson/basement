package minio

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// SSESupport reports bucket-default encryption capability (v1.10.0d).
// MinIO speaks the S3 PutBucketEncryption API verbatim — SSE-S3
// works out of the box; SSE-KMS works when MinIO is deployed with
// KES (Key Encryption Service) wired to a backing KMS (Vault, AWS
// KMS, Google KMS, etc.). We advertise both true; operators who run
// MinIO without KES will get a 4xx from the backend when they try to
// enable SSE-KMS, surfaced verbatim via writeDriverError.
func (d *driver) SSESupport() (bool, bool) {
	return true, true
}

// GetBucketEncryption mirrors the aws_s3 implementation — MinIO
// speaks the same S3 GetBucketEncryption API. A
// "ServerSideEncryptionConfigurationNotFoundError" from the backend
// means "never configured", surfaced as {Enabled: false}.
func (d *driver) GetBucketEncryption(ctx context.Context, bucket string) (*driverpkg.BucketEncryption, error) {
	out, err := d.s3Client.client.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "ServerSideEncryptionConfigurationNotFoundError" {
			return &driverpkg.BucketEncryption{Enabled: false}, nil
		}
		return nil, wrapMinioEncryptionErr("GetBucketEncryption", err)
	}

	enc := &driverpkg.BucketEncryption{}
	if out.ServerSideEncryptionConfiguration == nil ||
		len(out.ServerSideEncryptionConfiguration.Rules) == 0 {
		return enc, nil
	}
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
// configuration. Same validation as the aws_s3 implementation:
// algorithm shape + KMSKeyID-required-for-SSE-KMS.
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
		rule.BucketKeyEnabled = aws.Bool(true)
	}

	_, err := d.s3Client.client.PutBucketEncryption(ctx, &s3.PutBucketEncryptionInput{
		Bucket: aws.String(bucket),
		ServerSideEncryptionConfiguration: &types.ServerSideEncryptionConfiguration{
			Rules: []types.ServerSideEncryptionRule{rule},
		},
	})
	if err != nil {
		return wrapMinioEncryptionErr("PutBucketEncryption", err)
	}
	return nil
}

// DeleteBucketEncryption removes the bucket-level configuration.
// Idempotent: a "never configured" backend response is mapped to
// success.
func (d *driver) DeleteBucketEncryption(ctx context.Context, bucket string) error {
	_, err := d.s3Client.client.DeleteBucketEncryption(ctx, &s3.DeleteBucketEncryptionInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "ServerSideEncryptionConfigurationNotFoundError" {
			return nil
		}
		return wrapMinioEncryptionErr("DeleteBucketEncryption", err)
	}
	return nil
}

// wrapMinioEncryptionErr mirrors wrapAWSEncryptionErr — same S3 error
// codes, same driver-sentinel mapping. Duplicated rather than shared
// because each driver wraps with its own driverName.
func wrapMinioEncryptionErr(op string, err error) error {
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
