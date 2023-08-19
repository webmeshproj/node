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

package net

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"runtime"
	"sync"
	"time"

	v1 "github.com/webmeshproj/api/v1"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"google.golang.org/grpc"

	"github.com/webmeshproj/webmesh/pkg/context"
	"github.com/webmeshproj/webmesh/pkg/meshdb/peers"
	"github.com/webmeshproj/webmesh/pkg/net/datachannels"
	"github.com/webmeshproj/webmesh/pkg/net/endpoints"
	"github.com/webmeshproj/webmesh/pkg/net/mesh"
	"github.com/webmeshproj/webmesh/pkg/net/system"
	"github.com/webmeshproj/webmesh/pkg/net/system/dns"
	"github.com/webmeshproj/webmesh/pkg/net/system/firewall"
	"github.com/webmeshproj/webmesh/pkg/net/wireguard"
	"github.com/webmeshproj/webmesh/pkg/storage"
	"github.com/webmeshproj/webmesh/pkg/util"
)

// Options are the options for the network manager.
type Options struct {
	// NodeID is the ID of the node.
	NodeID string
	// InterfaceName is the name of the wireguard interface.
	InterfaceName string
	// ForceReplace is whether to force replace the wireguard interface.
	ForceReplace bool
	// ListenPort is the port to use for wireguard.
	ListenPort int
	// PersistentKeepAlive is the persistent keepalive to use for wireguard.
	PersistentKeepAlive time.Duration
	// ForceTUN is whether to force the use of TUN.
	ForceTUN bool
	// Modprobe is whether to use modprobe to attempt to load the wireguard kernel module.
	Modprobe bool
	// MTU is the MTU to use for the wireguard interface.
	MTU int
	// RecordMetrics is whether to enable metrics recording.
	RecordMetrics bool
	// RecordMetricsInterval is the interval to use for recording metrics.
	RecordMetricsInterval time.Duration
	// RaftPort is the port being used for raft.
	RaftPort int
	// GRPCPort is the port being used for gRPC.
	GRPCPort int
	// ZoneAwarenessID is the zone awareness ID.
	ZoneAwarenessID string
	// DialOptions are the dial options to use when calling peer nodes.
	DialOptions []grpc.DialOption
	// DisableIPv4 disables IPv4 on the interface.
	DisableIPv4 bool
	// DisableIPv6 disables IPv6 on the interface.
	DisableIPv6 bool
}

// StartOptions are the options for starting the network manager and configuring
// the wireguard interface.
type StartOptions struct {
	// Key is the wireguard key to use for the node.
	Key wgtypes.Key
	// AddressV4 is the IPv4 address to use for the node.
	AddressV4 netip.Prefix
	// AddressV6 is the IPv6 address to use for the node.
	AddressV6 netip.Prefix
	// NetworkV4 is the IPv4 network to use for the node.
	NetworkV4 netip.Prefix
	// NetworkV6 is the IPv6 network to use for the node.
	NetworkV6 netip.Prefix
}

// Manager is the interface for managing the network.
type Manager interface {
	// Start starts the network manager.
	Start(ctx context.Context, opts *StartOptions) error
	// NetworkV4 returns the current IPv4 network. The returned value may be invalid.
	NetworkV4() netip.Prefix
	// NetworkV6 returns the current IPv6 network, even if it is disabled.
	NetworkV6() netip.Prefix
	// StartMasquerade ensures that masquerading is enabled.
	StartMasquerade(ctx context.Context) error
	// AddDNSServers adds the given dns servers to the system configuration.
	AddDNSServers(ctx context.Context, servers []netip.AddrPort) error
	// RefreshDNSServers checks which peers in the database are offering DNS
	// and updates the system configuration accordingly.
	RefreshDNSServers(ctx context.Context) error
	// AddPeer adds a peer to the wireguard interface.
	AddPeer(ctx context.Context, peer *v1.WireGuardPeer, iceServers []string) error
	// RefreshPeers walks all peers in the database and ensures they are added to the wireguard interface.
	RefreshPeers(ctx context.Context) error
	// Firewall returns the firewall.
	// The firewall is only available after Start has been called.
	Firewall() firewall.Firewall
	// WireGuard returns the wireguard interface.
	// The wireguard interface is only available after Start has been called.
	WireGuard() wireguard.Interface
	// Resolver returns a net.Resolver that can be used to resolve DNS names.
	Resolver() *net.Resolver
	// Close closes the network manager and cleans up any resources.
	Close(ctx context.Context) error
}

