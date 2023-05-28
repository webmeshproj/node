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

package meshapi

import (
	"context"

	v1 "gitlab.com/webmesh/api/v1"
	"golang.org/x/exp/slog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *Server) ListNodes(ctx context.Context, req *emptypb.Empty) (*v1.NodeList, error) {
	node, err := s.peers.List(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get node: %v", err)
	}
	servers := s.store.Raft().GetConfiguration().Configuration().Servers
	leader, err := s.store.Leader()
	if err != nil {
		s.log.Error("failed to get leader", slog.String("error", err.Error()))
	}
	out := make([]*v1.MeshNode, len(node))
	for i, n := range node {
		out[i] = dbNodeToAPINode(&n, leader, servers)
	}
	return &v1.NodeList{
		Nodes: out,
	}, nil
}