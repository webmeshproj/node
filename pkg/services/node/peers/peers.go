/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package peers contains an interface for managing nodes in the mesh.
package peers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"gitlab.com/webmesh/node/pkg/db"
	"gitlab.com/webmesh/node/pkg/store"
)

// ErrNodeNotFound is returned when a node is not found.
var ErrNodeNotFound = errors.New("node not found")

// Peers is the peers interface.
type Peers interface {
	// Create creates a new node.
	Create(ctx context.Context, opts *CreateOptions) (*Node, error)
	// AssignASN assigns an ASN to a node.
	AssignASN(ctx context.Context, id string) (uint32, error)
	// UnassignASN unassigns an ASN from a node.
	UnassignASN(ctx context.Context, id string) error
	// Get gets a node by ID.
	Get(ctx context.Context, id string) (*Node, error)
	// Update updates a node.
	Update(ctx context.Context, node *Node) (*Node, error)
	// List lists all nodes.
	List(ctx context.Context) ([]Node, error)
	// ListPeers lists all peers for a node.
	ListPeers(ctx context.Context, nodeID string) ([]Node, error)
}

// Node represents a node. Not all fields are populated in all contexts.
// A fully populated node is returned by Get and List.
type Node struct {
	// ID is the node's ID.
	ID string
	// PublicKey is the node's public key.
	PublicKey wgtypes.Key
	// Endpoint is the node's public endpoint.
	Endpoint netip.AddrPort
	// AllowedIPs is the node's allowed IPs.
	AllowedIPs []string
	// AvailableZones is the node's available zones.
	AvailableZones []string
	// ASN is the node's ASN.
	ASN uint32
	// PrivateIPv4 is the node's private IPv4 address.
	PrivateIPv4 netip.Prefix
	// NetworkIPv6 is the node's IPv6 network.
	NetworkIPv6 netip.Prefix
	// GRPCPort is the node's GRPC port.
	GRPCPort int
	// RaftPort is the node's Raft port.
	RaftPort int
	// LastHeartbeatAt is the last time the node produced a heartbeat.
	LastHeartbeatAt time.Time
	// CreatedAt is the time the node was created.
	CreatedAt time.Time
}

// CreateOptions are options for creating a node.
type CreateOptions struct {
	// ID is the node's ID.
	ID string
	// PublicKey is the node's public key.
	PublicKey wgtypes.Key
	// Endpoint is the public endpoint of the node.
	Endpoint netip.AddrPort
	// NetworkIPv6 is true if the node's network is IPv6.
	NetworkIPv6 netip.Prefix
	// GRPCPort is the node's GRPC port.
	GRPCPort int
	// RaftPort is the node's Raft port.
	RaftPort int
	// AllowedIPs is the node's allowed IPs.
	AllowedIPs []string
	// AvailableZones is the node's available zones.
	AvailableZones []string
	// AssignASN is true if an ASN should be assigned to the node.
	AssignASN bool
}

// New returns a new Peers interface.
func New(store store.Store) Peers {
	return &peers{store}
}

type peers struct {
	store store.Store
}

// Create creates a new node.
func (p *peers) Create(ctx context.Context, opts *CreateOptions) (*Node, error) {
	params := db.CreateNodeParams{
		ID: opts.ID,
		PublicKey: sql.NullString{
			String: opts.PublicKey.String(),
			Valid:  true,
		},
		GrpcPort: int64(opts.GRPCPort),
		RaftPort: int64(opts.RaftPort),
	}
	if opts.NetworkIPv6.IsValid() {
		params.NetworkIpv6 = sql.NullString{
			String: opts.NetworkIPv6.String(),
			Valid:  true,
		}
	}
	if opts.Endpoint.IsValid() {
		params.Endpoint = sql.NullString{
			String: opts.Endpoint.String(),
			Valid:  true,
		}
	}
	if len(opts.AllowedIPs) > 0 {
		params.AllowedIps = sql.NullString{
			String: strings.Join(opts.AllowedIPs, ","),
			Valid:  true,
		}
	}
	if len(opts.AvailableZones) > 0 {
		params.AvailableZones = sql.NullString{
			String: strings.Join(opts.AvailableZones, ","),
			Valid:  true,
		}
	}
	q := db.New(p.store.DB())
	node, err := q.CreateNode(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create node: %w", err)
	}
	out, err := nodeModelToNode(&node)
	if err != nil {
		return nil, fmt.Errorf("failed to convert node model to node: %w", err)
	}
	if opts.AssignASN {
		asn, err := p.AssignASN(ctx, opts.PublicKey.String())
		if err != nil {
			return nil, fmt.Errorf("failed to assign node ASN: %w", err)
		}
		out.ASN = uint32(asn)
	}
	return out, nil
}

