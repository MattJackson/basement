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
