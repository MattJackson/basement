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
