package garage

import (
	"encoding/json"
	"testing"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

func TestLifecycleSupport_ReportsExpirationOnly(t *testing.T) {
	d := &driver{}
	caps := d.LifecycleSupport()
	if !caps.Supported {
		t.Fatalf("expected Supported=true for Garage v2")
	}
	if !caps.Expiration {
		t.Fatalf("expected Expiration=true")
	}
	if caps.Transition {
		t.Fatalf("expected Transition=false (Garage has no tiers)")
	}
	if caps.NoncurrentDays {
		t.Fatalf("expected NoncurrentDays=false")
	}
	if !caps.AbortMultipartDays {
		t.Fatalf("expected AbortMultipartDays=true")
	}
	if len(caps.TransitionTiers) != 0 {
		t.Fatalf("expected empty TransitionTiers, got %v", caps.TransitionTiers)
	}
}

// TestRuleRoundTrip ensures driverRuleToGarage emits the XML-shaped
// keys Garage expects (capitalised "ID"/"Status"/"Days") and that
// the unmarshalled response from a hypothetical GetBucketInfo
// flattens back into the driver shape.
func TestRuleRoundTrip(t *testing.T) {
	exp := 30
	abort := 7
	in := driverpkg.LifecycleRule{
		ID:                 "rule-1",
		Status:             "Enabled",
		Prefix:             "logs/",
		ExpirationDays:     &exp,
		AbortMultipartDays: &abort,
		// These three should be silently dropped on the way out (Garage
		// doesn't support them) — the round-trip should NOT show them.
		TransitionDays: &abort,
		TransitionTier: "GLACIER",
		NoncurrentDays: &abort,
	}
	gr := driverRuleToGarage(in)

	// JSON encode then decode to mimic the wire round-trip.
	bs, err := json.Marshal(gr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Verify the XML-cased field names are present.
	if !contains(bs, `"ID":"rule-1"`) || !contains(bs, `"Status":"Enabled"`) ||
		!contains(bs, `"Days":30`) || !contains(bs, `"DaysAfterInitiation":7`) ||
		!contains(bs, `"Prefix":"logs/"`) {
		t.Fatalf("unexpected wire shape: %s", bs)
	}

	var decoded garageLifecycleRule
	if err := json.Unmarshal(bs, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	back := garageRuleToDriver(decoded)
	if back.ID != "rule-1" || back.Status != "Enabled" || back.Prefix != "logs/" {
		t.Fatalf("scalars lost: %+v", back)
	}
	if back.ExpirationDays == nil || *back.ExpirationDays != 30 {
		t.Fatalf("expiration lost: %v", back.ExpirationDays)
	}
	if back.AbortMultipartDays == nil || *back.AbortMultipartDays != 7 {
		t.Fatalf("abort lost: %v", back.AbortMultipartDays)
	}
	// Transition / Noncurrent fields stay nil — Garage doesn't surface them.
	if back.TransitionDays != nil || back.TransitionTier != "" || back.NoncurrentDays != nil {
		t.Fatalf("unsupported fields leaked through: %+v", back)
	}
}

func contains(haystack []byte, needle string) bool {
	return indexOf(string(haystack), needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
