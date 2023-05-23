// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.18.0

package db

import (
	"context"
)

type Querier interface {
	AssignNodeASN(ctx context.Context, nodeID string) (Asn, error)
	CreateNode(ctx context.Context, arg CreateNodeParams) (Node, error)
	GetCurrentWireguardKey(ctx context.Context) (string, error)
	GetIPv4Prefix(ctx context.Context) (string, error)
	GetNode(ctx context.Context, id string) (GetNodeRow, error)
	GetNodePeer(ctx context.Context, id string) (GetNodePeerRow, error)
	GetRaftState(ctx context.Context) (GetRaftStateRow, error)
	GetULAPrefix(ctx context.Context) (string, error)
	InsertNodeLease(ctx context.Context, arg InsertNodeLeaseParams) (Lease, error)
	ListAllocatedIPv4(ctx context.Context) ([]string, error)
	ListNodePeers(ctx context.Context, id string) ([]ListNodePeersRow, error)
	ListNodes(ctx context.Context) ([]ListNodesRow, error)
	RecordHeartbeat(ctx context.Context, arg RecordHeartbeatParams) error
	ReleaseNodeLease(ctx context.Context, nodeID string) error
	RenewNodeLease(ctx context.Context, arg RenewNodeLeaseParams) error
	SetCurrentRaftTerm(ctx context.Context, value string) error
	SetCurrentWireguardKey(ctx context.Context, value string) error
	SetIPv4Prefix(ctx context.Context, value string) error
	SetLastAppliedRaftIndex(ctx context.Context, value string) error
	SetULAPrefix(ctx context.Context, value string) error
	UnassignNodeASN(ctx context.Context, nodeID string) error
	UpdateNode(ctx context.Context, arg UpdateNodeParams) (Node, error)
}

var _ Querier = (*Queries)(nil)
