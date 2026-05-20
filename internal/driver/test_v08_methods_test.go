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
