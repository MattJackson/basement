// Package minio: block-scrub stubs (v1.4.0c).
//
// MinIO does expose healing via mc admin heal, but that surface is
// cluster-wide and operator-driven outside basement's per-cluster
// admin model (and the S3 API itself has no scrub op). For v1.4.0c we
// advertise Supported=false with the same Reason as aws_s3; a future
// cycle could wire MinIO's MC healing API in if operators ask for it.
package minio

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
