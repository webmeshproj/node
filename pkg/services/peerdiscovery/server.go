/*
Copyright 2023 Avi Zimmerman <avi.zimmerman@gmail.com>

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

// Package peerdiscovery contains the webmesh PeerDiscovery API service.
package peerdiscovery

import (
	"github.com/hashicorp/raft"
	v1 "github.com/webmeshproj/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/webmeshproj/node/pkg/context"
	"github.com/webmeshproj/node/pkg/meshdb"
	"github.com/webmeshproj/node/pkg/meshdb/networking"
	"github.com/webmeshproj/node/pkg/meshdb/state"
)

// Server is the webmesh PeerDiscovery service.
type Server struct {
	v1.UnimplementedPeerDiscoveryServer
	store meshdb.Store
	state state.State
}

// NewServer returns a new Server.
func NewServer(store meshdb.Store) *Server {
	return &Server{
		store: store,
		state: state.New(store.Storage()),
	}
}

func (s *Server) ListPeers(ctx context.Context, _ *emptypb.Empty) (*v1.ListRaftPeersResponse, error) {
	peers, err := s.state.ListPublicRPCAddresses(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list peers: %v", err)
	}
	leader, err := s.store.Leader()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get leader: %v", err)
	}
	config := s.store.Raft().GetConfiguration().Configuration()
	out := make([]*v1.RaftPeer, 0)
	for id, addr := range peers {
		out = append(out, &v1.RaftPeer{
			Id:      id,
			Address: addr.String(),
			Voter: func() bool {
				for _, s := range config.Servers {
					if string(s.ID) == id {
						return s.Suffrage == raft.Voter
					}
				}
				return false
			}(),
			Leader: id == string(leader),
		})
	}
	// If the caller is authenticated, we should filter this down to only the
	// peers the caller has access to.
	caller, ok := context.AuthenticatedCallerFrom(ctx)
	if ok {
		n := networking.New(s.store.Storage())
		nacls, err := n.ListNetworkACLs(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to list network ACLs: %v", err)
		}
		allowedPeers := make([]*v1.RaftPeer, 0)
		action := v1.NetworkAction{
			SrcNode: caller,
		}
		for _, peer := range out {
			action.DstNode = peer.Id
			if !nacls.Accept(ctx, &action) {
				continue
			}
			allowedPeers = append(allowedPeers, peer)
		}
		out = allowedPeers
	}
	return &v1.ListRaftPeersResponse{Peers: out}, nil
}
