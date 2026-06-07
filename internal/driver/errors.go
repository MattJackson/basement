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
	// ErrMissingAdminToken is returned when a garage driver is used without admin_token configured.
	ErrMissingAdminToken = errors.New("driver: missing admin token for garage driver")
	// ErrUnreachable is returned when a transport-level failure (DNS error,
	// connection refused, TLS handshake failure, read/connect timeout)
	// prevents the request from reaching the backend at all. Distinct from
	// ErrUnauthenticated, which is reserved for an actual HTTP 401/403 from a
	// reachable backend — conflating the two makes a down/slow backend look
	// like a bad admin token and prompts a pointless credential re-entry.
	ErrUnreachable = errors.New("driver: backend unreachable")
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