// New creates a new network manager.
func New(store storage.Storage, opts *Options) Manager {
	return &manager{
		storage:  store,
		opts:     opts,
		iceConns: make(map[string]clientPeerConn),
	}
}

type manager struct {
	opts                 *Options
	storage              storage.Storage
	fw                   firewall.Firewall
	wg                   wireguard.Interface
	iceConns             map[string]clientPeerConn
	dnsservers           []netip.AddrPort
	networkv4, networkv6 netip.Prefix
	masquerading         bool
	dnsmu, wgmu, pcmu    sync.Mutex
}

type clientPeerConn struct {
	peerConn  *datachannels.WireGuardProxyClient
	localAddr netip.AddrPort
}

func (m *manager) NetworkV4() netip.Prefix {
	return m.networkv4
}

func (m *manager) NetworkV6() netip.Prefix {
	return m.networkv6
}

func (m *manager) Firewall() firewall.Firewall { return m.fw }

func (m *manager) WireGuard() wireguard.Interface { return m.wg }

func (m *manager) Resolver() *net.Resolver {
	if len(m.dnsservers) == 0 {
		return net.DefaultResolver
	}
	// TODO: use all DNS servers
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, m.dnsservers[0].String())
		},
	}
}

func (m *manager) Start(ctx context.Context, opts *StartOptions) error {
	m.wgmu.Lock()
	defer m.wgmu.Unlock()
	log := context.LoggerFrom(ctx).With("component", "net-manager")
	handleErr := func(err error) error {
		if m.wg != nil {
			if closeErr := m.wg.Close(ctx); closeErr != nil {
				err = fmt.Errorf("%w: %v", err, closeErr)
			}
		}
		if m.fw != nil {
			if clearErr := m.fw.Clear(ctx); clearErr != nil {
				err = fmt.Errorf("%w: %v", err, clearErr)
			}
		}
		return err
	}
	fwopts := &firewall.Options{
		ID: m.opts.NodeID,
		// TODO: Make this configurable
		DefaultPolicy: firewall.PolicyAccept,
		WireguardPort: uint16(m.opts.ListenPort),
		RaftPort:      uint16(m.opts.RaftPort),
		GRPCPort:      uint16(m.opts.GRPCPort),
	}
	log.Info("Configuring firewall", slog.Any("opts", fwopts))
	var err error
	m.fw, err = firewall.New(fwopts)
	if err != nil {
		return fmt.Errorf("new firewall: %w", err)
	}
	if m.opts.Modprobe && runtime.GOOS == "linux" {
		err := loadModule()
		if err != nil {
			// Will attempt a TUN device later on
			log.Error("load wireguard kernel module", slog.String("error", err.Error()))
		}
	}
	wgopts := &wireguard.Options{
		NodeID:              m.opts.NodeID,
		ListenPort:          m.opts.ListenPort,
		Name:                m.opts.InterfaceName,
		ForceName:           m.opts.ForceReplace,
		ForceTUN:            m.opts.ForceTUN,
		PersistentKeepAlive: m.opts.PersistentKeepAlive,
		MTU:                 m.opts.MTU,
		Metrics:             m.opts.RecordMetrics,
		MetricsInterval:     m.opts.RecordMetricsInterval,
		AddressV4:           opts.AddressV4,
		AddressV6:           opts.AddressV6,
		DisableIPv4:         m.opts.DisableIPv4,
		DisableIPv6:         m.opts.DisableIPv6,
	}
	log.Info("Configuring wireguard", slog.Any("opts", wgopts))
	m.wg, err = wireguard.New(ctx, wgopts)
	if err != nil {
		return handleErr(fmt.Errorf("new wireguard: %w", err))
	}
	err = m.wg.Configure(ctx, opts.Key, m.opts.ListenPort)
	if err != nil {
		return handleErr(fmt.Errorf("configure wireguard: %w", err))
	}
	if opts.NetworkV6.IsValid() && !m.opts.DisableIPv6 {
		log.Debug("Adding IPv6 network route", slog.String("network", opts.NetworkV6.String()))
		err = m.wg.AddRoute(ctx, opts.NetworkV6)
		if err != nil && !system.IsRouteExists(err) {
			return handleErr(fmt.Errorf("wireguard add mesh network route: %w", err))
		}
	}
	if opts.AddressV6.IsValid() && !m.opts.DisableIPv6 {
		log.Debug("Adding IPv6 address route", slog.String("address", opts.AddressV6.String()))
		err = m.wg.AddRoute(ctx, opts.AddressV6)
		if err != nil && !system.IsRouteExists(err) {
			return handleErr(fmt.Errorf("wireguard add ipv6 route: %w", err))
		}
	}
	if opts.NetworkV4.IsValid() && !m.opts.DisableIPv4 {
		log.Debug("Adding IPv4 network route", slog.String("network", opts.NetworkV4.String()))
		err = m.wg.AddRoute(ctx, opts.NetworkV4)
		if err != nil && !system.IsRouteExists(err) {
			return handleErr(fmt.Errorf("wireguard add mesh network route: %w", err))
		}
	}
	m.networkv4 = opts.NetworkV4
	m.networkv6 = opts.NetworkV6
	log.Debug("Configuring forwarding on wireguard interface", slog.String("interface", m.wg.Name()))
	err = m.fw.AddWireguardForwarding(ctx, m.wg.Name())
	if err != nil {
		return handleErr(fmt.Errorf("add wireguard forwarding rule: %w", err))
	}
	return nil
}

