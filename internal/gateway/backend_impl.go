// Package gateway: ProductionBackend composes basement's existing
// primitives (config, store.Users, ServiceAccounts, UserRegions,
// driver.Registry, Connections) into the Backend interface. The
// gateway code never reaches into any of those packages directly;
// everything goes through this composition so:
//
//   - the gateway tests can swap a small fake Backend in,
//   - protocol code never grows a transitive dep on internal/store,
//   - future gateways (SMB, NFS) inherit the same auth + data plane
//     surface for free.

package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/serviceaccount"
	"github.com/mattjackson/basement/internal/store"
)

// userLookup is the narrow surface the production Backend needs from
// store.Store for the Basic-auth password path. *store.Store satisfies
// this directly.
type userLookup interface {
	UserByUsername(name string) (store.User, error)
}

// ProductionBackend wires the real basement primitives into the
// Backend contract. Built once at boot via NewProductionBackend.
type ProductionBackend struct {
	cfg         *config.Config
	users       userLookup
	sas         serviceaccount.ServiceAccounts
	regions     store.UserRegions
	driverReg   *driver.Registry
	connections store.Connections

	// envAdmin* cache the per-process env-admin credentials so we
	// don't reach into cfg.Admin on every AuthBasic. Loaded lazily
	// inside loadEnvAdmin (mirrors the lazy load in internal/webdav
	// pre-refactor — keeps boot order tolerant of cfg mutations).
	envAdminOnce sync.Once
	envAdminUser string
	envAdminHash string
}

// BackendDeps groups the constructor args so the call site reads
// nicely. Every field may be nil for tests; the call paths that
// touch a nil field return an explicit error rather than panicking.
type BackendDeps struct {
	Cfg         *config.Config
	Users       userLookup
	SAs         serviceaccount.ServiceAccounts
	Regions     store.UserRegions
	DriverReg   *driver.Registry
	Connections store.Connections
}

// NewProductionBackend builds the production Backend. Mirrors the
// webdav.New constructor's nil-safety: every dep is optional
// individually, the call paths that need it report a sane error if
// it's missing.
func NewProductionBackend(deps BackendDeps) *ProductionBackend {
	return &ProductionBackend{
		cfg:         deps.Cfg,
		users:       deps.Users,
		sas:         deps.SAs,
		regions:     deps.Regions,
		driverReg:   deps.DriverReg,
		connections: deps.Connections,
	}
}

// loadEnvAdmin populates the cached env-admin credentials. Lazy so
// the constructor doesn't have to defensive-nil-check Cfg.
func (b *ProductionBackend) loadEnvAdmin() {
	b.envAdminOnce.Do(func() {
		if b.cfg == nil {
			return
		}
		b.envAdminUser = b.cfg.Admin.User
		b.envAdminHash = b.cfg.Admin.PasswordHash
	})
}

// AuthBasic implements Backend. Mirrors the legacy webdav.authResolver
// flow exactly: env-admin first (same bcrypt compare), then the
// persisted user table. OIDC-only accounts (no PasswordHash) fail
// uniformly with ErrUnauthenticated.
func (b *ProductionBackend) AuthBasic(_ context.Context, user, pass string) (*UserContext, error) {
	if user == "" || pass == "" {
		return nil, ErrUnauthenticated
	}

	b.loadEnvAdmin()
	if user == b.envAdminUser && b.envAdminHash != "" {
		if auth.VerifyPassword(b.envAdminHash, pass) {
			return &UserContext{UserID: b.envAdminUser}, nil
		}
		return nil, ErrUnauthenticated
	}

	if b.users == nil {
		return nil, ErrUnauthenticated
	}
	u, err := b.users.UserByUsername(user)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	if u.PasswordHash == "" {
		return nil, ErrUnauthenticated
	}
	if !auth.VerifyPassword(u.PasswordHash, pass) {
		return nil, ErrUnauthenticated
	}
	return &UserContext{UserID: u.Username}, nil
}

