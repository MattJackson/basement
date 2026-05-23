package api

import (
	"context"
	"io"

	"github.com/mattjackson/basement/internal/driver"
)

// v0.8.0a (DRIVER.STREAM) + v0.8.0b (DRIVER.COPY) added three Driver
// interface methods. Multiple test-only driver mocks in this package
// predate those methods. These shims satisfy the interface so the
// older tests keep compiling.

func (m *layoutDriver) StreamObject(_ context.Context, _, _, _ string) (driver.StreamResult, error) {
	return driver.StreamResult{}, nil
}
func (m *layoutDriver) PutObjectStream(_ context.Context, _, _ string, _ io.Reader, _ string, _ int64) (driver.PutResult, error) {
	return driver.PutResult{}, nil
}
func (m *layoutDriver) ServerSideCopy(_ context.Context, _, _, _, _ string) error {
	return nil
}

func (m *stubDriver) StreamObject(_ context.Context, _, _, _ string) (driver.StreamResult, error) {
	return driver.StreamResult{}, nil
}
func (m *stubDriver) PutObjectStream(_ context.Context, _, _ string, _ io.Reader, _ string, _ int64) (driver.PutResult, error) {
	return driver.PutResult{}, nil
}
func (m *stubDriver) ServerSideCopy(_ context.Context, _, _, _, _ string) error {
	return nil
}

func (m *mockDriver) StreamObject(_ context.Context, _, _, _ string) (driver.StreamResult, error) {
	return driver.StreamResult{}, nil
}
func (m *mockDriver) PutObjectStream(_ context.Context, _, _ string, _ io.Reader, _ string, _ int64) (driver.PutResult, error) {
	return driver.PutResult{}, nil
}
func (m *mockDriver) ServerSideCopy(_ context.Context, _, _, _, _ string) error {
	return nil
}

func (m *fanoutDriver) StreamObject(_ context.Context, _, _, _ string) (driver.StreamResult, error) {
	return driver.StreamResult{}, nil
}
func (m *fanoutDriver) PutObjectStream(_ context.Context, _, _ string, _ io.Reader, _ string, _ int64) (driver.PutResult, error) {
	return driver.PutResult{}, nil
}
func (m *fanoutDriver) ServerSideCopy(_ context.Context, _, _, _, _ string) error {
	return nil
}

// v0.9.0i (LIFECYCLE.WIZARD) shims for the same set of test-only
// drivers. Default behaviour: report unsupported + return nil/empty
// from Get/Put so tests that don't exercise lifecycle compile cleanly.
// Tests that DO exercise lifecycle (admin_lifecycle_test.go) override
// the methods at call time via embedding.

func (m *layoutDriver) LifecycleSupport() driver.LifecycleCapabilities {
	return driver.LifecycleCapabilities{Supported: false}
}
func (m *layoutDriver) GetLifecycle(_ context.Context, _ string) ([]driver.LifecycleRule, error) {
	return nil, nil
}
func (m *layoutDriver) PutLifecycle(_ context.Context, _ string, _ []driver.LifecycleRule) error {
	return nil
}

func (m *stubDriver) LifecycleSupport() driver.LifecycleCapabilities {
	return driver.LifecycleCapabilities{Supported: false}
}
func (m *stubDriver) GetLifecycle(_ context.Context, _ string) ([]driver.LifecycleRule, error) {
	return nil, nil
}
func (m *stubDriver) PutLifecycle(_ context.Context, _ string, _ []driver.LifecycleRule) error {
	return nil
}

func (m *mockDriver) LifecycleSupport() driver.LifecycleCapabilities {
	return driver.LifecycleCapabilities{Supported: false}
}
func (m *mockDriver) GetLifecycle(_ context.Context, _ string) ([]driver.LifecycleRule, error) {
	return nil, nil
}
func (m *mockDriver) PutLifecycle(_ context.Context, _ string, _ []driver.LifecycleRule) error {
	return nil
}

func (m *fanoutDriver) LifecycleSupport() driver.LifecycleCapabilities {
	return driver.LifecycleCapabilities{Supported: false}
}
func (m *fanoutDriver) GetLifecycle(_ context.Context, _ string) ([]driver.LifecycleRule, error) {
	return nil, nil
}
func (m *fanoutDriver) PutLifecycle(_ context.Context, _ string, _ []driver.LifecycleRule) error {
	return nil
}

// v1.4.0a shims — most test mocks don't care about per-bucket stats;
// default to false. Tests that exercise the FE column-visibility path
// (e.g. region_resolver_test) override on a per-fixture basis.

func (m *layoutDriver) PerBucketStatsAvailable() bool { return false }
func (m *stubDriver) PerBucketStatsAvailable() bool   { return false }
func (m *mockDriver) PerBucketStatsAvailable() bool   { return false }
func (m *fanoutDriver) PerBucketStatsAvailable() bool { return false }

// v1.4.0c (SCRUB.MAINT) shims for every test-only driver. Default
// behaviour: report unsupported + return ErrUnsupported from the
// state/start methods. Tests that exercise the scrub handler
// (admin_scrub_test.go) override on a per-fixture basis.

func (m *layoutDriver) ScrubSupport() driver.ScrubCapability {
	return driver.ScrubCapability{Supported: false}
}
func (m *layoutDriver) ScrubState(_ context.Context) (driver.ScrubState, error) {
	return driver.ScrubState{}, driver.ErrUnsupported
}
func (m *layoutDriver) StartScrub(_ context.Context) error { return driver.ErrUnsupported }

