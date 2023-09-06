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

package libp2p

import (
	"fmt"
	"log/slog"
	"net"

	p2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/webmeshproj/webmesh/pkg/context"
	"github.com/webmeshproj/webmesh/pkg/net/relay"
)

// UDPRelayOptions are the options for negotiating a UDP relay.
type UDPRelayOptions struct {
	// LocalNode is the local node to negotiate a UDP relay with.
	LocalPubKey wgtypes.Key
	// RemoteNode is the remote node to negotiate a UDP relay with.
	RemotePubKey wgtypes.Key
	// RemotePubKey is the remote node's public key.
	RemoteHostPubKey p2pcrypto.PubKey
	// Rendezvous is the rendezvous string to use for negotiating a UDP relay.
	Rendezvous string
	// Relay are options for the relay
	Relay relay.UDPOptions
	// Host are options for configuring the host
	Host HostOptions
}

// NewUDPRelay creates a new UDP relay.
func NewUDPRelay(ctx context.Context, opts UDPRelayOptions) (*UDPRelay, error) {
	host, err := NewHost(ctx, opts.Host)
	if err != nil {
		return nil, fmt.Errorf("new host: %w", err)
	}
	return newUDPRelayWithHostAndCloseFunc(ctx, host, opts, func() error { return host.Close(ctx) })
}

// NewUDPRelay creates a new UDP relay with the given host.
func NewUDPRelayWithHost(ctx context.Context, host Host, opts UDPRelayOptions) (*UDPRelay, error) {
	return newUDPRelayWithHostAndCloseFunc(ctx, host, opts, func() error { return nil })
}

func newUDPRelayWithHostAndCloseFunc(logCtx context.Context, host Host, opts UDPRelayOptions, closef func() error) (*UDPRelay, error) {
	log := context.LoggerFrom(logCtx).With("udp-relay", "libp2p")
	log = log.With(slog.String("host-id", host.ID().String()))
	logCtx = context.WithLogger(logCtx, log)
	log.Debug("Searching for peers on the DHT with our rendezvous string")
	// We create two relays. One that is just for incoming traffic and one
	// that we will use for both outgoing and incoming traffic.
	rxrelay, err := relay.NewLocalUDP(opts.Relay)
	if err != nil {
		return nil, fmt.Errorf("new local udp relay: %w", err)
	}
	rxtxrelay, err := relay.NewLocalUDP(opts.Relay)
	if err != nil {
		defer rxrelay.Close()
		return nil, fmt.Errorf("new local udp relay: %w", err)
	}
	host.Host().SetStreamHandler(UDPRelayProtocolFor(opts.LocalPubKey.String()), func(s network.Stream) {
		log.Debug("Handling incoming protocol stream", "peer", s.Conn().RemotePeer())
		info := s.Conn().RemotePeer()
		key, err := info.ExtractPublicKey()
		if err != nil {
			log.Error("Failed to extract public key from peer", "peer", info, "error", err)
			return
		}
		if !key.Equals(opts.RemoteHostPubKey) {
			log.Error("Peer public key does not match expected public key", "peer", info, "expected", opts.RemoteHostPubKey, "actual", key)
			return
		}
		defer s.Close()
		defer rxrelay.Close()
		if err := rxrelay.Relay(logCtx, s); err != nil {
			log.Error("Relay error", "error", err)
		}
	})
	errs := make(chan error, 1)
	closec := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	routingDiscovery := drouting.NewRoutingDiscovery(host.DHT())
	dutil.Advertise(ctx, routingDiscovery, opts.Rendezvous)
	go func() {
		defer host.Host().RemoveStreamHandler(UDPRelayProtocolFor(opts.RemotePubKey.String()))
		defer close(closec)
		defer close(errs)
		defer rxtxrelay.Close()
	FindPeers:
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			peerChan, err := routingDiscovery.FindPeers(ctx, opts.Rendezvous)
			if err != nil {
				errs <- fmt.Errorf("libp2p find peers: %w", err)
				return
			}
		LoopPeers:
			for {
				select {
				case <-ctx.Done():
					return
				case peer, ok := <-peerChan:
					if !ok {
						continue FindPeers
					}
					if peer.ID == host.ID() || len(peer.Addrs) == 0 {
						continue
					}
					peerKey, err := peer.ID.ExtractPublicKey()
					if err != nil {
						log.Error("Failed to extract public key from peer", "peer", peer.ID, "error", err)
						continue
					}
					if !peerKey.Equals(opts.RemoteHostPubKey) {
						log.Error("Peer public key does not match expected public key", "peer", peer.ID, "expected", opts.RemotePubKey, "actual", peerKey)
						continue
					}
					log.Debug("Found peer", "peer", peer.ID)
					var connectCtx context.Context = context.Background()
					var connectCancel context.CancelFunc = func() {}
					if opts.Host.ConnectTimeout > 0 {
						connectCtx, connectCancel = context.WithTimeout(context.Background(), opts.Host.ConnectTimeout)
					}
					stream, err := host.Host().NewStream(connectCtx, peer.ID, UDPRelayProtocolFor(opts.RemotePubKey.String()))
					connectCancel()
					if err != nil {
						// We'll try the next peer
						log.Debug("Failed to connect to peer", "peer", peer.ID, "error", err.Error())
						continue LoopPeers
					}
					log.Debug("Connected to peer", "peer", peer.ID)
					if err := rxtxrelay.Relay(ctx, stream); err != nil {
						log.Error("Relay error", "error", err)
					}
					return
				}
			}
		}
	}()
	return &UDPRelay{
		UDPRelayOptions: opts,
		localaddr: &net.UDPAddr{
			IP:   net.IP{127, 0, 0, 1},
			Port: int(rxtxrelay.LocalAddr().Port()),
		},
		cancel: cancel,
		errs:   errs,
		closec: closec,
		close:  closef,
	}, nil
}

// UDPRelay is a UDP relay.
type UDPRelay struct {
	UDPRelayOptions
	localaddr *net.UDPAddr
	cancel    context.CancelFunc
	errs      chan error
	closec    chan struct{}
	close     func() error
}

// LocalAddr returns the local address of the relay.
func (u *UDPRelay) LocalAddr() *net.UDPAddr {
	return u.localaddr
}

// Closed returns a channel that is closed when the relay is closed.
func (u *UDPRelay) Closed() <-chan struct{} {
	return u.closec
}

// Errors returns a channel that is closed when the relay encounters an error.
func (u *UDPRelay) Errors() <-chan error {
	return u.errs
}

// Close closes the relay.
func (u *UDPRelay) Close() error {
	u.cancel()
	return u.close()
}