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
	"bytes"
	"context"

	v1 "gitlab.com/webmesh/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *Server) GetMeshGraph(ctx context.Context, _ *emptypb.Empty) (*v1.MeshGraph, error) {
	nodeIDs, err := s.peers.ListIDs(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list node IDs: %v", err)
	}
	edges, err := s.peers.Graph().Edges()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list edges: %v", err)
	}
	out := &v1.MeshGraph{
		Nodes: nodeIDs,
		Edges: make([]*v1.MeshEdge, len(edges)),
	}
	for i, edge := range edges {
		out.Edges[i] = &v1.MeshEdge{
			Source: edge.Source,
			Target: edge.Target,
			Weight: int32(edge.Properties.Weight),
		}
	}
	var buf bytes.Buffer
	err = s.peers.DrawGraph(ctx, &buf)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to draw graph: %v", err)
	}
	out.Dot = buf.String()
	return out, nil
}