func (m *manager) StartMasquerade(ctx context.Context) error {
	m.wgmu.Lock()
	defer m.wgmu.Unlock()
	if m.masquerading {
		return nil
	}
	err := m.fw.AddMasquerade(ctx, m.wg.Name())
	if err != nil {
		return fmt.Errorf("add masquerade rule: %w", err)
	}
	m.masquerading = true
	return nil
}

func (m *manager) AddDNSServers(ctx context.Context, servers []netip.AddrPort) error {
	m.dnsmu.Lock()
	defer m.dnsmu.Unlock()
	context.LoggerFrom(ctx).Debug("Configuring DNS servers", slog.Any("servers", servers))
	err := dns.AddServers(m.wg.Name(), servers)
	if err != nil {
		return fmt.Errorf("add dns servers: %w", err)
	}
	m.dnsservers = append(m.dnsservers, servers...)
	return nil
}

func (m *manager) RefreshDNSServers(ctx context.Context) error {
	m.dnsmu.Lock()
	defer m.dnsmu.Unlock()
	context.LoggerFrom(ctx).Debug("Refreshing MeshDNS servers")
	servers, err := peers.New(m.storage).ListByFeature(ctx, v1.Feature_MESH_DNS)
	if err != nil {
		return fmt.Errorf("list peers with feature: %w", err)
	}
	seen := make(map[netip.AddrPort]bool)
	for _, server := range servers {
		if server.PrivateDNSAddrV4().IsValid() && !m.opts.DisableIPv4 {
			seen[server.PrivateDNSAddrV4()] = true
		}
		if server.PrivateDNSAddrV6().IsValid() && !m.opts.DisableIPv6 {
			seen[server.PrivateDNSAddrV6()] = true
		}
	}
	// Find out which (if any) DNS servers we are removing
	toRemove := make([]netip.AddrPort, 0)
	for _, server := range m.dnsservers {
		if _, ok := seen[server]; !ok {
			toRemove = append(toRemove, server)
		} else if ok {
			// We don't need to readd them
			seen[server] = false
		}
	}
	// Reset our dnsservers and determine which servers to add
	// to the system
	m.dnsservers = make([]netip.AddrPort, 0)
	toAdd := make([]netip.AddrPort, 0)
	for server, needsAdd := range seen {
		m.dnsservers = append(m.dnsservers, server)
		if needsAdd {
			toAdd = append(toAdd, server)
		}
	}
	// Add the new servers first
	if len(toAdd) > 0 {
		err := dns.AddServers(m.wg.Name(), toAdd)
		if err != nil {
			return fmt.Errorf("add dns servers: %w", err)
		}
	}
	// Remove the old servers
	if len(toRemove) > 0 {
		err := dns.RemoveServers(m.wg.Name(), toRemove)
		if err != nil {
			return fmt.Errorf("remove dns servers: %w", err)
		}
	}
	return nil
}