// AssignASN assigns an ASN to a node.
func (p *peers) AssignASN(ctx context.Context, publicKey string) (uint32, error) {
	q := db.New(p.store.DB())
	asn, err := q.AssignNodeASN(ctx, publicKey)
	if err != nil {
		return 0, fmt.Errorf("failed to assign ASN: %w", err)
	}
	return uint32(asn.Asn), nil
}

// UnassignASN unassigns an ASN from a node.
func (p *peers) UnassignASN(ctx context.Context, publicKey string) error {
	q := db.New(p.store.DB())
	err := q.UnassignNodeASN(ctx, publicKey)
	if err != nil {
		return fmt.Errorf("failed to unassign ASN: %w", err)
	}
	return nil
}

// Get gets a node by public key.
func (p *peers) Get(ctx context.Context, id string) (*Node, error) {
	q := db.New(p.store.WeakDB())
	node, err := q.GetNode(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNodeNotFound
		}
		return nil, err
	}
	key, err := wgtypes.ParseKey(node.PublicKey.String)
	if err != nil {
		return nil, fmt.Errorf("failed to parse node public key: %w", err)
	}
	var endpoint netip.AddrPort
	if node.Endpoint.Valid {
		endpoint, err = netip.ParseAddrPort(node.Endpoint.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse node endpoint: %w", err)
		}
	}
	var privateIPv4, privateIPv6 netip.Prefix
	if node.PrivateAddressV4 != "" {
		privateIPv4, err = netip.ParsePrefix(node.PrivateAddressV4)
		if err != nil {
			return nil, fmt.Errorf("failed to parse node private IPv4: %w", err)
		}
	}
	if node.NetworkIpv6.Valid {
		privateIPv6, err = netip.ParsePrefix(node.NetworkIpv6.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse node private IPv6: %w", err)
		}
	}
	return &Node{
		ID:        node.ID,
		PublicKey: key,
		Endpoint:  endpoint,
		AllowedIPs: func() []string {
			if !node.AllowedIps.Valid {
				return nil
			}
			return strings.Split(node.AllowedIps.String, ",")
		}(),
		AvailableZones: func() []string {
			if !node.AvailableZones.Valid {
				return nil
			}
			return strings.Split(node.AvailableZones.String, ",")
		}(),
		ASN:             uint32(node.Asn),
		PrivateIPv4:     privateIPv4,
		NetworkIPv6:     privateIPv6,
		GRPCPort:        int(node.GrpcPort),
		RaftPort:        int(node.RaftPort),
		LastHeartbeatAt: node.LastHeartbeatAt,
		CreatedAt:       node.CreatedAt,
	}, nil
}

// Update updates a node.
func (p *peers) Update(ctx context.Context, node *Node) (*Node, error) {
	params := db.UpdateNodeParams{
		ID:       node.ID,
		GrpcPort: int64(node.GRPCPort),
		RaftPort: int64(node.RaftPort),
		PublicKey: sql.NullString{
			String: node.PublicKey.String(),
			Valid:  true,
		},
	}
	if node.Endpoint.IsValid() {
		params.Endpoint = sql.NullString{
			String: node.Endpoint.String(),
			Valid:  true,
		}
	}
	if len(node.AllowedIPs) > 0 {
		params.AllowedIps = sql.NullString{
			String: strings.Join(node.AllowedIPs, ","),
			Valid:  true,
		}
	}
	if len(node.AvailableZones) > 0 {
		params.AvailableZones = sql.NullString{
			String: strings.Join(node.AvailableZones, ","),
			Valid:  true,
		}
	}
	if node.NetworkIPv6.IsValid() {
		params.NetworkIpv6 = sql.NullString{
			String: node.NetworkIPv6.String(),
			Valid:  true,
		}
	}
	q := db.New(p.store.DB())
	updated, err := q.UpdateNode(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to update node: %w", err)
	}
	out, err := nodeModelToNode(&updated)
	if err != nil {
		return nil, fmt.Errorf("failed to convert node model to node: %w", err)
	}
	if node.ASN != 0 {
		// Pass the ASN through if it was provided.
		// We assign and remove these via other methods.
		out.ASN = node.ASN
	}
	return out, nil
}

