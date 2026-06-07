package garage_v1

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

// client is the internal HTTP client for the Garage v1 admin API. It mirrors
// the v2 driver's client (internal/drivers/garage/client.go) but is kept in
// its own package so the two driver generations stay independent.
type client struct {
	baseURL string
	token   string
	http    *http.Client
}

// newClient creates a new Garage v1 admin client from config.
// Config keys:
//   - "admin_url": Garage admin URL (e.g., http://garage:3903)
//   - "admin_token": Bearer token for authentication
//
// security scheme: garage-admin-v1.yml:1101-1104 (bearerAuth)
func newClient(cfg driverpkg.Config) *client {
	return &client{
		baseURL: cfg["admin_url"],
		token:   cfg["admin_token"],
		// 10s ceiling: any single Garage v1 admin call that takes
		// longer than this is something we should fail fast on. The
		// admin endpoints typically respond in <100ms. /v1/health is
		// the slowest because it pings the whole cluster, but 10s is
		// plenty even for a slow multi-node setup. Was 30s — held
		// the request goroutine too long when /v1/health stalled,
		// blocking the server under concurrent _test polls.
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// do executes an HTTP request against the Garage v1 admin API. It handles
// JSON encoding of the request body, Bearer-token authentication, decoding
// of the response into out (if non-nil), and HTTP status -> driver sentinel
// error mapping identical to the v2 driver:
//
//	401          -> ErrUnauthenticated
//	403          -> ErrPermissionDenied
//	404          -> ErrNotFound
//	409          -> ErrConflict
//	400, 405, 422-> ErrInvalid
//	5xx          -> "HTTP <code>" (no sentinel; body in Message)
//
// Transport-level failures (DNS, connection refused, TLS, timeout) map to
// ErrUnreachable; context cancellation/deadline are returned as-is. The
// (truncated) response body is surfaced in *driver.Error.Message so callers
// can show Garage's diagnostic text upstream.
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
	// Token presence already validated at the top of do(); set unconditionally
	// (mirrors the v2 driver — the two do() implementations are kept identical).
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		// Transport-level failure: context cancel/deadline are returned
		// as-is; other transport errors (DNS, connection refused, TLS,
		// timeout) map to ErrUnreachable. ErrUnauthenticated is reserved
		// for an actual HTTP 401/403 below, so a down/slow backend no
		// longer masquerades as a bad admin token. (Mirrors v2 driver.)
		return &driverpkg.Error{
			Op:      method,
			Driver:  driverName,
			Err:     transportErr(err),
			Message: fmt.Sprintf("HTTP request failed: %v", err),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	// Bound the admin response read so a malfunctioning/compromised backend
	// can't return an unbounded body and exhaust memory (mirrors v2 driver).
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
		// 5xx and other unexpected statuses: no sentinel. Keep the wrapped
		// error generic ("HTTP <code>") and surface a single, truncated copy
		// of the body via Message — don't embed the (now bounded but still
		// potentially large) backend body into BOTH Err and Message, which
		// bloats error strings that reach logging. (Mirrors v2 driver.)
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
// sentinel. Context cancellation/deadline are returned as-is; every other
// transport failure (DNS error, connection refused, TLS handshake failure,
// read timeout) maps to ErrUnreachable. ErrUnauthenticated is reserved for an
// actual HTTP 401/403. (Identical to the v2 driver's transportErr.)
func transportErr(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return driverpkg.ErrUnreachable
}

// maxAdminRespBytes bounds how much of an admin API response body we read
// into memory (success decode + error surfacing). Identical to the v2 driver.
const maxAdminRespBytes = 8 << 20 // 8 MiB

// truncateBody renders a (possibly large) backend response body for an error
// Message, capped so a verbose backend can't bloat the error surface.
// Identical to the v2 driver.
func truncateBody(b []byte) string {
	const max = 512
	if len(b) > max {
		return string(b[:max]) + "…(truncated)"
	}
	return string(b)
}
