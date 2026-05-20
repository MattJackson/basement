// Package garage implements the garage device driver.
package garage

import (
	"context"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// Capabilities returns Garage v2 driver capabilities.
// Citation: docs/garage-admin-api.md lines 65-72, design.md § The driver interface
func (d *driver) Capabilities(_ context.Context) (driverpkg.Caps, error) {
	return driverpkg.Caps{
		Driver:         "garage",
		Layout:         driverpkg.LayoutApplyRevert,
		Quotas:         true,
		BucketAliases:  true,
		KeyModel:       driverpkg.KeyModelGarage,
		Presign:        d.s3Client != nil,
		Multipart:      d.s3Client != nil,
		Versioning:     false,
		ObjectBrowse:   d.s3Client != nil,
	}, nil
}

// HealthCheck implements health check for Garage cluster.
// Endpoint: GET /v2/GetClusterHealth (docs/garage-admin-api.md lines 65-72)
func (d *driver) HealthCheck(ctx context.Context) (driverpkg.HealthReport, error) {
	var resp getClusterHealthResponse
	if err := d.client.do(ctx, "GET", "/v2/GetClusterHealth", nil, &resp); err != nil {
		return driverpkg.HealthReport{}, err
	}

	details := map[string]any{
		"knownNodes":        resp.KnownNodes,
		"connectedNodes":    resp.ConnectedNodes,
		"storageNodes":      resp.StorageNodes,
		"storageNodesUp":    resp.StorageNodesUp,
		"partitions":        resp.Partitions,
		"partitionsQuorum":  resp.PartitionsQuorum,
		"partitionsAllOk":   resp.PartitionsAllOk,
	}

	return driverpkg.HealthReport{
		Status:  resp.Status,
		Details: details,
	}, nil
}

// ListNodes returns the list of nodes in the cluster.
// Endpoint: GET /v2/GetClusterStatus (docs/garage-admin-api.md lines 74-81)
func (d *driver) ListNodes(ctx context.Context) ([]driverpkg.Node, error) {
	var resp getClusterStatusResponse
	if err := d.client.do(ctx, "GET", "/v2/GetClusterStatus", nil, &resp); err != nil {
		return nil, err
	}

	nodes := make([]driverpkg.Node, 0, len(resp.Nodes))
	for _, n := range resp.Nodes {
		node := driverpkg.Node{
			ID:      n.ID,
			Address: n.Addr,
			Hostname: n.Hostname,
			Status:  "connected",
			Version: n.GarageVersion,
		}

		if n.Role != nil && n.Role.NodeAssignedRole != nil {
			node.Zone = n.Role.NodeAssignedRole.Zone
			if n.Role.NodeAssignedRole.Capacity != nil {
				node.Capacity = *n.Role.NodeAssignedRole.Capacity
			}
			node.Tags = n.Role.NodeAssignedRole.Tags
			node.Role = "storage"
			if n.Role.Gateway != nil && *n.Role.Gateway {
				node.Role = "gateway"
			}
		}

		if !n.IsUp {
			node.Status = "unreachable"
		}

		nodes = append(nodes, node)
	}

	return nodes, nil
}

// GetLayout returns the cluster layout (current and staged).
// Endpoint: GET /v2/GetClusterLayout (docs/garage-admin-api.md lines 126-133, garage-admin-v2.json:723-746)
func (d *driver) GetLayout(ctx context.Context) (driverpkg.Layout, error) {
	var resp getClusterLayoutResponse
	if err := d.client.do(ctx, "GET", "/v2/GetClusterLayout", nil, &resp); err != nil {
		return driverpkg.Layout{}, err
	}

	layout := driverpkg.Layout{
		Version: int(resp.Version),
		Nodes:   make([]driverpkg.Node, 0, len(resp.Roles)),
		Staged:  nil,
	}

	for _, r := range resp.Roles {
		var capacity int64
		if r.Capacity != nil {
			capacity = *r.Capacity
		}

		node := driverpkg.Node{
			ID:       r.ID,
			Zone:     r.Zone,
			Capacity: capacity,
			Tags:     r.Tags,
			Status:   "connected",
			Role:     "storage",
		}

		if r.Gateway != nil && *r.Gateway {
			node.Role = "gateway"
		}

		layout.Nodes = append(layout.Nodes, node)
	}

	if resp.StagedRoleChanges != nil && len(*resp.StagedRoleChanges) > 0 {
		staged := driverpkg.Layout{
			Version: layout.Version + 1,
			Nodes:   make([]driverpkg.Node, 0),
		}

		for _, change := range *resp.StagedRoleChanges {
			node := driverpkg.Node{
				ID:     change.ID,
				Status: "connected",
			}

			if change.NewRole != nil {
				if change.NewRole.NodeAssignedRole != nil {
					node.Zone = change.NewRole.NodeAssignedRole.Zone
					if change.NewRole.NodeAssignedRole.Capacity != nil {
						node.Capacity = *change.NewRole.NodeAssignedRole.Capacity
					}
					node.Tags = change.NewRole.NodeAssignedRole.Tags
					node.Role = "storage"
					if change.NewRole.NodeAssignedRole.Gateway != nil && *change.NewRole.NodeAssignedRole.Gateway {
						node.Role = "gateway"
					}
				} else if change.NewRole.Gateway != nil && *change.NewRole.Gateway {
					node.Role = "gateway"
				}
			}

			staged.Nodes = append(staged.Nodes, node)
		}

		layout.Staged = &staged
	} else {
		layout.Staged = nil
	}

	return layout, nil
}

// StageLayout stages a layout change.
// Endpoint: POST /v2/UpdateClusterLayout (garage-admin-v2.json:1678-1721)
func (d *driver) StageLayout(ctx context.Context, change driverpkg.LayoutChange) (driverpkg.LayoutDiff, error) {
	var remove bool
	var newRole *layoutNodeRole

	if change.Role != nil && (*change.Role == "gateway" || *change.Role == "storage") ||
		change.Zone != nil || change.Capacity != nil || len(change.Tags) > 0 {
		gw := false
		if change.Role != nil && *change.Role == "gateway" {
			gw = true
		}

		newRole = &layoutNodeRole{
			ID:       change.NodeID,
			Zone:     "",
			Tags:     []string{},
			Capacity: change.Capacity,
			Gateway:  &gw,
		}

		if change.Zone != nil {
			newRole.Zone = *change.Zone
		}

		if len(change.Tags) > 0 {
			newRole.Tags = change.Tags
		}
	} else if change.Role != nil && *change.Role == "remove" {
		remove = true
	}

	updateReq := updateClusterLayoutRequest{
		NodeID:  change.NodeID,
		NewRole: newRole,
		Gateway: nil,
		Remove:  remove,
	}

	var resp getClusterLayoutResponse
	if err := d.client.do(ctx, "POST", "/v2/UpdateClusterLayout", updateReq, &resp); err != nil {
		return driverpkg.LayoutDiff{}, err
	}

	diff := computeLayoutDiff(resp.Version, resp.Roles)
	return diff, nil
}

// ApplyLayout applies the currently staged layout changes.
// Endpoint: POST /v2/ApplyClusterLayout (garage-admin-v2.json:163-196)
func (d *driver) ApplyLayout(ctx context.Context) error {
	var currentResp getClusterLayoutResponse
	if err := d.client.do(ctx, "GET", "/v2/GetClusterLayout", nil, &currentResp); err != nil {
		return err
	}

	newVersion := currentResp.Version + 1

	reqBody := applyClusterLayoutRequest{
		Version: newVersion,
	}

	var resp applyClusterLayoutResponse
	if err := d.client.do(ctx, "POST", "/v2/ApplyClusterLayout", reqBody, &resp); err != nil {
		return err
	}

	return nil
}

// RevertLayout discards all staged layout changes.
// Endpoint: POST /v2/RevertClusterLayout (garage-admin-v2.json:1480-1503)
func (d *driver) RevertLayout(ctx context.Context) error {
	var currentResp getClusterLayoutResponse
	if err := d.client.do(ctx, "GET", "/v2/GetClusterLayout", nil, &currentResp); err != nil {
		return err
	}

	reqBody := revertClusterLayoutRequest{
		Version: currentResp.Version,
	}

	var resp revertClusterLayoutResponse
	if err := d.client.do(ctx, "POST", "/v2/RevertClusterLayout", reqBody, &resp); err != nil {
		return err
	}

	return nil
}

// computeLayoutDiff computes the diff between current and staged layout.
func computeLayoutDiff(currentVersion int64, roles []layoutNodeRole) driverpkg.LayoutDiff {
	return driverpkg.LayoutDiff{}
}

// Response types for Garage API

type getClusterHealthResponse struct {
	Status         string `json:"status"`
	ConnectedNodes int64  `json:"connectedNodes"`
	KnownNodes     int64  `json:"knownNodes"`
	StorageNodes   int64  `json:"storageNodes"`
	StorageNodesUp int64  `json:"storageNodesUp"`
	Partitions     int64  `json:"partitions"`
	PartitionsQuorum int64 `json:"partitionsQuorum"`
	PartitionsAllOk  int64 `json:"partitionsAllOk"`
}

type getClusterStatusResponse struct {
	NodeID        string             `json:"nodeId,omitempty"`
	Version       string             `json:"version,omitempty"`
	LiveNodes     []string           `json:"liveNodes,omitempty"`
	CurrentLayout clusterLayoutVersion `json:"currentLayout,omitempty"`
	StagedChanges *clusterLayoutVersion `json:"stagedChanges,omitempty"`
	Nodes         []nodeResp         `json:"nodes"`
}

type nodeResp struct {
	ID            string       `json:"id"`
	Addr          string       `json:"addr,omitempty"`
	Hostname      string       `json:"hostname,omitempty"`
	GarageVersion string       `json:"garageVersion,omitempty"`
	IsUp          bool         `json:"isUp"`
	Draining      bool         `json:"draining"`
	Role          *nodeRole    `json:"role,omitempty"`
}

type nodeRole struct {
	NodeAssignedRole *nodeAssignedRole `json:"storageNode,omitempty"`
	Gateway          *bool             `json:"gateway,omitempty"`
}

type nodeAssignedRole struct {
	ID              string   `json:"id"`
	Zone            string   `json:"zone"`
	Tags            []string `json:"tags"`
	Capacity        *int64   `json:"capacity,omitempty"`
	Gateway         *bool    `json:"gateway,omitempty"`
	StoredPartitions *int64  `json:"storedPartitions,omitempty"`
	UsableCapacity  *int64   `json:"usableCapacity,omitempty"`
}

type clusterLayoutVersion struct {
	Version int64 `json:"version"`
	Status  string `json:"status"`
}

type stagedRoleChange struct {
	ID        string      `json:"nodeId"`
	NewRole   *nodeRole   `json:"newRole,omitempty"`
	Remove    bool        `json:"remove,omitempty"`
}

// Request/Response types for StageLayout, ApplyLayout, RevertLayout

type updateClusterLayoutRequest struct {
	NodeID  string            `json:"nodeId"`
	NewRole *layoutNodeRole   `json:"storageNode,omitempty"`
	Gateway *bool             `json:"gateway,omitempty"`
	Remove  bool              `json:"remove,omitempty"`
}

type applyClusterLayoutRequest struct {
	Version int64 `json:"version"`
}

type applyClusterLayoutResponse struct {
	Layout     getClusterLayoutResponse `json:"layout"`
	Message    []string                 `json:"message"`
	Statistics *computationStat         `json:"statistics,omitempty"`
}

type revertClusterLayoutRequest struct {
	Version int64 `json:"version"`
}

type revertClusterLayoutResponse struct{}

type computationStat struct {
	ReplicationFactor           int               `json:"replicationFactor"`
	EffectiveZoneRedundancy     int               `json:"effectiveZoneRedundancy"`
	PartitionSize               int64             `json:"partitionSize"`
	LowPartitionSize            bool              `json:"lowPartitionSize"`
	UsableCapacity              int64             `json:"usableCapacity"`
	TotalCapacity               int64             `json:"totalCapacity"`
	EffectiveCapacity           int64             `json:"effectiveCapacity"`
	LowUsableCapacity           bool              `json:"lowUsableCapacity"`
	Zones                       []computationStatZone `json:"zones"`
}

type computationStatZone struct {
	Name                    string  `json:"name"`
	StorageNodes            int64   `json:"storageNodes"`
	TotalCapacity           int64   `json:"totalCapacity"`
	UsableCapacity          int64   `json:"usableCapacity"`
}


type layoutNodeRole struct {
	ID               string   `json:"id"`
	Zone             string   `json:"zone"`
	Tags             []string `json:"tags"`
	Capacity         *int64   `json:"capacity,omitempty"`
	Gateway          *bool    `json:"gateway,omitempty"`
}

type getClusterLayoutResponse struct {
	Version            int64                 `json:"version"`
	Roles              []layoutNodeRole      `json:"roles"`
	StagedRoleChanges  *[]stagedRoleChange   `json:"stagedRoleChanges,omitempty"`
}