// List lists all nodes.
func (p *peers) List(ctx context.Context) ([]Node, error) {
	q := db.New(p.store.WeakDB())
	nodes, err := q.ListNodes(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []Node{}, nil
		}
		return nil, err
	}
	out := make([]Node, len(nodes))
	for i, node := range nodes {
		key, err := wgtypes.ParseKey(node.PublicKey.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse node public key: %w", err)
		}
		var endpoint netip.AddrPort
		if node.Endpoint.Valid {
			endpoint, err = netip.ParseAddrPort(node.Endpoint.String)
			if err != nil {
				return nil, fmt.Errorf("failed to parse node endpoint: %w", err)
			}
		}
		var networkv4, networkv6 netip.Prefix
		if node.PrivateAddressV4 != "" {
			networkv4, err = netip.ParsePrefix(node.PrivateAddressV4)
			if err != nil {
				return nil, fmt.Errorf("failed to parse node network IPv4: %w", err)
			}
		}
		if node.NetworkIpv6.Valid {
			networkv6, err = netip.ParsePrefix(node.NetworkIpv6.String)
			if err != nil {
				return nil, fmt.Errorf("failed to parse node network IPv6: %w", err)
			}
		}
		out[i] = Node{
			ID:              node.ID,
			PublicKey:       key,
			Endpoint:        endpoint,
			ASN:             uint32(node.Asn),
			PrivateIPv4:     networkv4,
			NetworkIPv6:     networkv6,
			GRPCPort:        int(node.GrpcPort),
			RaftPort:        int(node.RaftPort),
			LastHeartbeatAt: node.LastHeartbeatAt,
			CreatedAt:       node.CreatedAt,
			AllowedIPs: func() []string {
				if !node.AllowedIps.Valid {
					return nil
				}
				return strings.Split(node.AllowedIps.String, ",")
			}(),
			AvailableZones: func() []string {
				if !node.AvailableZones.Valid {
					return nil
				}
				return strings.Split(node.AvailableZones.String, ",")
			}(),
		}
	}
	return out, nil
}

// ListPeers lists all peers for a node.
func (p *peers) ListPeers(ctx context.Context, nodeID string) ([]Node, error) {
	q := db.New(p.store.WeakDB())
	nodePeers, err := q.ListNodePeers(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	peers := make([]Node, len(nodePeers))
	for i, peer := range nodePeers {
		key, err := wgtypes.ParseKey(peer.PublicKey.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse node public key: %w", err)
		}
		var endpoint netip.AddrPort
		if peer.Endpoint.Valid {
			endpoint, err = netip.ParseAddrPort(peer.Endpoint.String)
			if err != nil {
				return nil, fmt.Errorf("failed to parse node endpoint: %w", err)
			}
		}
		var networkv4, networkv6 netip.Prefix
		if peer.PrivateAddressV4 != "" {
			networkv4, err = netip.ParsePrefix(peer.PrivateAddressV4)
			if err != nil {
				return nil, fmt.Errorf("failed to parse node network IPv4: %w", err)
			}
		}
		if peer.NetworkIpv6.Valid {
			networkv6, err = netip.ParsePrefix(peer.NetworkIpv6.String)
			if err != nil {
				return nil, fmt.Errorf("failed to parse node network IPv6: %w", err)
			}
		}
		peers[i] = Node{
			ID:              peer.ID,
			PublicKey:       key,
			Endpoint:        endpoint,
			ASN:             uint32(peer.Asn),
			PrivateIPv4:     networkv4,
			NetworkIPv6:     networkv6,
			GRPCPort:        int(peer.GrpcPort),
			RaftPort:        int(peer.RaftPort),
			LastHeartbeatAt: peer.LastHeartbeatAt,
			CreatedAt:       peer.CreatedAt,
			AllowedIPs: func() []string {
				if !peer.AllowedIps.Valid {
					return nil
				}
				return strings.Split(peer.AllowedIps.String, ",")
			}(),
			AvailableZones: func() []string {
				if !peer.AvailableZones.Valid {
					return nil
				}
				return strings.Split(peer.AvailableZones.String, ",")
			}(),
		}
	}
	return peers, nil
}

func nodeModelToNode(node *db.Node) (*Node, error) {
	var err error
	var endpoint netip.AddrPort
	key, err := wgtypes.ParseKey(node.PublicKey.String)
	if err != nil {
		return nil, fmt.Errorf("failed to parse node public key: %w", err)
	}
	if node.Endpoint.Valid {
		endpoint, err = netip.ParseAddrPort(node.Endpoint.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse endpoint: %w", err)
		}
	}
	var networkV6 netip.Prefix
	if node.NetworkIpv6.Valid {
		networkV6, err = netip.ParsePrefix(node.NetworkIpv6.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse network IPv6: %w", err)
		}
	}
	return &Node{
		ID:        node.ID,
		PublicKey: key,
		Endpoint:  endpoint,
		AllowedIPs: func() []string {
			if !node.AllowedIps.Valid {
				return nil
			}
			return strings.Split(node.AllowedIps.String, ",")
		}(),
		AvailableZones: func() []string {
			if !node.AvailableZones.Valid {
				return nil
			}
			return strings.Split(node.AvailableZones.String, ",")
		}(),
		NetworkIPv6: networkV6,
		GRPCPort:    int(node.GrpcPort),
		RaftPort:    int(node.RaftPort),
		CreatedAt:   node.CreatedAt,
	}, nil
}
