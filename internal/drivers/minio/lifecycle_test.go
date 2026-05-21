package minio

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
		t.Fatalf("expected Supported=true for minio")
	}
	if !caps.Expiration || !caps.Transition || !caps.NoncurrentDays || !caps.AbortMultipartDays {
		t.Fatalf("expected all sub-axes true, got %+v", caps)
	}
}

func TestRuleRoundTrip(t *testing.T) {
	exp := 30
	trn := 7

	in := driverpkg.LifecycleRule{
		ID:             "r1",
		Status:         "Disabled",
		Prefix:         "tmp/",
		ExpirationDays: &exp,
		TransitionDays: &trn,
		TransitionTier: "STANDARD_IA",
	}
	awsRule := driverRuleToMinIO(in)

	if awsRule.Status != types.ExpirationStatusDisabled {
		t.Fatalf("Status mismatch: %v", awsRule.Status)
	}
	if aws.ToString(awsRule.Filter.Prefix) != "tmp/" {
		t.Fatalf("Filter.Prefix mismatch")
	}
	if len(awsRule.Transitions) != 1 || awsRule.Transitions[0].StorageClass != types.TransitionStorageClassStandardIa {
		t.Fatalf("Transitions mismatch: %+v", awsRule.Transitions)
	}

	out := minioRuleToDriver(awsRule)
	if out.Status != in.Status || out.Prefix != in.Prefix {
		t.Fatalf("scalars lost: %+v", out)
	}
	if out.TransitionTier != "STANDARD_IA" {
		t.Fatalf("Tier lost: %v", out.TransitionTier)
	}
}
