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

package mesh

import (
	"context"

	"golang.org/x/exp/slog"
)

// Close closes the store.
func (s *meshStore) Close() error {
	if !s.open.Load() {
		return ErrNotOpen
	}
	ctx := context.Background()
	defer s.open.Store(false)
	s.kvSubCancel()
	if s.nw != nil {
		// Do this last so that we don't lose connectivity to the network
		defer func() {
			s.log.Debug("closing network manager")
			if err := s.nw.Close(ctx); err != nil {
				s.log.Error("error clearing firewall rules", slog.String("error", err.Error()))
			}
		}()
	}
	if s.plugins != nil {
		// Close the plugins
		s.log.Debug("closing plugin manager")
		err := s.plugins.Close()
		if err != nil {
			s.log.Error("error closing plugins", slog.String("error", err.Error()))
		}
	}
	if s.raft != nil {
		err := s.raft.Stop(ctx)
		if err != nil {
			s.log.Error("error stopping raft", slog.String("error", err.Error()))
		}
	}
	if s.opts.Raft.LeaveOnShutdown {
		if err := s.leaveCluster(ctx); err != nil {
			s.log.Error("error leaving cluster", slog.String("error", err.Error()))
		}
	}
	s.log.Debug("all services shut down")
	return nil
}