// AuthBearer implements Backend. Accepts the "AKID:secret" colon-
// separated form the WebDAV gateway has shipped since v1.9.0a. Same
// revocation / expiry gating as the bearer middleware: any failed
// state returns ErrUnauthenticated uniformly so probing clients
// can't distinguish "unknown key" from "revoked key".
func (b *ProductionBackend) AuthBearer(ctx context.Context, akidSecret string) (*UserContext, error) {
	if b.sas == nil {
		return nil, ErrUnauthenticated
	}
	idx := strings.IndexByte(akidSecret, ':')
	if idx < 0 {
		return nil, ErrUnauthenticated
	}
	akid := akidSecret[:idx]
	secret := akidSecret[idx+1:]
	if akid == "" || secret == "" {
		return nil, ErrUnauthenticated
	}

	sa, err := b.sas.GetByAccessKey(ctx, akid)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	if sa.IsRevoked() || sa.IsExpired(nowFunc()) {
		return nil, ErrUnauthenticated
	}
	ok, err := b.sas.VerifySecret(ctx, akid, secret)
	if err != nil || !ok {
		return nil, ErrUnauthenticated
	}
	_ = b.sas.TouchLastUsed(ctx, sa.ID)

	caps := make([]string, 0, len(sa.Capabilities))
	for _, c := range sa.Capabilities {
		caps = append(caps, c.ID)
	}
	return &UserContext{
		UserID:           sa.OwnerUserID,
		ServiceAccountID: sa.ID,
		Capabilities:     caps,
	}, nil
}

// AuthSigV4 reserves the SigV4 verification slot for the v2.0 S3
// gateway. v1.9.0c returns ErrUnsupported — the S3 stub gateway
// surfaces it back to the client as a 501.
func (b *ProductionBackend) AuthSigV4(_ context.Context, _ *http.Request) (*UserContext, error) {
	return nil, ErrUnsupported
}

// ListRegions implements Backend. Delegates to the per-user regions
// store. Returns an empty slice when the store is unwired so the
// gateway code can treat the result as "no regions" without nil
// checking.
func (b *ProductionBackend) ListRegions(ctx context.Context, uctx *UserContext) ([]Region, error) {
	if b.regions == nil || uctx == nil || uctx.UserID == "" {
		return nil, nil
	}
	rs, err := b.regions.ListForUser(ctx, uctx.UserID)
	if err != nil {
		return nil, fmt.Errorf("ListRegions: %w", err)
	}
	out := make([]Region, 0, len(rs))
	for _, r := range rs {
		out = append(out, Region{
			ID:              r.ID,
			Alias:           r.Alias,
			Endpoint:        r.Endpoint,
			AccessKeyID:     r.AccessKeyID,
			Region:          r.Region,
			AddressingStyle: r.AddressingStyle,
			CreatedAt:       r.CreatedAt,
		})
	}
	return out, nil
}

// driverForRegion builds the per-region driver. Mirrors
// webdav.Handler.driverFactory from before the refactor — same
// caching shape via driver.Registry, same TouchLastUsed best-effort.
func (b *ProductionBackend) driverForRegion(ctx context.Context, regionID string) (driver.Driver, store.UserRegion, error) {
	if b.regions == nil || b.driverReg == nil {
		return nil, store.UserRegion{}, ErrUnsupported
	}
	region, err := b.regions.Get(ctx, regionID)
	if err != nil {
		if errors.Is(err, store.ErrUserRegionNotFound) {
			return nil, store.UserRegion{}, ErrNotFound
		}
		return nil, store.UserRegion{}, fmt.Errorf("region lookup: %w", err)
	}
	secret, err := b.regions.Decrypt(region)
	if err != nil {
		return nil, region, fmt.Errorf("region decrypt: %w", err)
	}
	d, err := b.driverReg.ForUserRegion(ctx, region.Endpoint, region.AccessKeyID, secret, region.Region, region.AddressingStyle)
	if err != nil {
		return nil, region, fmt.Errorf("driver build: %w", err)
	}
	_ = b.regions.TouchLastUsed(ctx, region.ID)
	return d, region, nil
}

