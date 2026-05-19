// Package garage implements the garage device driver.
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

// client is the internal HTTP client for Garage v2 admin API.
type client struct {
	baseURL string
	token   string
	http    *http.Client
}

// newClient creates a new Garage client from config.
// Config keys:
//   - "admin_url": Garage admin URL (e.g., http://garage:3903)
//   - "admin_token": Bearer token for authentication
func newClient(cfg driverpkg.Config) *client {
	baseURL := cfg["admin_url"]
	token := cfg["admin_token"]

	return &client{
		baseURL: baseURL,
		token:   token,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// do makes an HTTP request to the Garage admin API and decodes the response.
// It handles JSON encoding/decoding, Bearer token authentication, and error mapping.
func (c *client) do(ctx context.Context, method, path string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return &driverpkg.Error{
				Op:      method,
				Driver:  "garage",
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
			Driver:  "garage",
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
			Driver:  "garage",
			Err:     driverpkg.ErrUnauthenticated,
			Message: fmt.Sprintf("HTTP request failed: %v", err),
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &driverpkg.Error{
			Op:      method,
			Driver:  "garage",
			Err:     driverpkg.ErrInvalid,
			Message: fmt.Sprintf("failed to read response body: %v", err),
		}
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(respBody) > 0 {
			if err := json.Unmarshal(respBody, out); err != nil {
				return &driverpkg.Error{
					Op:      method,
					Driver:  "garage",
					Err:     driverpkg.ErrInvalid,
					Message: fmt.Sprintf("failed to unmarshal response: %v", err),
				}
			}
		}
		return nil
	}

	var mappedErr error
	switch resp.StatusCode {
	case 401:
		mappedErr = driverpkg.ErrUnauthenticated
	case 403:
		mappedErr = driverpkg.ErrPermissionDenied
	case 404:
		mappedErr = driverpkg.ErrNotFound
	case 409:
		mappedErr = driverpkg.ErrConflict
	case 400, 405, 422:
		mappedErr = driverpkg.ErrInvalid
	default:
		if resp.StatusCode >= 500 {
			mappedErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
		} else {
			mappedErr = &driverpkg.Error{
				Driver:  "garage",
				Err:     driverpkg.ErrInvalid,
				Message: fmt.Sprintf("unexpected status: %d", resp.StatusCode),
			}
		}
	}

	return &driverpkg.Error{
		Op:      method,
		Driver:  "garage",
		Err:     mappedErr,
		Message: string(respBody),
	}
}
