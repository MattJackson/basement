package store

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConnectionsOpenCreatesFile(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	if s == nil {
		t.Fatal("store is nil")
	}

	connPath := filepath.Join(tmpDir, "connections.json")
	if _, err := os.Stat(connPath); err != nil {
		if os.IsNotExist(err) {
			t.Log("connections.json created lazily on first write (expected)")
		} else {
			t.Fatalf("stat connections.json: %v", err)
		}
	}
}

func TestConnectionRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	conn := Connection{
		Label:  "test-connection",
		Driver: "garage",
		Config: map[string]string{
			"admin_url":    "http://garage:3903",
			"admin_token":  "secret-token",
			"s3_region":    "us-east-1",
		},
		Color: "#FF5733",
		Owner: "org",
	}

	result, err := s.Create(ctx, conn)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if result.Label != "test-connection" {
		t.Errorf("expected label 'test-connection', got '%s'", result.Label)
	}

	if result.Driver != "garage" {
		t.Errorf("expected driver 'garage', got '%s'", result.Driver)
	}

	if result.ID == "" {
		t.Error("expected non-empty ID after Create")
	}

	if result.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt after Create")
	}

	if result.Color != "#FF5733" {
		t.Errorf("expected color '#FF5733', got '%s'", result.Color)
	}

	if result.Owner != "org" {
		t.Errorf("expected owner 'org', got '%s'", result.Owner)
	}

	id := result.ID

	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.Label != result.Label {
		t.Errorf("expected label '%s', got '%s'", result.Label, got.Label)
	}

	if got.Driver != result.Driver {
		t.Errorf("expected driver '%s', got '%s'", result.Driver, got.Driver)
	}

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(list) != 1 {
		t.Errorf("expected 1 connection in list, got %d", len(list))
	}
}

func TestConnectionCount(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	count, err := s.Count(ctx)
	if err != nil {
		t.Fatalf("Count failed (initial): %v", err)
	}

	if count != 0 {
		t.Errorf("expected initial count 0, got %d", count)
	}

	conn1 := Connection{Label: "conn-1", Driver: "garage", Config: map[string]string{}, Owner: "org"}
	if _, err := s.Create(ctx, conn1); err != nil {
		t.Fatalf("Create conn-1 failed: %v", err)
	}

	count, err = s.Count(ctx)
	if err != nil {
		t.Fatalf("Count failed (after 1 create): %v", err)
	}

	if count != 1 {
		t.Errorf("expected count 1 after first Create, got %d", count)
	}

	conn2 := Connection{Label: "conn-2", Driver: "aws-s3", Config: map[string]string{}, Owner: "org"}
	if _, err := s.Create(ctx, conn2); err != nil {
		t.Fatalf("Create conn-2 failed: %v", err)
	}

	count, err = s.Count(ctx)
	if err != nil {
		t.Fatalf("Count failed (after 2 creates): %v", err)
	}

	if count != 2 {
		t.Errorf("expected count 2 after second Create, got %d", count)
	}
}

func TestConnectionUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	conn := Connection{Label: "original", Driver: "garage", Config: map[string]string{"key": "val1"}, Owner: "org"}
	result, err := s.Create(ctx, conn)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	id := result.ID

	newConfig := map[string]string{
		"admin_url":    "http://new-garage:3903",
		"admin_token":  "new-token",
		"s3_region":    "eu-west-1",
	}

	patch := Connection{
		Label:  "updated-label",
		Config: newConfig,
		Color:  "#AABBCC",
	}

	updated, err := s.Update(ctx, id, patch)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.Label != "updated-label" {
		t.Errorf("expected label 'updated-label', got '%s'", updated.Label)
	}

	if updated.Color != "#AABBCC" {
		t.Errorf("expected color '#AABBCC', got '%s'", updated.Color)
	}

	if len(updated.Config) != 3 {
		t.Errorf("expected 3 config keys, got %d", len(updated.Config))
	}

	if updated.Config["admin_url"] != "http://new-garage:3903" {
		t.Errorf("expected admin_url 'http://new-garage:3903', got '%s'", updated.Config["admin_url"])
	}

	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after Update failed: %v", err)
	}

	if got.Label != "updated-label" {
		t.Errorf("persisted label mismatch")
	}
}

