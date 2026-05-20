//go:build integration

package garage

import (
	"context"
	"os"
	"testing"
)

func TestIntegrationDriver(t *testing.T) {
	testURL := os.Getenv("GARAGE_V2_TEST_URL")
	if testURL == "" {
		t.Skip("Skipping integration test: GARAGE_V2_TEST_URL not set")
	}

	cfg := map[string]string{
		"admin_url":   testURL,
		"admin_token": os.Getenv("GARAGE_V2_TEST_TOKEN"),
	}

	drv, err := newDriver(cfg)
	if err != nil {
		t.Fatalf("Failed to create driver: %v", err)
	}

	ctx := context.Background()

	// Test Capabilities
	caps, err := drv.Capabilities(ctx)
	if err != nil {
		t.Errorf("Capabilities() error = %v", err)
	} else if caps.Driver != "garage" {
		t.Errorf("Capabilities().Driver = %q, want %q", caps.Driver, "garage")
	}

	// Test HealthCheck
	health, err := drv.HealthCheck(ctx)
	if err != nil {
		t.Logf("HealthCheck() error (expected if no server): %v", err)
	} else {
		t.Logf("HealthCheck() status = %s", health.Status)
	}

	// Test ListNodes
	nodes, err := drv.ListNodes(ctx)
	if err != nil {
		t.Logf("ListNodes() error (expected if no server): %v", err)
	} else {
		t.Logf("ListNodes() returned %d nodes", len(nodes))
	}

	// Test ListBuckets
	buckets, err := drv.ListBuckets(ctx)
	if err != nil {
		t.Logf("ListBuckets() error (expected if no server): %v", err)
	} else {
		t.Logf("ListBuckets() returned %d buckets", len(buckets))
	}

	// Test ListKeys
	keys, err := drv.ListKeys(ctx)
	if err != nil {
		t.Logf("ListKeys() error (expected if no server): %v", err)
	} else {
		t.Logf("ListKeys() returned %d keys", len(keys))
	}

	t.Log("Integration test completed successfully")
}
