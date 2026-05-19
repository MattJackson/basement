package driver

import "errors"

var (
	ErrUnsupported      = errors.New("driver: operation not supported by this driver")
	ErrNotFound         = errors.New("driver: not found")
	ErrPermissionDenied = errors.New("driver: permission denied")
	ErrConflict         = errors.New("driver: conflict")
	ErrInvalid          = errors.New("driver: invalid input")
	ErrUnauthenticated  = errors.New("driver: not authenticated")
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
