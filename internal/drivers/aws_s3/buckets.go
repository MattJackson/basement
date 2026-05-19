package aws_s3

import (
	"context"
	"errors"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// ListBuckets returns all S3 buckets in the account.
// AWS bucket names are globally unique, so we use the name as both ID and alias.
func (d *driver) ListBuckets(ctx context.Context) ([]driverpkg.Bucket, error) {
	resp, err := d.s3Client.listBuckets(ctx)
	if err != nil {
		return nil, &driverpkg.Error{
			Op:      "ListBuckets",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	buckets := make([]driverpkg.Bucket, 0, len(resp.Buckets))
	for _, b := range resp.Buckets {
		bucket := driverpkg.Bucket{
			ID:      *b.Name,
			Aliases: []string{*b.Name}, // AWS bucket name doubles as alias
			Created: *b.CreationDate,
		}
		buckets = append(buckets, bucket)
	}

	return buckets, nil
}

// GetBucket fetches a single bucket by its ID (bucket name).
func (d *driver) GetBucket(ctx context.Context, id string) (driverpkg.Bucket, error) {
	_, err := d.s3Client.headBucket(ctx, id)
	if err != nil {
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchBucket" {
			return driverpkg.Bucket{}, &driverpkg.Error{
				Op:      "GetBucket",
				Driver:  driverName,
				Err:     driverpkg.ErrNotFound,
				Message: "bucket not found",
			}
		}

		return driverpkg.Bucket{}, &driverpkg.Error{
			Op:      "GetBucket",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	// For a bucket that exists but has no creation date metadata (unlikely), use zero time
	return driverpkg.Bucket{
		ID:      id,
		Aliases: []string{id},
		Created: time.Time{},
	}, nil
}

// CreateBucket creates a new S3 bucket with the given alias (bucket name).
func (d *driver) CreateBucket(ctx context.Context, spec driverpkg.BucketSpec) (driverpkg.Bucket, error) {
	_, err := d.s3Client.createBucket(ctx, spec.Alias, "")
	if err != nil {
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) {
			switch apiErr.ErrorCode() {
			case "BucketAlreadyExists", "BucketAlreadyOwnedByYou":
				return driverpkg.Bucket{}, &driverpkg.Error{
					Op:      "CreateBucket",
					Driver:  driverName,
					Err:     driverpkg.ErrConflict,
					Message: "bucket already exists",
				}
			}
		}

		return driverpkg.Bucket{}, &driverpkg.Error{
			Op:      "CreateBucket",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	return driverpkg.Bucket{
		ID:      spec.Alias,
		Aliases: []string{spec.Alias},
		Created: time.Now(),
	}, nil
}

// UpdateBucket returns ErrUnsupported since AWS S3 doesn't have a simple
// bucket-level alias/quota update API at this abstraction level.
func (d *driver) UpdateBucket(_ context.Context, _ string, _ driverpkg.BucketUpdate) (driverpkg.Bucket, error) {
	return driverpkg.Bucket{}, d.unsupported("UpdateBucket")
}

// DeleteBucket deletes an S3 bucket. The bucket must be empty.
func (d *driver) DeleteBucket(ctx context.Context, id string) error {
	err := d.s3Client.deleteBucket(ctx, id)
	if err != nil {
		var apiErr interface{ ErrorCode() string }
		if errors.As(err, &apiErr) {
			switch apiErr.ErrorCode() {
			case "BucketNotEmpty":
				return &driverpkg.Error{
					Op:      "DeleteBucket",
					Driver:  driverName,
					Err:     driverpkg.ErrConflict,
					Message: "bucket not empty",
				}
			case "NoSuchBucket":
				return &driverpkg.Error{
					Op:      "DeleteBucket",
					Driver:  driverName,
					Err:     driverpkg.ErrNotFound,
					Message: "bucket not found",
				}
			}
		}

		return &driverpkg.Error{
			Op:      "DeleteBucket",
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: err.Error(),
		}
	}

	return nil
}
