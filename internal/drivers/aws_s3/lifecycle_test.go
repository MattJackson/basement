package aws_s3

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

func TestLifecycleSupport_ReportsFullSurface(t *testing.T) {
	d := &driver{}
	caps := d.LifecycleSupport()
	if !caps.Supported {
		t.Fatalf("expected Supported=true for aws-s3")
	}
	if !caps.Expiration || !caps.Transition || !caps.NoncurrentDays || !caps.AbortMultipartDays {
		t.Fatalf("expected all sub-axes true, got %+v", caps)
	}
	if len(caps.TransitionTiers) == 0 {
		t.Fatalf("expected non-empty TransitionTiers")
	}
}

// driverRuleToAWS / awsRuleToDriver round-trip — every populated
// field on the driver shape lands on the AWS shape and back again.
// This is the marshalling path used by both GetLifecycle's response
// flattening and PutLifecycle's input building, so a round-trip
// covers both directions.
func TestRuleRoundTrip(t *testing.T) {
	exp := 30
	trn := 7
	nc := 14
	abort := 3

	in := driverpkg.LifecycleRule{
		ID:                 "r1",
		Status:             "Enabled",
		Prefix:             "logs/",
		ExpirationDays:     &exp,
		TransitionDays:     &trn,
		TransitionTier:     "GLACIER",
		NoncurrentDays:     &nc,
		AbortMultipartDays: &abort,
	}

	awsRule := driverRuleToAWS(in)

	// Verify the AWS-side shape is well-formed.
	if awsRule.ID == nil || *awsRule.ID != "r1" {
		t.Fatalf("ID mismatch: %v", awsRule.ID)
	}
	if awsRule.Status != types.ExpirationStatusEnabled {
		t.Fatalf("Status mismatch: %v", awsRule.Status)
	}
	if awsRule.Filter == nil || awsRule.Filter.Prefix == nil || *awsRule.Filter.Prefix != "logs/" {
		t.Fatalf("Filter.Prefix mismatch: %+v", awsRule.Filter)
	}
	if awsRule.Expiration == nil || awsRule.Expiration.Days == nil || *awsRule.Expiration.Days != 30 {
		t.Fatalf("Expiration mismatch: %+v", awsRule.Expiration)
	}
	if len(awsRule.Transitions) != 1 || aws.ToInt32(awsRule.Transitions[0].Days) != 7 ||
		awsRule.Transitions[0].StorageClass != types.TransitionStorageClassGlacier {
		t.Fatalf("Transitions mismatch: %+v", awsRule.Transitions)
	}
	if awsRule.NoncurrentVersionExpiration == nil ||
		aws.ToInt32(awsRule.NoncurrentVersionExpiration.NoncurrentDays) != 14 {
		t.Fatalf("Noncurrent mismatch")
	}
	if awsRule.AbortIncompleteMultipartUpload == nil ||
		aws.ToInt32(awsRule.AbortIncompleteMultipartUpload.DaysAfterInitiation) != 3 {
		t.Fatalf("Abort mismatch")
	}

	// Round-trip back.
	out := awsRuleToDriver(awsRule)
	if out.ID != in.ID || out.Status != in.Status || out.Prefix != in.Prefix {
		t.Fatalf("scalar fields lost: %+v", out)
	}
	if out.ExpirationDays == nil || *out.ExpirationDays != 30 {
		t.Fatalf("ExpirationDays lost: %v", out.ExpirationDays)
	}
	if out.TransitionDays == nil || *out.TransitionDays != 7 || out.TransitionTier != "GLACIER" {
		t.Fatalf("Transition fields lost: %v %v", out.TransitionDays, out.TransitionTier)
	}
	if out.NoncurrentDays == nil || *out.NoncurrentDays != 14 {
		t.Fatalf("NoncurrentDays lost")
	}
	if out.AbortMultipartDays == nil || *out.AbortMultipartDays != 3 {
		t.Fatalf("AbortMultipartDays lost")
	}
}
