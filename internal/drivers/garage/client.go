package garage

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
//	5xx          -> raw "HTTP <code>: <body>" (no sentinel)
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
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
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
