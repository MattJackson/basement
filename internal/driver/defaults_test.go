package driver

import (
	"testing"
)

// TestDefaultsHasEntryPerRegisteredDriver checks that every registered
// driver name resolves to a non-empty EndpointDefaults entry — either
// from the curated table or via the fallback stub.
func TestDefaultsHasEntryPerRegisteredDriver(t *testing.T) {
	defaults := Defaults()
	registered := Registered()

	if len(defaults) != len(registered) {
		t.Fatalf("Defaults() returned %d entries, want %d (one per registered driver)",
			len(defaults), len(registered))
	}

	seen := make(map[string]bool, len(defaults))
	for _, d := range defaults {
		if d.Driver == "" {
			t.Error("Defaults entry has empty Driver")
		}
		if d.DisplayName == "" {
			t.Errorf("Defaults entry for %q has empty DisplayName", d.Driver)
		}
		seen[d.Driver] = true
	}

	for _, name := range registered {
		if !seen[name] {
			t.Errorf("registered driver %q missing from Defaults()", name)
		}
	}
}

// TestDefaultsCuratedEntries asserts the v1.3.0b shape for the four
// drivers basement ships today. If a driver is dropped from the
// curated table or its hints change, this test is the canary —
// regenerate after intentional copy edits.
func TestDefaultsCuratedEntries(t *testing.T) {
	byName := make(map[string]EndpointDefaults, len(defaultsTable))
	for _, d := range defaultsTable {
		byName[d.Driver] = d
	}

	cases := []struct {
		driver        string
		wantDisplay   string
		wantAdminURL  string
		wantS3        string
		wantRegion    string
		wantSecretURL bool
	}{
		{"garage-v1", "Garage v1", "http://garage-host:3903", "http://garage-host:3902", "garage", false},
		{"garage", "Garage v2", "http://garage-host:3903", "http://garage-host:3902", "garage", false},
		{"aws-s3", "AWS S3", "", "https://s3.us-east-1.amazonaws.com", "us-east-1", true},
		{"minio", "MinIO / OpenMaxIO", "http://minio-host:9001", "http://minio-host:9000", "us-east-1", false},
	}

	for _, tc := range cases {
		d, ok := byName[tc.driver]
		if !ok {
			t.Errorf("curated defaults missing entry for %q", tc.driver)
			continue
		}
		if d.DisplayName != tc.wantDisplay {
			t.Errorf("%s DisplayName=%q want %q", tc.driver, d.DisplayName, tc.wantDisplay)
		}
		if d.AdminURL != tc.wantAdminURL {
			t.Errorf("%s AdminURL=%q want %q", tc.driver, d.AdminURL, tc.wantAdminURL)
		}
		if d.S3Endpoint != tc.wantS3 {
			t.Errorf("%s S3Endpoint=%q want %q", tc.driver, d.S3Endpoint, tc.wantS3)
		}
		if d.RegionLabel != tc.wantRegion {
			t.Errorf("%s RegionLabel=%q want %q", tc.driver, d.RegionLabel, tc.wantRegion)
		}
		if (d.SecretURL != "") != tc.wantSecretURL {
			t.Errorf("%s SecretURL presence=%v want %v (got %q)",
				tc.driver, d.SecretURL != "", tc.wantSecretURL, d.SecretURL)
		}
		if d.AdminURLHint == "" {
			t.Errorf("%s AdminURLHint must not be empty", tc.driver)
		}
		if d.S3EndpointHint == "" {
			t.Errorf("%s S3EndpointHint must not be empty", tc.driver)
		}
	}
}

// TestDefaultsFallbackForUnknownDriver covers the stub path: a driver
// registered without a curated entry still appears in the output with
// at least the name surfaced as DisplayName.
func TestDefaultsFallbackForUnknownDriver(t *testing.T) {
	// Use a unique name so this test is parallel-safe across the
	// package's many Register() calls in other tests.
	const name = "test-defaults-fallback"
	Register(name, func(_ Config) (Driver, error) { return &mockDriver{}, nil })

	defaults := Defaults()
	var found *EndpointDefaults
	for i := range defaults {
		if defaults[i].Driver == name {
			found = &defaults[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("fallback driver %q not present in Defaults() output", name)
	}
	if found.DisplayName != name {
		t.Errorf("fallback DisplayName=%q want %q", found.DisplayName, name)
	}
	// Hints + URLs intentionally empty on the fallback path.
	if found.AdminURL != "" || found.S3Endpoint != "" {
		t.Errorf("fallback entry has unexpected URLs: %+v", *found)
	}
}
