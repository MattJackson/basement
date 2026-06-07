package garage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// client is the internal HTTP client for the Garage v2 admin API.
type client struct {
	baseURL string
	token   string
	http    *http.Client
}

// newClient creates a new Garage v2 admin client from config.
// Config keys:
//   - "admin_url": Garage admin URL (e.g., http://garage:3902)
//   - "admin_token": Bearer token for authentication
//   - "s3_endpoint": Optional S3 API endpoint (defaults to :3902 if not specified)
//
// security scheme: garage-admin-v2.json:5063-5074 (bearerAuth)
func newClient(cfg driverpkg.Config) *client {
	return &client{
		baseURL: cfg["admin_url"],
		token:   cfg["admin_token"],
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// do executes an HTTP request against the Garage v2 admin API. It handles
// JSON encoding of the request body, Bearer-token authentication, decoding
// of the response into out (if non-nil), and HTTP status -> driver sentinel
// error mapping:
//
//	401          -> ErrUnauthenticated
//	403          -> ErrPermissionDenied
//	404          -> ErrNotFound
//	409          -> ErrConflict
//	400, 405, 422-> ErrInvalid
//	5xx          -> "HTTP <code>" (no sentinel; body in Message)
//
// Transport-level failures (DNS, connection refused, TLS, timeout) map to
// ErrUnreachable; context cancellation/deadline are returned as-is.
func (c *client) do(ctx context.Context, method, path string, body, out any) error {
	if c.token == "" {
		return &driverpkg.Error{
			Op:      method,
			Driver:  driverName,
			Err:     driverpkg.ErrMissingAdminToken,
			Message: "Garage admin token is not configured for this cluster. Edit the cluster to provide it.",
		}
	}

	var reqBody io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return &driverpkg.Error{
				Op:      method,
				Driver:  driverName,
				Err:     driverpkg.ErrInvalid,
				Message: fmt.Sprintf("failed to marshal request body: %v", err),
			}
		}
		reqBody = bytes.NewReader(jsonBytes)
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return &driverpkg.Error{
			Op:      method,
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: fmt.Sprintf("failed to create request: %v", err),
		}
	}

	req.Header.Set("Content-Type", "application/json")
	// Token presence already validated at the top of do(); set unconditionally.
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return &driverpkg.Error{
			Op:      method,
			Driver:  driverName,
			Err:     transportErr(err),
			Message: fmt.Sprintf("HTTP request failed: %v", err),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	// Bound the admin response read so a malfunctioning/compromised backend
	// can't return an unbounded body and exhaust memory.
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxAdminRespBytes))
	if err != nil {
		return &driverpkg.Error{
			Op:      method,
			Driver:  driverName,
			Err:     driverpkg.ErrInvalid,
			Message: fmt.Sprintf("failed to read response body: %v", err),
		}
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(respBody) > 0 {
			if err := json.Unmarshal(respBody, out); err != nil {
				return &driverpkg.Error{
					Op:      method,
					Driver:  driverName,
					Err:     driverpkg.ErrInvalid,
					Message: fmt.Sprintf("failed to unmarshal response: %v", err),
				}
			}
		}
		return nil
	}

	var mappedErr error
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		mappedErr = driverpkg.ErrUnauthenticated
	case http.StatusForbidden:
		mappedErr = driverpkg.ErrPermissionDenied
	case http.StatusNotFound:
		mappedErr = driverpkg.ErrNotFound
	case http.StatusConflict:
		mappedErr = driverpkg.ErrConflict
	case http.StatusBadRequest, http.StatusMethodNotAllowed, http.StatusUnprocessableEntity:
		mappedErr = driverpkg.ErrInvalid
	default:
		// 5xx and other unexpected statuses: no sentinel. Don't embed the
		// raw backend body in BOTH Err and Message — keep the sentinel
		// generic ("HTTP <code>") and surface a single, truncated copy of
		// the body via Message below.
		mappedErr = fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return &driverpkg.Error{
		Op:      method,
		Driver:  driverName,
		Err:     mappedErr,
		Message: truncateBody(respBody),
	}
}

// transportErr maps a *http.Client.Do transport-level failure to a driver
// sentinel. Context cancellation/deadline are returned as-is so callers can
// distinguish caller-initiated abort/timeout; every other transport failure
// (DNS error, connection refused, TLS handshake failure, read timeout) maps to
// ErrUnreachable. ErrUnauthenticated is reserved for an actual HTTP 401/403
// (handled in the status switch), so a down/slow backend no longer masquerades
// as a bad admin token.
func transportErr(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return driverpkg.ErrUnreachable
}

// maxAdminRespBytes bounds how much of an admin API response body we read
// into memory (success decode + error surfacing).
const maxAdminRespBytes = 8 << 20 // 8 MiB

// truncateBody renders a (possibly large) backend response body for an error
// Message, capped so a verbose backend can't bloat the error surface.
func truncateBody(b []byte) string {
	const max = 512
	if len(b) > max {
		return string(b[:max]) + "…(truncated)"
	}
	return string(b)
}
