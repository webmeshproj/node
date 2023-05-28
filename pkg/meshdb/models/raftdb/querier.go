// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.18.0

package raftdb

import (
	"context"
)

type Querier interface {
	DeleteNode(ctx context.Context, id string) error
	DeleteNodeEdge(ctx context.Context, arg DeleteNodeEdgeParams) error
	DeleteNodeEdges(ctx context.Context, arg DeleteNodeEdgesParams) error
	DeleteRaftACL(ctx context.Context, name string) error
	DropLeases(ctx context.Context) error
	DropMeshState(ctx context.Context) error
	DropNodeEdges(ctx context.Context) error
	DropNodes(ctx context.Context) error
	DropRaftACLs(ctx context.Context) error
	DumpLeases(ctx context.Context) ([]Lease, error)
	DumpMeshState(ctx context.Context) ([]MeshState, error)
	DumpNodeEdges(ctx context.Context) ([]NodeEdge, error)
	DumpNodes(ctx context.Context) ([]Node, error)
	DumpRaftACLs(ctx context.Context) ([]RaftAcl, error)
	EitherNodeExists(ctx context.Context, arg EitherNodeExistsParams) (int64, error)
	GetIPv4Prefix(ctx context.Context) (string, error)
	GetNode(ctx context.Context, id string) (GetNodeRow, error)
	GetNodeCount(ctx context.Context) (int64, error)
	GetNodeEdge(ctx context.Context, arg GetNodeEdgeParams) (NodeEdge, error)
	GetNodePrivateRPCAddress(ctx context.Context, nodeID string) (interface{}, error)
	GetNodePublicRPCAddress(ctx context.Context, nodeID string) (interface{}, error)
	GetPeerPrivateRPCAddresses(ctx context.Context, nodeID string) ([]interface{}, error)
	GetPeerPublicRPCAddresses(ctx context.Context, nodeID string) ([]interface{}, error)
	GetRaftACL(ctx context.Context, name string) (RaftAcl, error)
	GetULAPrefix(ctx context.Context) (string, error)
	InsertNode(ctx context.Context, arg InsertNodeParams) (Node, error)
	InsertNodeEdge(ctx context.Context, arg InsertNodeEdgeParams) error
	InsertNodeLease(ctx context.Context, arg InsertNodeLeaseParams) (Lease, error)
	ListAllocatedIPv4(ctx context.Context) ([]string, error)
	ListNodeEdges(ctx context.Context) ([]NodeEdge, error)
	ListNodeIDs(ctx context.Context) ([]string, error)
	ListNodes(ctx context.Context) ([]ListNodesRow, error)
	ListPublicRPCAddresses(ctx context.Context) ([]ListPublicRPCAddressesRow, error)
	ListPublicWireguardEndpoints(ctx context.Context) ([]ListPublicWireguardEndpointsRow, error)
	ListRaftACLs(ctx context.Context) ([]RaftAcl, error)
	NodeEdgeExists(ctx context.Context, arg NodeEdgeExistsParams) (int64, error)
	NodeExists(ctx context.Context, id string) (int64, error)
	NodeHasEdges(ctx context.Context, arg NodeHasEdgesParams) (int64, error)
	PutRaftACL(ctx context.Context, arg PutRaftACLParams) error
	ReleaseNodeLease(ctx context.Context, nodeID string) error
	RestoreLease(ctx context.Context, arg RestoreLeaseParams) error
	RestoreMeshState(ctx context.Context, arg RestoreMeshStateParams) error
	RestoreNode(ctx context.Context, arg RestoreNodeParams) error
	RestoreNodeEdge(ctx context.Context, arg RestoreNodeEdgeParams) error
	RestoreRaftACL(ctx context.Context, arg RestoreRaftACLParams) error
	SetIPv4Prefix(ctx context.Context, value string) error
	SetULAPrefix(ctx context.Context, value string) error
	UpdateNode(ctx context.Context, arg UpdateNodeParams) (Node, error)
	UpdateNodeEdge(ctx context.Context, arg UpdateNodeEdgeParams) error
}

var _ Querier = (*Queries)(nil)