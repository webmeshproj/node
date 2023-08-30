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
	"fmt"
	"io"
	"log/slog"
	"net/netip"

	v1 "github.com/webmeshproj/api/v1"

	"github.com/webmeshproj/webmesh/pkg/net"
	"github.com/webmeshproj/webmesh/pkg/net/transport"
	"github.com/webmeshproj/webmesh/pkg/plugins"
	"github.com/webmeshproj/webmesh/pkg/raft"
)

// ConnectOptions are options for opening the connection to the mesh.
type ConnectOptions struct {
	// Raft is the Raft instance. It should not be closed.
	Raft raft.Raft
	// Features are the features to broadcast to others in the mesh.
	Features []v1.Feature
	// Plugins is a map of plugins to use.
	Plugins map[string]plugins.Plugin
	// JoinRoundTripper is the round tripper to use for joining the mesh.
	JoinRoundTripper transport.JoinRoundTripper
	// NetworkOptions are options for the network manager
	NetworkOptions net.Options
	// Discovery are options for using peer discovery
	Discovery *DiscoveryOptions
	// MaxJoinRetries is the maximum number of join retries.
	MaxJoinRetries int
	// GRPCAdvertisePort is the port to advertise for gRPC connections.
	GRPCAdvertisePort int
	// MeshDNSAdvertisePort is the port to advertise for MeshDNS connections.
	MeshDNSAdvertisePort int
	// PrimaryEndpoint is a publicly accessible address to broadcast as the
	// primary endpoint for this node. This is used for discovery and
	// connection into the mesh. If left unset, the node will be assumed to be
	// behind a NAT.
	PrimaryEndpoint netip.Addr
	// WireGuardEndpoints are endpoints to advertise for WireGuard connections.
	WireGuardEndpoints []netip.AddrPort
	// RequestVote requests a vote in Raft elections.
	RequestVote bool
	// RequestObserver requests to be an observer in Raft elections.
	RequestObserver bool
	// Routes are additional routes to broadcast to the mesh.
	Routes []netip.Prefix
	// DirectPeers are additional peers to connect to directly.
	DirectPeers []string
	// Bootstrap are options for bootstrapping the mesh when connecting for
	// the first time.
	Bootstrap *BootstrapOptions
	// PreferIPv6 is true if IPv6 should be preferred over IPv4.
	PreferIPv6 bool
}

// BootstrapOptions are options for bootstrapping the mesh when connecting for
// the first time.
type BootstrapOptions struct {
	// Transport is the transport to use for bootstrapping the mesh.
	Transport transport.BootstrapTransport
	// IPv4Network is the IPv4 Network to use for the mesh. Defaults to
	// DefaultIPv4Network.
	IPv4Network string
	// MeshDomain is the domain of the mesh network. Defaults to
	// DefaultMeshDomain.
	MeshDomain string
	// Admin is the ID of the administrator node. Defaults to "admin".
	Admin string
	// Servers are other node IDs that were bootstrapped with the same
	// transport.
	Servers []string
	// Voters are additional node IDs to assign voter permissions to.
	Voters []string
	// DisableRBAC disables RBAC for the mesh.
	DisableRBAC bool
	// DefaultNetworkPolicy is the default network policy for the mesh.
	// If empty, DefaultNetworkPolicy will be used.
	DefaultNetworkPolicy string
	// Force is true if the node should force bootstrap.
	Force bool
}

// Connect opens the connection to the mesh.
func (s *meshStore) Connect(ctx context.Context, opts ConnectOptions) (err error) {
	if s.open.Load() {
		return ErrOpen
	}
	s.raft = opts.Raft
	// If we still don't have a node id, use raft's node id.
	if s.ID() == "" {
		s.nodeID = s.raft.ID()
	}
	log := s.log
	// Create the plugin manager
	var pluginopts plugins.Options
	pluginopts.Storage = s.Storage()
	pluginopts.Plugins = opts.Plugins
	s.plugins, err = plugins.NewManager(ctx, pluginopts)
	if err != nil {
		return fmt.Errorf("failed to load plugins: %w", err)
	}
	// Create the raft node
	s.raft.OnObservation(s.newObserver())
	s.raft.OnSnapshotRestore(func(ctx context.Context, meta *raft.SnapshotMeta, data io.ReadCloser) {
		// Dispatch the snapshot to any storage plugins.
		if err = s.plugins.ApplySnapshot(ctx, meta, data); err != nil {
			// This is non-fatal for now.
			s.log.Error("failed to apply snapshot to plugins", slog.String("error", err.Error()))
		}
	})
	s.raft.OnApply(func(ctx context.Context, term, index uint64, log *v1.RaftLogEntry) {
		// Dispatch the log entry to any storage plugins.
		if _, err := s.plugins.ApplyRaftLog(ctx, &v1.StoreLogRequest{
			Term:  term,
			Index: index,
			Log:   log,
		}); err != nil {
			// This is non-fatal for now.
			s.log.Error("failed to apply log to plugins", slog.String("error", err.Error()))
		}
	})
	// Start serving storage queries for plugins.
	go s.plugins.ServeStorage(s.raft.Storage())
	handleErr := func(cause error) error {
		s.kvSubCancel()
		log.Error("failed to open store", slog.String("error", err.Error()))
		perr := s.plugins.Close()
		if perr != nil {
			log.Error("failed to close plugin manager", slog.String("error", perr.Error()))
		}
		cerr := s.raft.Stop(ctx)
		if cerr != nil {
			log.Error("failed to stop raft node", slog.String("error", cerr.Error()))
		}
		return cause
	}
	// Create the network manager
	opts.NetworkOptions.NodeID = s.ID()
	opts.NetworkOptions.RaftPort = int(s.raft.ListenPort())
	s.nw = net.New(s.Storage(), opts.NetworkOptions)
	// At this point we are open for business.
	s.open.Store(true)
	key, err := s.loadWireGuardKey(ctx)
	if err != nil {
		return fmt.Errorf("load wireguard key: %w", err)
	}
	if opts.Bootstrap != nil {
		// Attempt bootstrap.
		if err = s.bootstrap(ctx, opts, key); err != nil {
			return handleErr(fmt.Errorf("bootstrap: %w", err))
		}
	} else if opts.JoinRoundTripper != nil {
		// Attempt to join the cluster.
		err = s.join(ctx, opts, key)
		if err != nil {
			return handleErr(fmt.Errorf("join: %w", err))
		}
	} else {
		// We neither had the bootstrap flag nor any join flags set.
		// This means we are possibly a single node cluster.
		// Recover our previous wireguard configuration and start up.
		if err := s.recoverWireguard(ctx); err != nil {
			return fmt.Errorf("recover wireguard: %w", err)
		}
	}
	// Register an update hook to watch for network changes.
	s.kvSubCancel, err = s.raft.Storage().Subscribe(context.Background(), "", s.onDBUpdate)
	if err != nil {
		return handleErr(fmt.Errorf("subscribe: %w", err))
	}
	if opts.Discovery != nil && opts.Discovery.Announce {
		err = s.AnnounceDHT(ctx, *opts.Discovery)
		if err != nil {
			return handleErr(fmt.Errorf("announce dht: %w", err))
		}
	}
	return nil
}