package driver

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRegistry(t *testing.T) {
	tests := []struct {
		name        string
		setup       func()
		expectPanic bool
		expectErr   bool
		errContains string
		result      Driver
	}{
		{
			name: "open unknown driver",
			setup: func() {
				// no registration
			},
			expectPanic: false,
			expectErr:   true,
			errContains: `unknown`,
			result:      nil,
		},
		{
			name: "open valid registered driver",
			setup: func() {
				Register("testdriver", func(_ Config) (Driver, error) {
					return &mockDriver{}, nil
				})
			},
			expectPanic: false,
			expectErr:   false,
			errContains: "",
			result:      &mockDriver{},
		},
		{
			name: "register empty name panics",
			setup: func() {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("Register(\"\") did not panic")
					}
				}()
				Register("", func(_ Config) (Driver, error) {
					return &mockDriver{}, nil
				})
			},
			expectPanic: true,
			expectErr:   false,
			errContains: "",
			result:      nil,
		},
		{
			name: "register duplicate panics",
			setup: func() {
				Register("dupdriver", func(_ Config) (Driver, error) {
					return &mockDriver{}, nil
				})
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("Register(\"dupdriver\") a second time did not panic")
					}
				}()
				Register("dupdriver", func(_ Config) (Driver, error) {
					return &mockDriver{}, nil
				})
			},
			expectPanic: true,
			expectErr:   false,
			errContains: "",
			result:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset registry state before each test
			mu.Lock()
			factories = make(map[string]Factory)
			mu.Unlock()

			tt.setup()

			if tt.expectPanic {
				return // panic expected and caught by defer in setup
			}

			driver, err := Open("testdriver", nil)
			if tt.expectErr && err == nil {
				t.Errorf("expected error but got none")
				return
			}

			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.expectErr && tt.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %v", tt.errContains, err)
				}
			}

			if !tt.expectPanic && tt.result != nil && driver != tt.result {
				t.Errorf("expected driver %T, got %T", tt.result, driver)
			}
		})
	}
}

func TestRegistryRegistered(t *testing.T) {
	tests := []struct {
		name       string
		setup      func()
		expectLen  int
		expectSorted bool
	}{
		{
			name: "empty registry returns empty list",
			setup: func() {},
			expectLen: 0,
			expectSorted: true,
		},
		{
			name: "registered drivers returned sorted",
			setup: func() {
				Register("zebra", func(_ Config) (Driver, error) { return &mockDriver{}, nil })
				Register("alpha", func(_ Config) (Driver, error) { return &mockDriver{}, nil })
				Register("middle", func(_ Config) (Driver, error) { return &mockDriver{}, nil })
			},
			expectLen:  3,
			expectSorted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mu.Lock()
			factories = make(map[string]Factory)
			mu.Unlock()

			tt.setup()

			names := Registered()

			if len(names) != tt.expectLen {
				t.Errorf("expected %d registered drivers, got %d", tt.expectLen, len(names))
			}

			if tt.expectSorted {
				for i := 1; i < len(names); i++ {
					if names[i-1] >= names[i] {
						t.Errorf("registered names not sorted: %q >= %q at index %d", names[i-1], names[i], i)
					}
				}
			}
		})
	}
}

func TestErrorIs(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		target   error
		expected bool
	}{
		{
			name:     "ErrUnsupported matches",
			err:      &Error{Op: "Test", Driver: "test", Err: ErrUnsupported, Message: "not supported"},
			target:   ErrUnsupported,
			expected: true,
		},
		{
			name:     "ErrNotFound matches",
			err:      &Error{Op: "GetBucket", Driver: "garage", Err: ErrNotFound, Message: "bucket not found"},
			target:   ErrNotFound,
			expected: true,
		},
		{
			name:     "ErrPermissionDenied matches",
			err:      &Error{Op: "ListBuckets", Driver: "garage", Err: ErrPermissionDenied, Message: "access denied"},
			target:   ErrPermissionDenied,
			expected: true,
		},
		{
			name:     "ErrConflict matches",
			err:      &Error{Op: "CreateBucket", Driver: "garage", Err: ErrConflict, Message: "bucket exists"},
			target:   ErrConflict,
			expected: true,
		},
		{
			name:     "ErrInvalid matches",
			err:      &Error{Op: "UpdateKey", Driver: "garage", Err: ErrInvalid, Message: "invalid key ID"},
			target:   ErrInvalid,
			expected: true,
		},
		{
			name:     "ErrUnauthenticated matches",
			err:      &Error{Op: "DeleteObject", Driver: "garage", Err: ErrUnauthenticated, Message: "not authenticated"},
			target:   ErrUnauthenticated,
			expected: true,
		},
		{
			name:     "wrong sentinel does not match",
			err:      &Error{Op: "Test", Driver: "test", Err: ErrNotFound, Message: "not found"},
			target:   ErrUnsupported,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := errors.Is(tt.err, tt.target)
			if result != tt.expected {
				t.Errorf("errors.Is(%v, %v) = %v, want %v", tt.err, tt.target, result, tt.expected)
			}
		})
	}
}

