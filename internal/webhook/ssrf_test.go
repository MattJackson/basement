package webhook

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestSafeWebhookClient_BlocksLoopback verifies the SSRF guard: the
// production webhook client must refuse to connect to a loopback /
// private address even though the target is reachable.
func TestSafeWebhookClient_BlocksLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newSafeWebhookClient(5 * time.Second)
	_, err := client.Get(srv.URL) // srv.URL is http://127.0.0.1:<port>
	if err == nil {
		t.Fatalf("expected the SSRF guard to block loopback %s, got nil error", srv.URL)
	}
	if !strings.Contains(err.Error(), "non-public address") {
		t.Errorf("expected a non-public-address block error, got: %v", err)
	}
}

// TestBlockedWebhookIP covers the IP classification used by the guard.
func TestBlockedWebhookIP(t *testing.T) {
	blocked := []string{"127.0.0.1", "::1", "10.0.0.5", "192.168.1.1", "172.16.0.1", "169.254.169.254", "fd00::1", "0.0.0.0"}
	for _, s := range blocked {
		if !blockedWebhookIP(net.ParseIP(s)) {
			t.Errorf("%s should be blocked", s)
		}
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946"}
	for _, s := range allowed {
		if blockedWebhookIP(net.ParseIP(s)) {
			t.Errorf("%s should be allowed (public)", s)
		}
	}
	if !blockedWebhookIP(nil) {
		t.Error("nil IP must be blocked")
	}
}
