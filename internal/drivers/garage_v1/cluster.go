package garage_v1

import (
	"context"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// Capabilities returns the Garage v1 driver's capability flags. From
// basement's POV, v1 supports the same set as v2: stage-apply-revert
// layout, quotas, bucket aliases, garage-style key model. Presign + multipart
// are advertised because the operator will wire them via aws-sdk-go-v2 in
// a follow-up; the methods themselves currently stub ErrUnsupported.
func (d *driver) Capabilities(_ context.Context) (driverpkg.Caps, error) {
	c := driverpkg.Caps{
		Driver:         driverName,
		Layout:         driverpkg.LayoutApplyRevert,
		Quotas:         true,
		BucketAliases:  true,
		KeyModel:       driverpkg.KeyModelGarage,
		Presign:        d.s3Client != nil,
		Multipart:      d.s3Client != nil,
		Versioning:     false,
		ObjectBrowse:   d.s3Client != nil,
	}
	return c, nil
}

// HealthCheck reports the cluster health.
// Endpoint: GET /v1/health (garage-admin-v1.yml:10-61).
//
// The v1 health response uses storageNodesOk (NOT storageNodesUp like v2);
// see garage-admin-v1.yml:46-49.
func (d *driver) HealthCheck(ctx context.Context) (driverpkg.HealthReport, error) {
	var resp getHealthResponseV1
	if err := d.client.do(ctx, "GET", "/v1/health", nil, &resp); err != nil {
		return driverpkg.HealthReport{}, err
	}

	details := map[string]any{
		"knownNodes":       resp.KnownNodes,
		"connectedNodes":   resp.ConnectedNodes,
		"storageNodes":     resp.StorageNodes,
		"storageNodesOk":   resp.StorageNodesOk,
		"partitions":       resp.Partitions,
		"partitionsQuorum": resp.PartitionsQuorum,
		"partitionsAllOk":  resp.PartitionsAllOk,
	}

	return driverpkg.HealthReport{
		Status:  resp.Status,
		Details: details,
	}, nil
}

// ListNodes returns the cluster's known nodes, merging network info
// (knownNodes[]) with layout roles (layout.roles[]).
// Endpoint: GET /v1/status (garage-admin-v1.yml:62-135).
//
// ListNodes returns one entry per cluster member: it MERGES layout.roles
// (the source of truth for "who's in the cluster" with role + zone +
// capacity + tags) with knownNodes (network state — isUp, addr, hostname).
//
// Why layout.roles is the source of truth: knownNodes is the *network
// discovery* list, populated by ongoing peer gossip. On a settled
// single-node cluster, Garage v1.0.1 returns knownNodes=[] (the local node
// doesn't gossip with itself), but layout.roles still contains the local
// node with its assignment. Iterating knownNodes alone produces an empty
// dashboard.
//
// We start from layout.roles (cluster members), enrich each with
// network state from knownNodes when present. Any knownNode NOT in the
// layout (newly-connected, unassigned) is appended at the end.
func (d *driver) ListNodes(ctx context.Context) ([]driverpkg.Node, error) {
	// Garage v1.0.1's GET /v1/status returns layout.version but NOT
	// layout.roles[]. Roles only come back from the dedicated
	// GET /v1/layout endpoint. Hit both and merge.
	var status getStatusResponseV1
	if err := d.client.do(ctx, "GET", "/v1/status", nil, &status); err != nil {
		return nil, err
	}
	var layoutResp clusterLayoutV1
	if err := d.client.do(ctx, "GET", "/v1/layout", nil, &layoutResp); err != nil {
		return nil, err
	}
	// Splice the freshly-fetched roles back into the status response so the
	// rest of the function reads naturally.
	resp := status
	resp.Layout = layoutResp

	// Index knownNodes by id for O(1) enrichment.
	known := make(map[string]nodeNetworkInfoV1, len(resp.KnownNodes))
	for _, kn := range resp.KnownNodes {
		known[kn.ID] = kn
	}

	nodes := make([]driverpkg.Node, 0, len(resp.Layout.Roles))
	seen := make(map[string]struct{}, len(resp.Layout.Roles))

	// Cluster members from layout.roles. Each is in the cluster regardless
	// of whether knownNodes reports it (single-node clusters omit self).
	for _, r := range resp.Layout.Roles {
		seen[r.ID] = struct{}{}
		node := driverpkg.Node{
			ID:      r.ID,
			Zone:    r.Zone,
			Tags:    r.Tags,
			Version: resp.GarageVersion,
		}
		if r.Capacity != nil {
			node.Capacity = *r.Capacity
			node.Role = "storage"
		} else {
			// "Setting the capacity to `null` will configure the node as a
			// gateway." -- garage-admin-v1.yml:222
			node.Role = "gateway"
		}

		if kn, ok := known[r.ID]; ok {
			node.Hostname = kn.Hostname
			node.Address = kn.Addr
			if kn.IsUp {
				node.Status = "connected"
			} else {
				node.Status = "unreachable"
			}
		} else if r.ID == resp.Node {
			// The local node (resp.Node) reports through Garage's own admin
			// API; if we got a response, it's reachable.
			node.Status = "connected"
		} else {
			node.Status = "unknown"
		}

		nodes = append(nodes, node)
	}

	// Surface knownNodes that aren't in the layout yet (e.g. newly
	// connected, awaiting role assignment).
	for _, kn := range resp.KnownNodes {
		if _, ok := seen[kn.ID]; ok {
			continue
		}
		status := "connected"
		if !kn.IsUp {
			status = "unreachable"
		}
		nodes = append(nodes, driverpkg.Node{
			ID:       kn.ID,
			Hostname: kn.Hostname,
			Address:  kn.Addr,
			Status:   status,
			Version:  resp.GarageVersion,
			Role:     "unassigned",
		})
	}

	return nodes, nil
}

// GetLayout returns the current cluster layout plus any staged role changes.
// Endpoint: GET /v1/layout (garage-admin-v1.yml:187-212).
//
// ClusterLayout: garage-admin-v1.yml:1184-1218. stagedRoleChanges entries
// are NodeRoleChange (oneOf NodeRoleRemove/NodeRoleUpdate;
// garage-admin-v1.yml:1147-1182).
func (d *driver) GetLayout(ctx context.Context) (driverpkg.Layout, error) {
	var resp clusterLayoutV1
	if err := d.client.do(ctx, "GET", "/v1/layout", nil, &resp); err != nil {
		return driverpkg.Layout{}, err
	}

	layout := driverpkg.Layout{
		Version: resp.Version,
		Nodes:   make([]driverpkg.Node, 0, len(resp.Roles)),
		Staged:  nil,
	}

	for _, r := range resp.Roles {
		node := driverpkg.Node{
			ID:     r.ID,
			Zone:   r.Zone,
			Tags:   r.Tags,
			Status: "connected",
			Role:   "storage",
		}
		if r.Capacity != nil {
			node.Capacity = *r.Capacity
		} else {
			node.Role = "gateway"
		}
		layout.Nodes = append(layout.Nodes, node)
	}

	if len(resp.StagedRoleChanges) > 0 {
		staged := driverpkg.Layout{
			Version: layout.Version + 1,
			Nodes:   make([]driverpkg.Node, 0, len(resp.StagedRoleChanges)),
		}
		for _, change := range resp.StagedRoleChanges {
			if change.Remove {
				// Removal: surface as a Node entry with no Zone/Capacity so
				// callers can compute the diff against layout.Nodes.
				staged.Nodes = append(staged.Nodes, driverpkg.Node{
					ID:     change.ID,
					Status: "removed",
				})
				continue
			}
			node := driverpkg.Node{
				ID:     change.ID,
				Zone:   change.Zone,
				Tags:   change.Tags,
				Status: "connected",
				Role:   "storage",
			}
			if change.Capacity != nil {
				node.Capacity = *change.Capacity
			} else {
				node.Role = "gateway"
			}
			staged.Nodes = append(staged.Nodes, node)
		}
		layout.Staged = &staged
	}

	return layout, nil
}

// StageLayout stages a single node change against the cluster layout.
// Endpoint: POST /v1/layout (garage-admin-v1.yml:214-259).
//
// Unlike the v2 admin API, v1 takes an ARRAY of NodeRoleChange objects
// (garage-admin-v1.yml:237-248). This driver sends a single-element array
// per call.
//
// Semantics from the spec (garage-admin-v1.yml:228-233): "Contrary to the
// CLI ... when calling this API all of these values must be specified" for
// an update — meaning zone+capacity+tags are required to *add* or *update*
// a role; only removals can omit them (remove: true).
//
// OPEN: this driver follows v2's pattern of expecting the caller to supply
// a fully-formed LayoutChange (Zone/Capacity/Tags set together) for updates.
// If only some fields are set the request will still be sent and Garage will
// 400 — surfaced as ErrInvalid.
func (d *driver) StageLayout(ctx context.Context, change driverpkg.LayoutChange) (driverpkg.LayoutDiff, error) {
	var body []nodeRoleChangeV1

	// Remove if no zone/capacity/tags requested AND no role specified
	// (caller intent is unclear); the v2 driver assumes any non-removal
	// has at least zone set. We do the same: any change with no fields is
	// treated as a noop and we just refresh the layout.
	if change.Zone == nil && change.Capacity == nil && len(change.Tags) == 0 && change.Role == nil {
		// Treat as a removal request.
		body = []nodeRoleChangeV1{{ID: change.NodeID, Remove: true}}
	} else {
		entry := nodeRoleChangeV1{
			ID:   change.NodeID,
			Tags: change.Tags,
		}
		if change.Zone != nil {
			entry.Zone = *change.Zone
		}
		// capacity: nil means gateway in the v1 wire format
		// (garage-admin-v1.yml:222). Caller's Capacity==nil + Role=="gateway"
		// both encode to a null capacity; Capacity!=nil encodes to the
		// concrete byte count.
		if change.Capacity != nil {
			capacity := *change.Capacity
			entry.Capacity = &capacity
		} else if change.Role != nil && *change.Role == "gateway" {
			entry.Capacity = nil
		}
		body = []nodeRoleChangeV1{entry}
	}

	var resp clusterLayoutV1
	if err := d.client.do(ctx, "POST", "/v1/layout", body, &resp); err != nil {
		return driverpkg.LayoutDiff{}, err
	}

	// Matches the v2 driver: we return an empty diff. Computing a real
	// diff requires knowing the prior layout, which this method does not
	// receive. Callers needing a diff should call GetLayout before+after.
	return driverpkg.LayoutDiff{}, nil
}

// ApplyLayout applies the currently staged layout changes.
// Endpoint: POST /v1/layout/apply (garage-admin-v1.yml:261-310).
//
// The spec (garage-admin-v1.yml:273-274) requires version = current + 1
// as a safety assertion. We GET /v1/layout first to discover current,
// then POST with current+1. Garage returns 400 on version mismatch in v1
// (no 409 documented for this endpoint), so a wrong version surfaces as
// ErrInvalid rather than ErrConflict.
func (d *driver) ApplyLayout(ctx context.Context) error {
	var current clusterLayoutV1
	if err := d.client.do(ctx, "GET", "/v1/layout", nil, &current); err != nil {
		return err
	}

	body := layoutVersionV1{Version: current.Version + 1}
	var resp applyLayoutResponseV1
	return d.client.do(ctx, "POST", "/v1/layout/apply", body, &resp)
}

// RevertLayout clears all staged layout changes.
// Endpoint: POST /v1/layout/revert (garage-admin-v1.yml:313-335).
//
// Spec (garage-admin-v1.yml:322-323): "the body must include the
// incremented version number, which MUST be 1 + the value of the currently
// existing layout". Same version-assertion semantics as ApplyLayout.
func (d *driver) RevertLayout(ctx context.Context) error {
	var current clusterLayoutV1
	if err := d.client.do(ctx, "GET", "/v1/layout", nil, &current); err != nil {
		return err
	}

	body := layoutVersionV1{Version: current.Version + 1}
	return d.client.do(ctx, "POST", "/v1/layout/revert", body, nil)
}

// ===== v1 wire types =====
// Suffixed V1 to avoid clashes with parallel/future types in this package.

// getHealthResponseV1 is the body of GET /v1/health
// (garage-admin-v1.yml:29-61).
type getHealthResponseV1 struct {
	Status           string `json:"status"`
	KnownNodes       int64  `json:"knownNodes"`
	ConnectedNodes   int64  `json:"connectedNodes"`
	StorageNodes     int64  `json:"storageNodes"`
	StorageNodesOk   int64  `json:"storageNodesOk"`
	Partitions       int64  `json:"partitions"`
	PartitionsQuorum int64  `json:"partitionsQuorum"`
	PartitionsAllOk  int64  `json:"partitionsAllOk"`
}

// getStatusResponseV1 is the body of GET /v1/status
// (garage-admin-v1.yml:86-135).
type getStatusResponseV1 struct {
	Node           string                `json:"node"`
	GarageVersion  string                `json:"garageVersion"`
	GarageFeatures []string              `json:"garageFeatures"`
	RustVersion    string                `json:"rustVersion"`
	DBEngine       string                `json:"dbEngine"`
	KnownNodes     []nodeNetworkInfoV1   `json:"knownNodes"`
	Layout         clusterLayoutV1       `json:"layout"`
}

// nodeNetworkInfoV1 mirrors NodeNetworkInfo
// (garage-admin-v1.yml:1106-1125).
type nodeNetworkInfoV1 struct {
	ID              string `json:"id"`
	Addr            string `json:"addr"`
	IsUp            bool   `json:"isUp"`
	LastSeenSecsAgo *int64 `json:"lastSeenSecsAgo,omitempty"`
	Hostname        string `json:"hostname"`
}

// nodeClusterInfoV1 mirrors NodeClusterInfo
// (garage-admin-v1.yml:1126-1146). The id field is present on the items
// of layout.roles[] (see ClusterLayout example, garage-admin-v1.yml:1194)
// even though the NodeClusterInfo schema itself doesn't list it among the
// required properties — Garage includes it in practice.
type nodeClusterInfoV1 struct {
	ID       string   `json:"id"`
	Zone     string   `json:"zone"`
	Capacity *int64   `json:"capacity"` // nullable: null means gateway
	Tags     []string `json:"tags"`
}

// nodeRoleChangeV1 covers both NodeRoleUpdate and NodeRoleRemove from the
// oneOf (garage-admin-v1.yml:1147-1182). We use a single struct with
// omitempty so an update sends zone/capacity/tags and a removal sends
// only {id, remove: true}.
type nodeRoleChangeV1 struct {
	ID       string   `json:"id"`
	Zone     string   `json:"zone,omitempty"`
	Capacity *int64   `json:"capacity,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Remove   bool     `json:"remove,omitempty"`
}

// clusterLayoutV1 mirrors ClusterLayout (garage-admin-v1.yml:1184-1218).
type clusterLayoutV1 struct {
	Version           int                 `json:"version"`
	Roles             []nodeClusterInfoV1 `json:"roles"`
	StagedRoleChanges []nodeRoleChangeV1  `json:"stagedRoleChanges"`
}

// layoutVersionV1 mirrors LayoutVersion (garage-admin-v1.yml:1219-1226).
type layoutVersionV1 struct {
	Version int `json:"version"`
}

// applyLayoutResponseV1 is the body of POST /v1/layout/apply
// (garage-admin-v1.yml:289-310).
type applyLayoutResponseV1 struct {
	Message []string        `json:"message"`
	Layout  clusterLayoutV1 `json:"layout"`
}