// ListBuckets implements Backend. Applies the Garage admin bridge
// when the region's endpoint matches an admin Connection on a Garage
// backend — same logic the WebDAV handler carried pre-refactor.
func (b *ProductionBackend) ListBuckets(ctx context.Context, _ *UserContext, regionID string) ([]Bucket, error) {
	d, region, err := b.driverForRegion(ctx, regionID)
	if err != nil {
		return nil, err
	}

	if bridged, applied, err := b.adminBridge(ctx, region); err != nil {
		return nil, err
	} else if applied {
		return convertBuckets(bridged), nil
	}

	bs, err := d.ListBuckets(ctx)
	if err != nil {
		return nil, mapDriverError(err)
	}
	return convertBuckets(bs), nil
}

// adminBridge mirrors webdav.Handler.adminBridge: when the region's
// endpoint matches an admin-curated Garage Connection, list buckets
// via the admin key (Garage's data-plane ListBuckets returns empty
// for user keys today). Returns (nil, false, nil) when no bridge
// applies — gateway falls back to the user-key list.
func (b *ProductionBackend) adminBridge(ctx context.Context, region store.UserRegion) ([]driver.Bucket, bool, error) {
	if b.connections == nil || b.driverReg == nil {
		return nil, false, nil
	}
	conns, err := b.connections.List(ctx)
	if err != nil {
		// Don't fail the listing for a hiccup in admin lookups; the
		// user driver answers below.
		return nil, false, nil
	}
	target := region.Endpoint
	var match *store.Connection
	for i := range conns {
		c := &conns[i]
		raw := strings.TrimSpace(c.Config["s3_endpoint"])
		if raw == "" {
			raw = strings.TrimSpace(c.Config["endpoint"])
		}
		if raw == "" {
			continue
		}
		canon, err := store.NormalizeEndpoint(raw)
		if err != nil || canon != target {
			continue
		}
		if c.Driver != store.DriverGarage && c.Driver != store.DriverGarageV1 {
			continue
		}
		match = c
		break
	}
	if match == nil {
		return nil, false, nil
	}
	adminDrv, err := b.driverReg.For(ctx, match.ID)
	if err != nil {
		return nil, true, err
	}
	buckets, err := adminDrv.ListBuckets(ctx)
	if err != nil {
		return nil, true, err
	}
	return buckets, true, nil
}

// ListObjects implements Backend.
func (b *ProductionBackend) ListObjects(ctx context.Context, _ *UserContext, regionID, bucket, prefix, delimiter, continuationToken string, limit int) (ObjectPage, error) {
	d, _, err := b.driverForRegion(ctx, regionID)
	if err != nil {
		return ObjectPage{}, err
	}
	if limit <= 0 {
		limit = 1000
	}
	page, err := d.ListObjects(ctx, bucket, prefix, continuationToken, delimiter, limit)
	if err != nil {
		return ObjectPage{}, mapDriverError(err)
	}
	out := ObjectPage{
		Objects:          make([]ObjectMeta, 0, len(page.Objects)),
		CommonPrefixes:   page.CommonPrefixes,
		IsTruncated:      page.IsTruncated,
		NextContinuation: page.NextContinuation,
	}
	for _, o := range page.Objects {
		out.Objects = append(out.Objects, ObjectMeta{
			Key:          o.Key,
			Size:         o.Size,
			LastModified: o.LastModified,
			ETag:         o.ETag,
			ContentType:  o.ContentType,
		})
	}
	return out, nil
}

// HeadObject implements Backend.
func (b *ProductionBackend) HeadObject(ctx context.Context, _ *UserContext, regionID, bucket, key string) (ObjectMeta, error) {
	d, _, err := b.driverForRegion(ctx, regionID)
	if err != nil {
		return ObjectMeta{}, err
	}
	oi, err := d.StatObject(ctx, bucket, key)
	if err != nil {
		return ObjectMeta{}, mapDriverError(err)
	}
	return ObjectMeta{
		Key:          oi.Key,
		Size:         oi.Size,
		LastModified: oi.LastModified,
		ETag:         strings.TrimSpace(oi.ETag),
		ContentType:  strings.TrimSpace(oi.ContentType),
	}, nil
}