func TestErrorUnwrap(t *testing.T) {
	err := &Error{Op: "Test", Driver: "test", Err: ErrUnsupported, Message: "not supported"}
	unwrapped := err.Unwrap()

	if unwrapped != ErrUnsupported {
		t.Errorf("unwrap returned %v, want %v", unwrapped, ErrUnsupported)
	}
}

func TestErrorFormat(t *testing.T) {
	err := &Error{Op: "ListBuckets", Driver: "garage", Err: ErrNotFound, Message: "bucket 'foo' not found"}
	expected := `driver(garage).ListBuckets: bucket 'foo' not found: driver: not found`

	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

// mockDriver implements Driver for testing purposes.
type mockDriver struct{}

func (m *mockDriver) Capabilities(_ context.Context) (Caps, error) { return Caps{}, nil }
func (m *mockDriver) HealthCheck(_ context.Context) (HealthReport, error) { return HealthReport{}, nil }
func (m *mockDriver) ListNodes(_ context.Context) ([]Node, error) { return nil, nil }
func (m *mockDriver) GetLayout(_ context.Context) (Layout, error) { return Layout{}, nil }
func (m *mockDriver) StageLayout(_ context.Context, _ LayoutChange) (LayoutDiff, error) { return LayoutDiff{}, nil }
func (m *mockDriver) ApplyLayout(_ context.Context) error { return nil }
func (m *mockDriver) RevertLayout(_ context.Context) error { return nil }
func (m *mockDriver) ListBuckets(_ context.Context) ([]Bucket, error) { return nil, nil }
func (m *mockDriver) GetBucket(_ context.Context, _ string) (Bucket, error) { return Bucket{}, nil }
func (m *mockDriver) CreateBucket(_ context.Context, _ BucketSpec) (Bucket, error) { return Bucket{}, nil }
func (m *mockDriver) UpdateBucket(_ context.Context, _ string, _ BucketUpdate) (Bucket, error) { return Bucket{}, nil }
func (m *mockDriver) DeleteBucket(_ context.Context, _ string) error { return nil }
func (m *mockDriver) ListKeys(_ context.Context) ([]Key, error) { return nil, nil }
func (m *mockDriver) GetKey(_ context.Context, _ string) (Key, error) { return Key{}, nil }
func (m *mockDriver) CreateKey(_ context.Context, _ KeySpec) (Key, error) { return Key{}, nil }
func (m *mockDriver) UpdateKeyPermissions(_ context.Context, _ string, _ []BucketPermission) error { return nil }
func (m *mockDriver) DeleteKey(_ context.Context, _ string) error { return nil }
func (m *mockDriver) ListObjects(_ context.Context, _, _, _ string, _ int) (ObjectPage, error) { return ObjectPage{}, nil }
func (m *mockDriver) StatObject(_ context.Context, _, _ string) (ObjectInfo, error) { return ObjectInfo{}, nil }
func (m *mockDriver) PresignGet(_ context.Context, _, _ string, _ time.Duration) (PresignedURL, error) { return PresignedURL{}, nil }
func (m *mockDriver) PresignPut(_ context.Context, _, _ string, _ time.Duration, _ string) (PresignedURL, error) { return PresignedURL{}, nil }
func (m *mockDriver) DeleteObject(_ context.Context, _, _ string) error { return nil }
func (m *mockDriver) CreateMultipart(_ context.Context, _, _, _ string) (MultipartUpload, error) { return MultipartUpload{}, nil }
func (m *mockDriver) PresignUploadPart(_ context.Context, _ MultipartUpload, _ int) (PresignedURL, error) { return PresignedURL{}, nil }
func (m *mockDriver) CompleteMultipart(_ context.Context, _ MultipartUpload, _ []CompletedPart) error { return nil }
func (m *mockDriver) AbortMultipart(_ context.Context, _ MultipartUpload) error { return nil }
