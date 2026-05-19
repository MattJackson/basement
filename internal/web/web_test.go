package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler_GET_root_returns_index_html(t *testing.T) {
	handler := Handler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("expected Content-Type to contain 'text/html', got %q", contentType)
	}

	body := w.Body.String()
	if !strings.Contains(body, "basement") {
		t.Errorf("expected body to contain 'basement', got %q", body)
	}
}

func TestHandler_GET_any_path_returns_index_html(t *testing.T) {
	handler := Handler()
	req := httptest.NewRequest(http.MethodGet, "/anywhere/else", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "basement") {
		t.Errorf("expected body to contain 'basement', got %q", body)
	}
}

func TestHandler_GET_assets_missing_returns_404(t *testing.T) {
	handler := Handler()
	req := httptest.NewRequest(http.MethodGet, "/assets/notthere.js", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestHandler_POST_rejected(t *testing.T) {
	handler := Handler()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}
