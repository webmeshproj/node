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

// Package admin provides the admin gRPC server.
package admin

import (
	v1 "github.com/webmeshproj/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/webmeshproj/webmesh/pkg/context"
	"github.com/webmeshproj/webmesh/pkg/storage/meshdb/networking"
)

func (s *Server) GetRoute(ctx context.Context, route *v1.Route) (*v1.Route, error) {
	if route.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "route name is required")
	}
	rt, err := s.networking.GetRoute(ctx, route.GetName())
	if err != nil {
		if err == networking.ErrRouteNotFound {
			return nil, status.Errorf(codes.NotFound, "network route %q not found", route.GetName())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return rt.Proto(), nil
}
