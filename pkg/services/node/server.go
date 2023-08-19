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

// Package node contains the webmesh node service.
package node

import (
	"log/slog"
	"time"

	v1 "github.com/webmeshproj/api/v1"

	"github.com/webmeshproj/webmesh/pkg/meshdb"
	"github.com/webmeshproj/webmesh/pkg/services/rbac"
)

// Server is the webmesh node service.
type Server struct {
	v1.UnimplementedNodeServer

	store     meshdb.Store
	rbacEval  rbac.Evaluator
	features  []v1.Feature
	startedAt time.Time
	// insecure flags that no authentication plugins are enabled.
	insecure bool
	log      *slog.Logger
}

// NewServer returns a new Server. Features are used for returning what features are enabled.
// It is the callers responsibility to ensure those servers are registered on the node.
// Insecure is used to disable authorization.
func NewServer(store meshdb.Store, features []v1.Feature, insecure bool) *Server {
	var rbaceval rbac.Evaluator
	if insecure {
		rbaceval = rbac.NewNoopEvaluator()
	} else {
		rbaceval = rbac.NewStoreEvaluator(store)
	}
	return &Server{
		store:     store,
		rbacEval:  rbaceval,
		features:  features,
		startedAt: time.Now(),
		insecure:  insecure,
		log:       slog.Default().With("component", "node-server"),
	}
}