func (m *stubDriver) ScrubSupport() driver.ScrubCapability {
	return driver.ScrubCapability{Supported: false}
}
func (m *stubDriver) ScrubState(_ context.Context) (driver.ScrubState, error) {
	return driver.ScrubState{}, driver.ErrUnsupported
}
func (m *stubDriver) StartScrub(_ context.Context) error { return driver.ErrUnsupported }

func (m *mockDriver) ScrubSupport() driver.ScrubCapability {
	return driver.ScrubCapability{Supported: false}
}
func (m *mockDriver) ScrubState(_ context.Context) (driver.ScrubState, error) {
	return driver.ScrubState{}, driver.ErrUnsupported
}
func (m *mockDriver) StartScrub(_ context.Context) error { return driver.ErrUnsupported }

func (m *fanoutDriver) ScrubSupport() driver.ScrubCapability {
	return driver.ScrubCapability{Supported: false}
}
func (m *fanoutDriver) ScrubState(_ context.Context) (driver.ScrubState, error) {
	return driver.ScrubState{}, driver.ErrUnsupported
}
func (m *fanoutDriver) StartScrub(_ context.Context) error { return driver.ErrUnsupported }

// v1.10.0a versioning shims — default behaviour: report unsupported.
// Tests that exercise the versioning handlers
// (user_bucket_versioning_test.go) use the regionMockDriver +
// testMockDriver pair which have overridable hooks; these shims only
// exist to keep the rest of the package compiling.

func (m *layoutDriver) VersioningSupport() bool { return false }
func (m *layoutDriver) GetVersioningStatus(_ context.Context, _ string) (driver.VersioningStatus, error) {
	return driver.VersioningDisabled, driver.ErrUnsupported
}
func (m *layoutDriver) EnableVersioning(_ context.Context, _ string) error {
	return driver.ErrUnsupported
}
func (m *layoutDriver) SuspendVersioning(_ context.Context, _ string) error {
	return driver.ErrUnsupported
}
func (m *layoutDriver) ListObjectVersions(_ context.Context, _, _, _ string, _ int) ([]driver.ObjectVersion, string, error) {
	return nil, "", driver.ErrUnsupported
}
func (m *layoutDriver) GetObjectVersion(_ context.Context, _, _, _ string) (driver.StreamResult, error) {
	return driver.StreamResult{}, driver.ErrUnsupported
}
func (m *layoutDriver) DeleteObjectVersion(_ context.Context, _, _, _ string) error {
	return driver.ErrUnsupported
}

func (m *stubDriver) VersioningSupport() bool { return false }
func (m *stubDriver) GetVersioningStatus(_ context.Context, _ string) (driver.VersioningStatus, error) {
	return driver.VersioningDisabled, driver.ErrUnsupported
}
func (m *stubDriver) EnableVersioning(_ context.Context, _ string) error {
	return driver.ErrUnsupported
}
func (m *stubDriver) SuspendVersioning(_ context.Context, _ string) error {
	return driver.ErrUnsupported
}
func (m *stubDriver) ListObjectVersions(_ context.Context, _, _, _ string, _ int) ([]driver.ObjectVersion, string, error) {
	return nil, "", driver.ErrUnsupported
}
func (m *stubDriver) GetObjectVersion(_ context.Context, _, _, _ string) (driver.StreamResult, error) {
	return driver.StreamResult{}, driver.ErrUnsupported
}
func (m *stubDriver) DeleteObjectVersion(_ context.Context, _, _, _ string) error {
	return driver.ErrUnsupported
}

func (m *mockDriver) VersioningSupport() bool { return false }
func (m *mockDriver) GetVersioningStatus(_ context.Context, _ string) (driver.VersioningStatus, error) {
	return driver.VersioningDisabled, driver.ErrUnsupported
}
func (m *mockDriver) EnableVersioning(_ context.Context, _ string) error {
	return driver.ErrUnsupported
}
func (m *mockDriver) SuspendVersioning(_ context.Context, _ string) error {
	return driver.ErrUnsupported
}
func (m *mockDriver) ListObjectVersions(_ context.Context, _, _, _ string, _ int) ([]driver.ObjectVersion, string, error) {
	return nil, "", driver.ErrUnsupported
}
func (m *mockDriver) GetObjectVersion(_ context.Context, _, _, _ string) (driver.StreamResult, error) {
	return driver.StreamResult{}, driver.ErrUnsupported
}
func (m *mockDriver) DeleteObjectVersion(_ context.Context, _, _, _ string) error {
	return driver.ErrUnsupported
}

func (m *fanoutDriver) VersioningSupport() bool { return false }
func (m *fanoutDriver) GetVersioningStatus(_ context.Context, _ string) (driver.VersioningStatus, error) {
	return driver.VersioningDisabled, driver.ErrUnsupported
}
func (m *fanoutDriver) EnableVersioning(_ context.Context, _ string) error {
	return driver.ErrUnsupported
}
func (m *fanoutDriver) SuspendVersioning(_ context.Context, _ string) error {
	return driver.ErrUnsupported
}
func (m *fanoutDriver) ListObjectVersions(_ context.Context, _, _, _ string, _ int) ([]driver.ObjectVersion, string, error) {
	return nil, "", driver.ErrUnsupported
}
func (m *fanoutDriver) GetObjectVersion(_ context.Context, _, _, _ string) (driver.StreamResult, error) {
	return driver.StreamResult{}, driver.ErrUnsupported
}
func (m *fanoutDriver) DeleteObjectVersion(_ context.Context, _, _, _ string) error {
	return driver.ErrUnsupported
}
