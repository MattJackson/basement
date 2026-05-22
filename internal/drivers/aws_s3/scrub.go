// Package aws_s3: block-scrub stubs (v1.4.0c).
//
// AWS S3 manages durability internally (the "11 nines" service level)
// and exposes no operator-initiated scrub endpoint. We advertise
// Supported=false with a human-readable Reason; the UI hides the Run
// button and surfaces the Reason text in its place. ScrubState +
// StartScrub return ErrUnsupported — the API layer short-circuits on
// the capability flag before reaching those, but the wrapped sentinel
// keeps a direct caller's error path sane.
package aws_s3

import (
	"context"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

const scrubUnsupportedReason = "Backend manages durability via internal mechanisms; no operator-initiated scrub"

func (d *driver) ScrubSupport() driverpkg.ScrubCapability {
	return driverpkg.ScrubCapability{
		Supported: false,
		Reason:    scrubUnsupportedReason,
	}
}

func (d *driver) ScrubState(_ context.Context) (driverpkg.ScrubState, error) {
	return driverpkg.ScrubState{}, &driverpkg.Error{
		Op:      "ScrubState",
		Driver:  driverName,
		Err:     driverpkg.ErrUnsupported,
		Message: scrubUnsupportedReason,
	}
}

func (d *driver) StartScrub(_ context.Context) error {
	return &driverpkg.Error{
		Op:      "StartScrub",
		Driver:  driverName,
		Err:     driverpkg.ErrUnsupported,
		Message: scrubUnsupportedReason,
	}
}
