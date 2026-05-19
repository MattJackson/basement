package garage_v1

import (
	"bytes"
	"context"
	"encoding/json"
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
		http: &http.Client{
			Timeout: 30 * time.Second,
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
//	5xx          -> raw "HTTP <code>: <body>" (no sentinel)
//
// The response body is preserved verbatim in *driver.Error.Message so callers
// can surface Garage's diagnostic text upstream.
func (c *client) do(ctx context.Context, method, path string, body, out any) error {
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
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// Mirror the v2 driver: transport-level failure maps to
		// ErrUnauthenticated (operator-visible "can't reach"/"no creds").
		return &driverpkg.Error{
			Op:      method,
			Driver:  driverName,
			Err:     driverpkg.ErrUnauthenticated,
			Message: fmt.Sprintf("HTTP request failed: %v", err),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
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
		if resp.StatusCode >= 500 {
			mappedErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
		} else {
			mappedErr = &driverpkg.Error{
				Driver:  driverName,
				Err:     driverpkg.ErrInvalid,
				Message: fmt.Sprintf("unexpected status: %d", resp.StatusCode),
			}
		}
	}

	return &driverpkg.Error{
		Op:      method,
		Driver:  driverName,
		Err:     mappedErr,
		Message: string(respBody),
	}
}