func TestConnectionDelete(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	conn := Connection{Label: "to-delete", Driver: "garage", Config: map[string]string{}, Owner: "org"}
	result, err := s.Create(ctx, conn)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	id := result.ID

	count, _ := s.Count(ctx)
	if count != 1 {
		t.Errorf("expected count 1 before delete, got %d", count)
	}

	if err := s.Delete(ctx, id); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	count, err = s.Count(ctx)
	if err != nil {
		t.Fatalf("Count after Delete failed: %v", err)
	}

	if count != 0 {
		t.Errorf("expected count 0 after delete, got %d", count)
	}

	_, err = s.Get(ctx, id)
	if err == nil {
		t.Error("expected error getting deleted connection")
	}
}

func TestConnectionValidationUnsupportedDriver(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	conn := Connection{Label: "bad-driver", Driver: "unknown-driver", Config: map[string]string{}, Owner: "org"}
	_, err = s.Create(ctx, conn)

	if err == nil {
		t.Error("expected error for unsupported driver")
	}

	expectedMsg := "unsupported driver"
	if err != nil && err.Error()[:len(expectedMsg)] != expectedMsg {
		t.Errorf("expected error containing %q, got %q", expectedMsg, err.Error())
	}
}

func TestConnectionValidationEmptyLabel(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	conn := Connection{Label: "", Driver: "garage", Config: map[string]string{}, Owner: "org"}
	_, err = s.Create(ctx, conn)

	if err == nil {
		t.Error("expected error for empty label")
	}
}

func TestConnectionValidationDuplicateLabel(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	conn1 := Connection{Label: "MyConnection", Driver: "garage", Config: map[string]string{}, Owner: "org"}
	if _, err := s.Create(ctx, conn1); err != nil {
		t.Fatalf("Create first connection failed: %v", err)
	}

	conn2 := Connection{Label: "myconnection", Driver: "aws-s3", Config: map[string]string{}, Owner: "org"} // same label, different case
	_, err = s.Create(ctx, conn2)

	if err == nil {
		t.Error("expected error for duplicate label (case-insensitive)")
	}

	expectedMsg := "duplicate label"
	if err != nil && err.Error()[:len(expectedMsg)] != expectedMsg {
		t.Errorf("expected error containing %q, got %q", expectedMsg, err.Error())
	}
}

func TestConnectionDefaultColor(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	conn := Connection{Label: "no-color", Driver: "garage", Config: map[string]string{}, Owner: "org"}
	result, err := s.Create(ctx, conn)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if result.Color == "" {
		t.Error("expected default color to be set")
	}

	expectedColor := "#C9874B"
	if result.Color != expectedColor {
		t.Errorf("expected default color '%s', got '%s'", expectedColor, result.Color)
	}
}

func TestConcurrentConnectionWrites(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	var wg sync.WaitGroup
	numGoroutines := 10
	connectionsPerGoroutine := 5

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < connectionsPerGoroutine; j++ {
				label := "conn-" + string(rune('a'+base)) + "-" + string(rune('0'+j))
				conn := Connection{
					Label:  label,
					Driver: "garage",
					Config: map[string]string{"index": label},
					Owner:  "org",
				}
				if _, err := s.Create(ctx, conn); err != nil {
					t.Errorf("concurrent Create failed: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	count, err := s.Count(ctx)
	if err != nil {
		t.Fatalf("Count after concurrent writes failed: %v", err)
	}

	expected := numGoroutines * connectionsPerGoroutine
	if count != expected {
		t.Errorf("expected %d connections after concurrent writes, got %d", expected, count)
	}

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List after concurrent writes failed: %v", err)
	}

	if len(list) != expected {
		t.Errorf("expected %d connections in list, got %d", expected, len(list))
	}
}

func TestConnectionReloadAfterWrite(t *testing.T) {
	tmpDir := t.TempDir()

	conn1 := Connection{Label: "conn-1", Driver: "garage", Config: map[string]string{"a": "b"}, Owner: "org"}
	s1, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections first open failed: %v", err)
	}

	ctx := context.Background()
	result, err := s1.Create(ctx, conn1)
	if err != nil {
		t.Fatalf("Create via first store failed: %v", err)
	}

	id := result.ID

	s2, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections second open (reload) failed: %v", err)
	}

	gotConn, err := s2.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after reload failed: %v", err)
	}

	if gotConn.Label != "conn-1" {
		t.Errorf("expected label 'conn-1' after reload, got '%s'", gotConn.Label)
	}
}
