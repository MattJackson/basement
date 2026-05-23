package driver

import (
	"context"
	"io"
)

// v0.8.0a (DRIVER.STREAM) + v0.8.0b (DRIVER.COPY) added three Driver
// interface methods. The existing mockDriver in driver_test.go was
// written before that. These shims satisfy the interface so the
// older tests keep compiling.

func (m *mockDriver) StreamObject(_ context.Context, _, _, _ string) (StreamResult, error) {
	return StreamResult{}, nil
}

func (m *mockDriver) PutObjectStream(_ context.Context, _, _ string, _ io.Reader, _ string, _ int64) (PutResult, error) {
	return PutResult{}, nil
}

func (m *mockDriver) ServerSideCopy(_ context.Context, _, _, _, _ string) error {
	return nil
}

// v0.9.0i (LIFECYCLE.WIZARD) shims — registry tests don't exercise
// the new lifecycle methods so cheap no-op stubs are fine here.

func (m *mockDriver) LifecycleSupport() LifecycleCapabilities {
	return LifecycleCapabilities{Supported: false}
}

func (m *mockDriver) GetLifecycle(_ context.Context, _ string) ([]LifecycleRule, error) {
	return nil, nil
}

func (m *mockDriver) PutLifecycle(_ context.Context, _ string, _ []LifecycleRule) error {
	return nil
}

// v1.4.0a shim — mock advertises no per-bucket stats; tests that
// care override via embedding/composition.
func (m *mockDriver) PerBucketStatsAvailable() bool {
	return false
}

// v1.4.0c SCRUB.MAINT shims — mock advertises no scrub support.
func (m *mockDriver) ScrubSupport() ScrubCapability {
	return ScrubCapability{Supported: false}
}

func (m *mockDriver) ScrubState(_ context.Context) (ScrubState, error) {
	return ScrubState{}, ErrUnsupported
}

func (m *mockDriver) StartScrub(_ context.Context) error {
	return ErrUnsupported
}

// v1.10.0a versioning shims — mock advertises no versioning support
// (matches Garage v1 / v2 posture). Drivers that care override these
// per-test; for registry-level tests the unsupported defaults keep
// the interface satisfied without dragging real S3 versioning shape
// into the registry test suite.

func (m *mockDriver) VersioningSupport() bool { return false }

func (m *mockDriver) GetVersioningStatus(_ context.Context, _ string) (VersioningStatus, error) {
	return VersioningDisabled, ErrUnsupported
}

func (m *mockDriver) EnableVersioning(_ context.Context, _ string) error {
	return ErrUnsupported
}

func (m *mockDriver) SuspendVersioning(_ context.Context, _ string) error {
	return ErrUnsupported
}

func (m *mockDriver) ListObjectVersions(_ context.Context, _, _, _ string, _ int) ([]ObjectVersion, string, error) {
	return nil, "", ErrUnsupported
}

func (m *mockDriver) GetObjectVersion(_ context.Context, _, _, _ string) (StreamResult, error) {
	return StreamResult{}, ErrUnsupported
}

func (m *mockDriver) DeleteObjectVersion(_ context.Context, _, _, _ string) error {
	return ErrUnsupported
}

// v1.10.0c Object Lock shims — same pattern as the versioning shims
// above. The driver-package registry tests don't exercise Object Lock
// directly; these stubs satisfy the interface so the existing tests
// keep compiling.

func (m *mockDriver) ObjectLockSupport() bool { return false }

func (m *mockDriver) GetObjectLockConfig(_ context.Context, _ string) (*ObjectLockConfig, error) {
	return nil, ErrUnsupported
}

func (m *mockDriver) PutObjectLockConfig(_ context.Context, _ string, _ ObjectLockConfig) error {
	return ErrUnsupported
}

func (m *mockDriver) GetObjectRetention(_ context.Context, _, _, _ string) (*ObjectLockRetention, error) {
	return nil, ErrUnsupported
}

func (m *mockDriver) PutObjectRetention(_ context.Context, _, _, _ string, _ ObjectLockRetention, _ bool) error {
	return ErrUnsupported
}

func (m *mockDriver) GetObjectLegalHold(_ context.Context, _, _, _ string) (bool, error) {
	return false, ErrUnsupported
}

func (m *mockDriver) PutObjectLegalHold(_ context.Context, _, _, _ string, _ bool) error {
	return ErrUnsupported
}

// v1.10.0d Bucket Encryption shims — same pattern as the Object Lock
// shims above. The driver-package registry tests don't exercise SSE
// directly; these stubs satisfy the interface so the existing tests
// keep compiling.

func (m *mockDriver) SSESupport() (bool, bool) { return false, false }

func (m *mockDriver) GetBucketEncryption(_ context.Context, _ string) (*BucketEncryption, error) {
	return nil, ErrUnsupported
}

func (m *mockDriver) PutBucketEncryption(_ context.Context, _ string, _ BucketEncryption) error {
	return ErrUnsupported
}

func (m *mockDriver) DeleteBucketEncryption(_ context.Context, _ string) error {
	return ErrUnsupported
}
