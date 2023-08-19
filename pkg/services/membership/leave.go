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

package membership

import (
	"time"

	v1 "github.com/webmeshproj/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/webmeshproj/webmesh/pkg/context"
	"github.com/webmeshproj/webmesh/pkg/meshdb/peers"
	"github.com/webmeshproj/webmesh/pkg/services/leaderproxy"
)

func (s *Server) Leave(ctx context.Context, req *v1.LeaveRequest) (*v1.LeaveResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if !s.store.Raft().IsLeader() {
		return nil, status.Errorf(codes.FailedPrecondition, "not leader")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Check that the node is indeed who they say they are
	if !s.insecure {
		if proxiedFor, ok := leaderproxy.ProxiedFor(ctx); ok {
			if proxiedFor != req.GetId() {
				return nil, status.Errorf(codes.PermissionDenied, "proxied for %s, not %s", proxiedFor, req.GetId())
			}
		} else {
			if peer, ok := context.AuthenticatedCallerFrom(ctx); ok {
				if peer != req.GetId() {
					return nil, status.Errorf(codes.PermissionDenied, "peer id %s, not %s", peer, req.GetId())
				}
			} else {
				return nil, status.Error(codes.PermissionDenied, "no peer authentication info in context")
			}
		}
	}

	// Send a barrier afterwards to sync the cluster
	// Check if they were a raft member
	cfg, err := s.store.Raft().Configuration()
	if err != nil {
		// Should never happen
		return nil, status.Errorf(codes.Internal, "failed to get configuration: %v", err)
	}
	var raftMember bool
	for _, srv := range cfg.Servers {
		if string(srv.ID) == req.GetId() {
			// They were a raft member, so remove them
			raftMember = true
			break
		}
	}
	if raftMember {
		defer func() {
			_, _ = s.store.Raft().Barrier(ctx, time.Second*15)
		}()
		s.log.Info("Removing mesh node from raft", "id", req.GetId())
		err := s.store.Raft().RemoveServer(ctx, req.GetId(), false)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to remove raft member: %v", err)
		}
	}
	s.log.Info("Removing mesh node from peers DB", "id", req.GetId())
	err = peers.New(s.store.Storage()).Delete(ctx, req.GetId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete peer: %v", err)
	}
	return &v1.LeaveResponse{}, nil
}
