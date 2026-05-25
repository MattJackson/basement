package docs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleDocs_PathVariants(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		wantStatus int
		wantBody   string
	}{
		{"bare path", "/docs/integrations/webdav", http.StatusOK, "WebDAV"},
		{"with .md suffix", "/docs/integrations/webdav.md", http.StatusOK, "WebDAV"},
		{"with trailing slash", "/docs/integrations/webdav/", http.StatusOK, "WebDAV"},
		{"missing doc", "/docs/integrations/does-not-exist", http.StatusNotFound, ""},
		{"missing doc with .md", "/docs/integrations/does-not-exist.md", http.StatusNotFound, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rr := httptest.NewRecorder()
			HandleDocs(rr, req)

			if rr.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rr.Code, tc.wantStatus, rr.Body.String())
			}
			if tc.wantBody != "" && !strings.Contains(rr.Body.String(), tc.wantBody) {
				t.Errorf("body does not contain %q", tc.wantBody)
			}
		})
	}
}

func TestHandleDocs_RootRedirect(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/docs/", nil)
	rr := httptest.NewRecorder()
	HandleDocs(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 SeeOther", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "/docs/") {
		t.Errorf("Location = %q, want a /docs/ redirect", loc)
	}
}

func TestRenderMarkdown_ExistingFile(t *testing.T) {
	html, err := RenderMarkdown("integrations/webdav")
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if !strings.Contains(html, "<h1") {
		t.Errorf("rendered HTML missing <h1>; got first 200 chars: %s", html[:min(200, len(html))])
	}
	if !strings.Contains(html, "WebDAV") {
		t.Errorf("rendered HTML missing 'WebDAV'")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
