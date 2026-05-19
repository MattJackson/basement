// Package garage implements the garage device driver.
package garage

import (
	"context"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

func init() {
	driverpkg.Register("garage", newDriver)
}

type driver struct{}

func newDriver(cfg driverpkg.Config) (driverpkg.Driver, error) {
	return &driver{}, nil
}

func (d *driver) unsupported(op string) error {
	return &driverpkg.Error{
		Op:      op,
		Driver:  "garage",
		Err:     driverpkg.ErrUnsupported,
		Message: "not implemented yet",
	}
}

func (d *driver) Capabilities(ctx context.Context) (driverpkg.Caps, error) {
	return driverpkg.Caps{}, &driverpkg.Error{Op: "Capabilities", Driver: "garage", Err: driverpkg.ErrUnsupported, Message: "not implemented yet"}
}

func (d *driver) HealthCheck(ctx context.Context) (driverpkg.HealthReport, error) {
	return driverpkg.HealthReport{}, d.unsupported("HealthCheck")
}

func (d *driver) ListNodes(ctx context.Context) ([]driverpkg.Node, error) {
	return nil, d.unsupported("ListNodes")
}

func (d *driver) GetLayout(ctx context.Context) (driverpkg.Layout, error) {
	return driverpkg.Layout{}, d.unsupported("GetLayout")
}

func (d *driver) StageLayout(ctx context.Context, change driverpkg.LayoutChange) (driverpkg.LayoutDiff, error) {
	return driverpkg.LayoutDiff{}, d.unsupported("StageLayout")
}

func (d *driver) ApplyLayout(ctx context.Context) error {
	return d.unsupported("ApplyLayout")
}

func (d *driver) RevertLayout(ctx context.Context) error {
	return d.unsupported("RevertLayout")
}

func (d *driver) ListBuckets(ctx context.Context) ([]driverpkg.Bucket, error) {
	return nil, d.unsupported("ListBuckets")
}

func (d *driver) GetBucket(ctx context.Context, id string) (driverpkg.Bucket, error) {
	return driverpkg.Bucket{}, d.unsupported("GetBucket")
}

func (d *driver) CreateBucket(ctx context.Context, spec driverpkg.BucketSpec) (driverpkg.Bucket, error) {
	return driverpkg.Bucket{}, d.unsupported("CreateBucket")
}

func (d *driver) UpdateBucket(ctx context.Context, id string, update driverpkg.BucketUpdate) (driverpkg.Bucket, error) {
	return driverpkg.Bucket{}, d.unsupported("UpdateBucket")
}

func (d *driver) DeleteBucket(ctx context.Context, id string) error {
	return d.unsupported("DeleteBucket")
}

func (d *driver) ListKeys(ctx context.Context) ([]driverpkg.Key, error) {
	return nil, d.unsupported("ListKeys")
}

func (d *driver) GetKey(ctx context.Context, id string) (driverpkg.Key, error) {
	return driverpkg.Key{}, d.unsupported("GetKey")
}

func (d *driver) CreateKey(ctx context.Context, spec driverpkg.KeySpec) (driverpkg.Key, error) {
	return driverpkg.Key{}, d.unsupported("CreateKey")
}

func (d *driver) UpdateKeyPermissions(ctx context.Context, keyID string, perms []driverpkg.BucketPermission) error {
	return d.unsupported("UpdateKeyPermissions")
}

func (d *driver) DeleteKey(ctx context.Context, id string) error {
	return d.unsupported("DeleteKey")
}

func (d *driver) ListObjects(ctx context.Context, bucket, prefix, continuation string, limit int) (driverpkg.ObjectPage, error) {
	return driverpkg.ObjectPage{}, d.unsupported("ListObjects")
}

func (d *driver) StatObject(ctx context.Context, bucket, key string) (driverpkg.ObjectInfo, error) {
	return driverpkg.ObjectInfo{}, d.unsupported("StatObject")
}

func (d *driver) PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (driverpkg.PresignedURL, error) {
	return driverpkg.PresignedURL{}, d.unsupported("PresignGet")
}

func (d *driver) PresignPut(ctx context.Context, bucket, key string, ttl time.Duration, contentType string) (driverpkg.PresignedURL, error) {
	return driverpkg.PresignedURL{}, d.unsupported("PresignPut")
}

func (d *driver) DeleteObject(ctx context.Context, bucket, key string) error {
	return d.unsupported("DeleteObject")
}

func (d *driver) CreateMultipart(ctx context.Context, bucket, key, contentType string) (driverpkg.MultipartUpload, error) {
	return driverpkg.MultipartUpload{}, d.unsupported("CreateMultipart")
}

func (d *driver) PresignUploadPart(ctx context.Context, upload driverpkg.MultipartUpload, partNum int) (driverpkg.PresignedURL, error) {
	return driverpkg.PresignedURL{}, d.unsupported("PresignUploadPart")
}

func (d *driver) CompleteMultipart(ctx context.Context, upload driverpkg.MultipartUpload, parts []driverpkg.CompletedPart) error {
	return d.unsupported("CompleteMultipart")
}

func (d *driver) AbortMultipart(ctx context.Context, upload driverpkg.MultipartUpload) error {
	return d.unsupported("AbortMultipart")
}
