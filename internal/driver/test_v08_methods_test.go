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
