package sync

import (
	"context"
	"io"
	"time"

	"github.com/mattjackson/basement/internal/driver"
)

// baseDriver is a no-op driver.Driver. It implements every interface method so
// test fakes can embed it and override only the handful of methods the sync
// engine exercises. Unimplemented methods return ErrUnsupported / zero values.
type baseDriver struct{}

func (baseDriver) Capabilities(ctx context.Context) (driver.Caps, error) {
	return driver.Caps{}, nil
}
func (baseDriver) HealthCheck(ctx context.Context) (driver.HealthReport, error) {
	return driver.HealthReport{}, driver.ErrUnsupported
}
func (baseDriver) ListNodes(ctx context.Context) ([]driver.Node, error) {
	return nil, driver.ErrUnsupported
}
func (baseDriver) GetLayout(ctx context.Context) (driver.Layout, error) {
	return driver.Layout{}, driver.ErrUnsupported
}
func (baseDriver) StageLayout(ctx context.Context, change driver.LayoutChange) (driver.LayoutDiff, error) {
	return driver.LayoutDiff{}, driver.ErrUnsupported
}
func (baseDriver) ApplyLayout(ctx context.Context) error  { return driver.ErrUnsupported }
func (baseDriver) RevertLayout(ctx context.Context) error { return driver.ErrUnsupported }
func (baseDriver) ListBuckets(ctx context.Context) ([]driver.Bucket, error) {
	return nil, driver.ErrUnsupported
}
func (baseDriver) GetBucket(ctx context.Context, id string) (driver.Bucket, error) {
	return driver.Bucket{}, driver.ErrUnsupported
}
func (baseDriver) CreateBucket(ctx context.Context, spec driver.BucketSpec) (driver.Bucket, error) {
	return driver.Bucket{}, driver.ErrUnsupported
}
func (baseDriver) UpdateBucket(ctx context.Context, id string, update driver.BucketUpdate) (driver.Bucket, error) {
	return driver.Bucket{}, driver.ErrUnsupported
}
func (baseDriver) DeleteBucket(ctx context.Context, id string) error { return driver.ErrUnsupported }
func (baseDriver) ListKeys(ctx context.Context) ([]driver.Key, error) {
	return nil, driver.ErrUnsupported
}
func (baseDriver) GetKey(ctx context.Context, id string) (driver.Key, error) {
	return driver.Key{}, driver.ErrUnsupported
}
func (baseDriver) CreateKey(ctx context.Context, spec driver.KeySpec) (driver.Key, error) {
	return driver.Key{}, driver.ErrUnsupported
}
func (baseDriver) UpdateKeyPermissions(ctx context.Context, keyID string, perms []driver.BucketPermission) error {
	return driver.ErrUnsupported
}
func (baseDriver) DeleteKey(ctx context.Context, id string) error { return driver.ErrUnsupported }
func (baseDriver) ListObjects(ctx context.Context, bucket, prefix, continuation, delimiter string, limit int) (driver.ObjectPage, error) {
	return driver.ObjectPage{}, driver.ErrUnsupported
}
func (baseDriver) StatObject(ctx context.Context, bucket, key string) (driver.ObjectInfo, error) {
	return driver.ObjectInfo{}, driver.ErrNotFound
}
func (baseDriver) PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, driver.ErrUnsupported
}
func (baseDriver) PresignPut(ctx context.Context, bucket, key string, ttl time.Duration, contentType string) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, driver.ErrUnsupported
}
func (baseDriver) DeleteObject(ctx context.Context, bucket, key string) error {
	return driver.ErrUnsupported
}
func (baseDriver) CreateMultipart(ctx context.Context, bucket, key, contentType string) (driver.MultipartUpload, error) {
	return driver.MultipartUpload{}, driver.ErrUnsupported
}
func (baseDriver) PresignUploadPart(ctx context.Context, upload driver.MultipartUpload, partNum int) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, driver.ErrUnsupported
}
func (baseDriver) CompleteMultipart(ctx context.Context, upload driver.MultipartUpload, parts []driver.CompletedPart) error {
	return driver.ErrUnsupported
}
func (baseDriver) AbortMultipart(ctx context.Context, upload driver.MultipartUpload) error {
	return driver.ErrUnsupported
}
func (baseDriver) StreamObject(ctx context.Context, bucket, key, rng string) (driver.StreamResult, error) {
	return driver.StreamResult{}, driver.ErrUnsupported
}
func (baseDriver) PutObjectStream(ctx context.Context, bucket, key string, reader io.Reader, contentType string, size int64) (driver.PutResult, error) {
	return driver.PutResult{}, driver.ErrUnsupported
}
func (baseDriver) ServerSideCopy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	return driver.ErrUnsupported
}
func (baseDriver) LifecycleSupport() driver.LifecycleCapabilities {
	return driver.LifecycleCapabilities{}
}
func (baseDriver) GetLifecycle(ctx context.Context, bucketID string) ([]driver.LifecycleRule, error) {
	return nil, driver.ErrUnsupported
}
func (baseDriver) PutLifecycle(ctx context.Context, bucketID string, rules []driver.LifecycleRule) error {
	return driver.ErrUnsupported
}
func (baseDriver) PerBucketStatsAvailable() bool        { return false }
func (baseDriver) ScrubSupport() driver.ScrubCapability { return driver.ScrubCapability{} }
func (baseDriver) ScrubState(ctx context.Context) (driver.ScrubState, error) {
	return driver.ScrubState{}, driver.ErrUnsupported
}
func (baseDriver) StartScrub(ctx context.Context) error { return driver.ErrUnsupported }
func (baseDriver) VersioningSupport() bool              { return false }
func (baseDriver) GetVersioningStatus(ctx context.Context, bucket string) (driver.VersioningStatus, error) {
	return driver.VersioningDisabled, driver.ErrUnsupported
}
func (baseDriver) EnableVersioning(ctx context.Context, bucket string) error {
	return driver.ErrUnsupported
}
func (baseDriver) SuspendVersioning(ctx context.Context, bucket string) error {
	return driver.ErrUnsupported
}
func (baseDriver) ListObjectVersions(ctx context.Context, bucket, prefix, versionIDMarker string, limit int) ([]driver.ObjectVersion, string, error) {
	return nil, "", driver.ErrUnsupported
}
func (baseDriver) GetObjectVersion(ctx context.Context, bucket, key, versionID string) (driver.StreamResult, error) {
	return driver.StreamResult{}, driver.ErrUnsupported
}
func (baseDriver) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	return driver.ErrUnsupported
}
func (baseDriver) ObjectLockSupport() bool { return false }
func (baseDriver) GetObjectLockConfig(ctx context.Context, bucket string) (*driver.ObjectLockConfig, error) {
	return nil, driver.ErrUnsupported
}
func (baseDriver) PutObjectLockConfig(ctx context.Context, bucket string, cfg driver.ObjectLockConfig) error {
	return driver.ErrUnsupported
}
func (baseDriver) GetObjectRetention(ctx context.Context, bucket, key, versionID string) (*driver.ObjectLockRetention, error) {
	return nil, driver.ErrUnsupported
}
func (baseDriver) PutObjectRetention(ctx context.Context, bucket, key, versionID string, retention driver.ObjectLockRetention, bypassGovernance bool) error {
	return driver.ErrUnsupported
}
func (baseDriver) GetObjectLegalHold(ctx context.Context, bucket, key, versionID string) (bool, error) {
	return false, driver.ErrUnsupported
}
func (baseDriver) PutObjectLegalHold(ctx context.Context, bucket, key, versionID string, on bool) error {
	return driver.ErrUnsupported
}
func (baseDriver) SSESupport() (s3 bool, kms bool) { return false, false }
func (baseDriver) GetBucketEncryption(ctx context.Context, bucket string) (*driver.BucketEncryption, error) {
	return nil, driver.ErrUnsupported
}
func (baseDriver) PutBucketEncryption(ctx context.Context, bucket string, enc driver.BucketEncryption) error {
	return driver.ErrUnsupported
}
func (baseDriver) DeleteBucketEncryption(ctx context.Context, bucket string) error {
	return driver.ErrUnsupported
}

// compile-time assertion that baseDriver satisfies the interface.
var _ driver.Driver = baseDriver{}
