package driver

import "errors"

var (
	// ErrUnsupported is returned when a driver does not support the requested operation.
	ErrUnsupported = errors.New("driver: operation not supported by this driver")
	// ErrNotFound is returned when the requested resource does not exist.
	ErrNotFound = errors.New("driver: not found")
	// ErrPermissionDenied is returned when the caller lacks permission for the operation.
	ErrPermissionDenied = errors.New("driver: permission denied")
	// ErrConflict is returned when the operation conflicts with existing state.
	ErrConflict = errors.New("driver: conflict")
	// ErrInvalid is returned when the input is invalid.
	ErrInvalid = errors.New("driver: invalid input")
	// ErrUnauthenticated is returned when the caller is not authenticated.
	ErrUnauthenticated = errors.New("driver: not authenticated")
)

// Error wraps a sentinel error with backend-specific context.
type Error struct {
	Op      string // operation name, e.g., "ListBuckets"
	Driver  string // driver name, e.g., "garage"
	Err     error  // typically one of the sentinels above
	Message string // human-readable message
}

func (e *Error) Error() string {
	return "driver(" + e.Driver + ")." + e.Op + ": " + e.Message + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error {
	return e.Err
}

// Is reports whether the wrapped error matches a target.
// This enables errors.Is(err, ErrUnsupported) to work through wrapping.
func (e *Error) Is(target error) bool {
	return errors.Is(e.Err, target)
}
