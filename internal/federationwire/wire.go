// Package federationwire wires the v1.6 federation engine to the
// production driver.Registry + store.UserRegions. Lives outside the
// federation package so the engine + its narrow ReplicationClient
// surface stay free of imports on the driver / store packages — that
// keeps the import graph acyclic (store -> federation; never reverse)
// and lets the engine unit tests substitute in-memory fakes without
// dragging in the full driver/store stack.
//
// Entry point: NewResolver returns a federation.DriverResolver suitable
// for handing into federation.NewEngine. cmd/basement-server/main.go calls
// it once at boot.
package federationwire

import (
	"context"
	"fmt"
	"io"

	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/federation"
	"github.com/mattjackson/basement/internal/store"
)

// NewResolver builds the production DriverResolver. Both arguments are
// required; passing nil produces a resolver that errors on every
// Resolve call (the engine then records the error as a failed tick and
// retries on the next interval).
func NewResolver(regions store.UserRegions, registry *driver.Registry) federation.DriverResolver {
	return federation.DriverResolverFunc(func(ctx context.Context, ownerUserID, regionID string) (federation.ReplicationClient, error) {
		if regions == nil {
			return nil, fmt.Errorf("federationwire: regions store not wired")
		}
		if registry == nil {
			return nil, fmt.Errorf("federationwire: driver registry not wired")
		}

		region, err := regions.Get(ctx, regionID)
		if err != nil {
			return nil, fmt.Errorf("federationwire: get region %q: %w", regionID, err)
		}
		if region.UserID != ownerUserID {
			// Refuse to bridge to another user's key — the engine has
			// no business signing requests with credentials it didn't
			// own at federation-create time.
			return nil, fmt.Errorf("federationwire: region %q not owned by %q", regionID, ownerUserID)
		}

		secret, err := regions.Decrypt(region)
		if err != nil {
			return nil, fmt.Errorf("federationwire: decrypt region secret: %w", err)
		}

		drv, err := registry.ForUserRegion(ctx, region.Endpoint, region.AccessKeyID, secret, region.Region, region.AddressingStyle)
		if err != nil {
			return nil, fmt.Errorf("federationwire: build region driver: %w", err)
		}

		return &driverAdapter{drv: drv}, nil
	})
}

// driverAdapter translates the production driver.Driver surface to the
// narrow federation.ReplicationClient interface. Each method is a
// straight pass-through with the parameter shape the engine wants
// (delimiter "" is hardcoded — the engine never sub-folder-browses).
type driverAdapter struct {
	drv driver.Driver
}

func (a *driverAdapter) Capabilities(ctx context.Context) (federation.Capabilities, error) {
	c, err := a.drv.Capabilities(ctx)
	if err != nil {
		return federation.Capabilities{}, err
	}
	return federation.Capabilities{
		Driver:         c.Driver,
		ServerSideCopy: c.ServerSideCopy,
	}, nil
}

func (a *driverAdapter) ListObjects(ctx context.Context, bucket, continuation string, limit int) (federation.ObjectPage, error) {
	page, err := a.drv.ListObjects(ctx, bucket, "", continuation, "", limit)
	if err != nil {
		return federation.ObjectPage{}, err
	}
	out := make([]federation.ObjectInfo, 0, len(page.Objects))
	for _, o := range page.Objects {
		out = append(out, federation.ObjectInfo{
			Key:          o.Key,
			Size:         o.Size,
			ETag:         o.ETag,
			LastModified: o.LastModified,
			IsDir:        o.IsDir,
		})
	}
	return federation.ObjectPage{
		Objects:          out,
		NextContinuation: page.NextContinuation,
		IsTruncated:      page.IsTruncated,
	}, nil
}

func (a *driverAdapter) StatObject(ctx context.Context, bucket, key string) (federation.ObjectInfo, error) {
	o, err := a.drv.StatObject(ctx, bucket, key)
	if err != nil {
		return federation.ObjectInfo{}, err
	}
	return federation.ObjectInfo{
		Key:          o.Key,
		Size:         o.Size,
		ETag:         o.ETag,
		LastModified: o.LastModified,
		IsDir:        o.IsDir,
	}, nil
}

func (a *driverAdapter) StreamObject(ctx context.Context, bucket, key string) (federation.StreamResult, error) {
	s, err := a.drv.StreamObject(ctx, bucket, key, "")
	if err != nil {
		return federation.StreamResult{}, err
	}
	return federation.StreamResult{
		Body:          s.Body,
		ContentType:   s.ContentType,
		ContentLength: s.ContentLength,
		ETag:          s.ETag,
		LastModified:  s.LastModified,
	}, nil
}

func (a *driverAdapter) PutObjectStream(ctx context.Context, bucket, key string, reader io.Reader, contentType string, size int64) error {
	_, err := a.drv.PutObjectStream(ctx, bucket, key, reader, contentType, size)
	return err
}

func (a *driverAdapter) ServerSideCopy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	return a.drv.ServerSideCopy(ctx, srcBucket, srcKey, dstBucket, dstKey)
}

// DeleteObject is the v1.7.0f addition — the event-driven federation
// path needs delete propagation to mirror ObjectDeleted envelopes
// from primary to replicas. Direct pass-through to the production
// driver; backends that lack delete privileges surface the same 4xx
// the operator would see from a manual DELETE.
func (a *driverAdapter) DeleteObject(ctx context.Context, bucket, key string) error {
	return a.drv.DeleteObject(ctx, bucket, key)
}