func (m *manager) Close(ctx context.Context) error {
	m.wgmu.Lock()
	defer m.wgmu.Unlock()
	log := context.LoggerFrom(ctx).With("component", "net-manager")
	if m.fw != nil {
		// Clear the firewall rules after wireguard is shutdown
		defer func() {
			log.Debug("clearing firewall rules")
			if err := m.fw.Clear(ctx); err != nil {
				log.Error("error clearing firewall rules", slog.String("error", err.Error()))
			}
		}()
	}
	if len(m.dnsservers) > 0 {
		log.Debug("removing DNS servers", slog.Any("servers", m.dnsservers))
		err := dns.RemoveServers(m.wg.Name(), m.dnsservers)
		if err != nil {
			log.Error("error removing DNS servers", slog.String("error", err.Error()))
		}
	}
	if m.wg != nil {
		log.Debug("closing wireguard interface")
		err := m.wg.Close(ctx)
		if err != nil {
			return fmt.Errorf("close wireguard: %w", err)
		}
	}
	return nil
}

func (m *manager) AddPeer(ctx context.Context, peer *v1.WireGuardPeer, iceServers []string) error {
	m.wgmu.Lock()
	defer m.wgmu.Unlock()
	if m.wg == nil {
		return nil
	}
	log := context.LoggerFrom(ctx).With("component", "net-manager")
	ctx = context.WithLogger(ctx, log)
	return m.addPeer(ctx, peer, iceServers)
}