// GetObject implements Backend.
func (b *ProductionBackend) GetObject(ctx context.Context, _ *UserContext, regionID, bucket, key string) (io.ReadCloser, ObjectMeta, error) {
	d, _, err := b.driverForRegion(ctx, regionID)
	if err != nil {
		return nil, ObjectMeta{}, err
	}
	stream, err := d.StreamObject(ctx, bucket, key, "")
	if err != nil {
		return nil, ObjectMeta{}, mapDriverError(err)
	}
	meta := ObjectMeta{
		Key:          key,
		Size:         stream.ContentLength,
		LastModified: stream.LastModified,
		ETag:         strings.TrimSpace(stream.ETag),
		ContentType:  strings.TrimSpace(stream.ContentType),
	}
	return stream.Body, meta, nil
}

// PutObject implements Backend.
func (b *ProductionBackend) PutObject(ctx context.Context, _ *UserContext, regionID, bucket, key string, body io.Reader, size int64, contentType string) error {
	d, _, err := b.driverForRegion(ctx, regionID)
	if err != nil {
		return err
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if _, err := d.PutObjectStream(ctx, bucket, key, body, contentType, size); err != nil {
		return mapDriverError(err)
	}
	return nil
}

// DeleteObject implements Backend.
func (b *ProductionBackend) DeleteObject(ctx context.Context, _ *UserContext, regionID, bucket, key string) error {
	d, _, err := b.driverForRegion(ctx, regionID)
	if err != nil {
		return err
	}
	if err := d.DeleteObject(ctx, bucket, key); err != nil {
		return mapDriverError(err)
	}
	return nil
}

// CopyObject implements Backend. Same-region only — the cycle scope
// explicitly excludes cross-region copy.
func (b *ProductionBackend) CopyObject(ctx context.Context, _ *UserContext, srcRegionID, srcBucket, srcKey, dstRegionID, dstBucket, dstKey string) error {
	if srcRegionID != dstRegionID {
		return ErrUnsupported
	}
	d, _, err := b.driverForRegion(ctx, srcRegionID)
	if err != nil {
		return err
	}
	if err := d.ServerSideCopy(ctx, srcBucket, srcKey, dstBucket, dstKey); err != nil {
		return mapDriverError(err)
	}
	return nil
}

// CreateBucket implements Backend. Delegates to the driver's
// CreateBucket — drivers that don't support bucket creation surface
// ErrUnsupported back through mapDriverError.
func (b *ProductionBackend) CreateBucket(ctx context.Context, _ *UserContext, regionID, bucket string) error {
	d, _, err := b.driverForRegion(ctx, regionID)
	if err != nil {
		return err
	}
	if _, err := d.CreateBucket(ctx, driver.BucketSpec{Alias: bucket}); err != nil {
		return mapDriverError(err)
	}
	return nil
}

// DeleteBucket implements Backend.
func (b *ProductionBackend) DeleteBucket(ctx context.Context, _ *UserContext, regionID, bucket string) error {
	d, _, err := b.driverForRegion(ctx, regionID)
	if err != nil {
		return err
	}
	if err := d.DeleteBucket(ctx, bucket); err != nil {
		return mapDriverError(err)
	}
	return nil
}

// convertBuckets adapts driver.Bucket -> gateway.Bucket so callers
// downstream of ListBuckets don't have to import internal/driver.
func convertBuckets(bs []driver.Bucket) []Bucket {
	out := make([]Bucket, 0, len(bs))
	for _, b := range bs {
		out = append(out, Bucket{
			ID:      b.ID,
			Aliases: b.Aliases,
			Created: b.Created,
			Objects: b.Objects,
			Bytes:   b.Bytes,
		})
	}
	return out
}

// mapDriverError translates internal/driver sentinels into gateway-
// tier sentinels so protocol code can errors.Is against this package
// without a transitive dep on internal/driver.
func mapDriverError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, driver.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, driver.ErrUnsupported):
		return ErrUnsupported
	case errors.Is(err, driver.ErrConflict):
		return ErrConflict
	case errors.Is(err, driver.ErrPermissionDenied):
		return ErrUnauthorized
	case errors.Is(err, driver.ErrUnauthenticated):
		return ErrUnauthenticated
	}
	return err
}