func (m *manager) RefreshPeers(ctx context.Context) error {
	m.wgmu.Lock()
	defer m.wgmu.Unlock()
	if m.wg == nil {
		return nil
	}
	log := context.LoggerFrom(ctx).With("component", "net-manager")
	ctx = context.WithLogger(ctx, log)
	wgpeers, err := mesh.WireGuardPeersFor(ctx, m.storage, m.opts.NodeID)
	if err != nil {
		return fmt.Errorf("wireguard peers for: %w", err)
	}
	log.Debug("current wireguard peers", slog.Any("peers", wgpeers))
	currentPeers := m.wg.Peers()
	seenPeers := make(map[string]struct{})
	var iceServers []string
	errs := make([]error, 0)
	for _, peer := range wgpeers {
		seenPeers[peer.GetId()] = struct{}{}
		// Check if we need to gather ice servers
		if peer.GetIce() && len(iceServers) == 0 {
			peerdb := peers.New(m.storage)
			icepeers, err := peerdb.ListByFeature(ctx, v1.Feature_ICE_NEGOTIATION)
			if err != nil {
				errs = append(errs, fmt.Errorf("list public peers with feature: %w", err))
				continue
			}
			for _, icepeer := range icepeers {
				switch {
				// Prefer primary endpoint
				case icepeer.PublicRPCAddr().IsValid():
					iceServers = append(iceServers, icepeer.PublicRPCAddr().String())
				// Fall back to private endpoint (prefering IPv4)
				case icepeer.PrivateRPCAddrV4().IsValid():
					iceServers = append(iceServers, icepeer.PrivateRPCAddrV4().String())
				case icepeer.PrivateRPCAddrV6().IsValid():
					iceServers = append(iceServers, icepeer.PrivateRPCAddrV6().String())
				}
			}
		}
		// Ensure the peer is configured
		err := m.addPeer(ctx, peer, iceServers)
		if err != nil {
			errs = append(errs, fmt.Errorf("add peer: %w", err))
		}
	}
	// Remove any peers that are no longer in the store
	for peer := range currentPeers {
		if _, ok := seenPeers[peer]; !ok {
			log.Debug("removing peer", slog.String("peer_id", peer))
			m.pcmu.Lock()
			if conn, ok := m.iceConns[peer]; ok {
				conn.peerConn.Close()
				delete(m.iceConns, peer)
			}
			m.pcmu.Unlock()
			if err := m.wg.DeletePeer(ctx, peer); err != nil {
				errs = append(errs, fmt.Errorf("delete peer: %w", err))
			}
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (m *manager) addPeer(ctx context.Context, peer *v1.WireGuardPeer, iceServers []string) error {
	log := context.LoggerFrom(ctx)
	key, err := wgtypes.ParseKey(peer.GetPublicKey())
	if err != nil {
		return fmt.Errorf("parse peer key: %w", err)
	}
	var priv4, priv6 netip.Prefix
	if peer.AddressIpv4 != "" {
		priv4, err = netip.ParsePrefix(peer.AddressIpv4)
		if err != nil {
			return fmt.Errorf("parse peer ipv4: %w", err)
		}
	}
	if peer.AddressIpv6 != "" {
		priv6, err = netip.ParsePrefix(peer.AddressIpv6)
		if err != nil {
			return fmt.Errorf("parse peer ipv6: %w", err)
		}
	}
	endpoint, err := m.determinePeerEndpoint(ctx, peer, iceServers)
	if err != nil {
		if !peer.GetIce() {
			return fmt.Errorf("determine peer endpoint: %w", err)
		}
		// If this is an ICE peer, we'll entertain that they might be able
		// to connect to us.
		log.Warn("error determining ICE endpoint, will wait for incoming connection", "error", err.Error())
	}
	allowedIPs := make([]netip.Prefix, 0)
	for _, ip := range peer.GetAllowedIps() {
		prefix, err := netip.ParsePrefix(ip)
		if err != nil {
			return fmt.Errorf("parse peer allowed ip: %w", err)
		}
		if m.opts.DisableIPv4 && prefix.Addr().Is4() {
			continue
		}
		if m.opts.DisableIPv6 && prefix.Addr().Is6() {
			continue
		}
		allowedIPs = append(allowedIPs, prefix)
	}
	allowedRoutes := make([]netip.Prefix, 0)
	for _, ip := range peer.GetAllowedRoutes() {
		prefix, err := netip.ParsePrefix(ip)
		if err != nil {
			return fmt.Errorf("parse peer allowed route: %w", err)
		}
		if m.opts.DisableIPv4 && prefix.Addr().Is4() {
			continue
		}
		if m.opts.DisableIPv6 && prefix.Addr().Is6() {
			continue
		}
		allowedRoutes = append(allowedRoutes, prefix)
	}
	wgpeer := wireguard.Peer{
		ID:            peer.GetId(),
		GRPCPort:      int(peer.GetGrpcPort()),
		RaftMember:    peer.GetRaftMember(),
		PublicKey:     key,
		Endpoint:      endpoint,
		PrivateIPv4:   priv4,
		PrivateIPv6:   priv6,
		AllowedIPs:    allowedIPs,
		AllowedRoutes: allowedRoutes,
	}
	log.Debug("ensuring wireguard peer", slog.Any("peer", &wgpeer))
	err = m.wg.PutPeer(ctx, &wgpeer)
	if err != nil {
		return fmt.Errorf("put wireguard peer: %w", err)
	}
	// Try to ping the peer to establish a connection
	go func() {
		// TODO: make this configurable
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var addr netip.Prefix
		var err error
		if !m.opts.DisableIPv4 && peer.AddressIpv4 != "" {
			addr, err = netip.ParsePrefix(peer.AddressIpv4)
		} else {
			addr, err = netip.ParsePrefix(peer.AddressIpv6)
		}
		if err != nil {
			log.Warn("could not parse address", slog.String("error", err.Error()))
			return
		}
		err = util.Ping(ctx, addr.Addr())
		if err != nil {
			log.Debug("could not ping descendant", slog.String("descendant", peer.Id), slog.String("error", err.Error()))
			return
		}
		log.Debug("successfully pinged descendant", slog.String("descendant", peer.Id))
	}()
	return nil
}

func (m *manager) determinePeerEndpoint(ctx context.Context, peer *v1.WireGuardPeer, iceServers []string) (netip.AddrPort, error) {
	log := context.LoggerFrom(ctx)
	var endpoint netip.AddrPort
	if peer.GetIce() {
		if len(iceServers) == 0 {
			return endpoint, fmt.Errorf("no ice servers available")
		}
		// TODO: Try all ICE servers
		return m.negotiateICEConn(ctx, iceServers[0], peer)
	}
	// TODO: We don't honor ipv4/ipv6 preferences currently in this function
	if peer.GetPrimaryEndpoint() != "" {
		addr, err := net.ResolveUDPAddr("udp", peer.GetPrimaryEndpoint())
		if err != nil {
			return endpoint, fmt.Errorf("resolve primary endpoint: %w", err)
		}
		if addr.AddrPort().Addr().Is4In6() {
			// This is an IPv4 address masquerading as an IPv6 address.
			// We need to convert it to a real IPv4 address.
			// This is a workaround for a bug in Go's net package.
			addr = &net.UDPAddr{
				IP:   addr.IP.To4(),
				Port: addr.Port,
			}
		}
		endpoint = addr.AddrPort()
	}
	// Check if we are using zone awareness and the peer is in the same zone
	if m.opts.ZoneAwarenessID != "" && peer.GetZoneAwarenessId() == m.opts.ZoneAwarenessID {
		log.Debug("using zone awareness, collecting local CIDRs")
		localCIDRs, err := endpoints.Detect(ctx, endpoints.DetectOpts{
			DetectPrivate:  true,
			DetectIPv6:     true,
			SkipInterfaces: []string{m.wg.Name()},
		})
		if err != nil {
			return endpoint, fmt.Errorf("detect local cidrs: %w", err)
		}
		log.Debug("detected local CIDRs", slog.Any("cidrs", localCIDRs.Strings()))
		// If the primary endpoint is not in our zone and additional endpoints are available,
		// check if any of the additional endpoints are in our zone
		if !localCIDRs.Contains(endpoint.Addr()) && len(peer.GetWireguardEndpoints()) > 0 {
			for _, additionalEndpoint := range peer.GetWireguardEndpoints() {
				addr, err := net.ResolveUDPAddr("udp", additionalEndpoint)
				if err != nil {
					log.Error("could not resolve peer primary endpoint", slog.String("error", err.Error()))
					continue
				}
				if addr.AddrPort().Addr().Is4In6() {
					// Same as above, this is an IPv4 address masquerading as an IPv6 address.
					addr = &net.UDPAddr{
						IP:   addr.IP.To4(),
						Port: addr.Port,
					}
				}
				log.Debug("evalauting zone awareness endpoint",
					slog.String("endpoint", addr.String()),
					slog.String("zone", peer.GetZoneAwarenessId()))
				ep := addr.AddrPort()
				if localCIDRs.Contains(ep.Addr()) {
					// We found an additional endpoint that is in one of our local
					// CIDRs. We'll use this one instead.
					log.Debug("zone awareness shared with peer, using LAN endpoint", slog.String("endpoint", ep.String()))
					endpoint = ep
					break
				}
			}
		}
	}
	return endpoint, nil
}

func (m *manager) negotiateICEConn(ctx context.Context, negotiateServer string, peer *v1.WireGuardPeer) (netip.AddrPort, error) {
	m.pcmu.Lock()
	defer m.pcmu.Unlock()
	log := context.LoggerFrom(ctx)
	if conn, ok := m.iceConns[peer.GetId()]; ok {
		// We already have an ICE connection for this peer
		log.Debug("using existing wireguard ICE connection", slog.String("local-proxy", conn.localAddr.String()), slog.String("peer", peer.GetId()))
		return conn.localAddr, nil
	}
	wgPort, err := m.wg.ListenPort()
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("wireguard listen port: %w", err)
	}
	var endpoint netip.AddrPort
	log.Debug("negotiating wireguard ICE connection", slog.String("server", negotiateServer), slog.String("peer", peer.GetId()))
	conn, err := grpc.DialContext(ctx, negotiateServer, m.opts.DialOptions...)
	if err != nil {
		return endpoint, fmt.Errorf("dial webRTC server: %w", err)
	}
	defer conn.Close()
	pc, err := datachannels.NewWireGuardProxyClient(ctx, v1.NewWebRTCClient(conn), peer.GetId(), wgPort)
	if err != nil {
		return endpoint, fmt.Errorf("create peer connection: %w", err)
	}
	go func() {
		<-pc.Closed()
		defer func() {
			// This is a hacky way to attempt to reconnect to the peer if
			// the ICE connection is closed and they are still in the store.
			if err := m.RefreshPeers(context.Background()); err != nil {
				log.Error("error refreshing peers after ICE connection closed", slog.String("error", err.Error()))
			}
		}()
		m.pcmu.Lock()
		delete(m.iceConns, peer.GetId())
		m.pcmu.Unlock()
	}()
	peerconn := clientPeerConn{
		peerConn:  pc,
		localAddr: pc.LocalAddr().AddrPort(),
	}
	m.iceConns[peer.GetId()] = peerconn
	return peerconn.localAddr, nil
}
