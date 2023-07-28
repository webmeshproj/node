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
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"flag"
	"fmt"
	"os"

	"golang.org/x/exp/slog"

	"github.com/webmeshproj/webmesh/pkg/plugins"
	"github.com/webmeshproj/webmesh/pkg/raft"
	"github.com/webmeshproj/webmesh/pkg/util"
)

// Options are the options for the store.
type Options struct {
	// Auth are options for authentication to the mesh.
	Auth *AuthOptions `json:"auth,omitempty" yaml:"auth,omitempty" toml:"auth,omitempty"`
	// Mesh are options for participating in an existing mesh.
	Mesh *MeshOptions `json:"mesh,omitempty" yaml:"mesh,omitempty" toml:"mesh,omitempty"`
	// Bootstrap are options for bootstrapping the store.
	Bootstrap *BootstrapOptions `json:"bootstrap,omitempty" yaml:"bootstrap,omitempty" toml:"bootstrap,omitempty"`
	// Raft are options for the raft store.
	Raft *raft.Options `json:"raft,omitempty" yaml:"raft,omitempty" toml:"raft,omitempty"`
	// TLS are options for TLS.
	TLS *TLSOptions `json:"tls,omitempty" yaml:"tls,omitempty" toml:"tls,omitempty"`
	// WireGuard are options for WireGuard.
	WireGuard *WireGuardOptions `json:"wireguard,omitempty" yaml:"wireguard,omitempty" toml:"wireguard,omitempty"`
	// Plugins are options for plugins.
	Plugins *plugins.Options `yaml:"plugins,omitempty" json:"plugins,omitempty" toml:"plugins,omitempty"`
}

// NewOptions returns new options with sensible defaults.
func NewOptions() *Options {
	return &Options{
		Auth:      NewAuthOptions(),
		Mesh:      NewMeshOptions(),
		Bootstrap: NewBootstrapOptions(),
		Raft:      raft.NewOptions(),
		TLS:       NewTLSOptions(),
		WireGuard: NewWireGuardOptions(),
		Plugins:   plugins.NewOptions(),
	}
}

// BindFlags binds the options to the flags.
func (o *Options) BindFlags(fl *flag.FlagSet) {
	o.Auth.BindFlags(fl)
	o.Mesh.BindFlags(fl)
	o.Bootstrap.BindFlags(fl)
	o.Raft.BindFlags(fl)
	o.TLS.BindFlags(fl)
	o.WireGuard.BindFlags(fl)
	o.Plugins.BindFlags(fl)
}

// Validate validates the options.
func (o *Options) Validate() error {
	if o.Raft == nil {
		o.Raft = raft.NewOptions()
	}
	if o.Mesh == nil {
		o.Mesh = NewMeshOptions()
	}
	if o.Bootstrap == nil {
		o.Bootstrap = NewBootstrapOptions()
	}
	if o.TLS == nil {
		o.TLS = NewTLSOptions()
	}
	if o.WireGuard == nil {
		o.WireGuard = NewWireGuardOptions()
	}
	if err := o.Auth.Validate(); err != nil {
		return err
	}
	if err := o.Mesh.Validate(); err != nil {
		return err
	}
	if err := o.Raft.Validate(); err != nil {
		return err
	}
	if err := o.Bootstrap.Validate(); err != nil {
		return err
	}
	if err := o.WireGuard.Validate(); err != nil {
		return err
	}
	return nil
}

// TLSConfig returns the TLS configuration.
func (o *Options) TLSConfig() (*tls.Config, error) {
	if o.TLS == nil || o.TLS.Insecure {
		return nil, nil
	}
	var config tls.Config
	if o.Auth != nil && o.Auth.MTLS != nil {
		config.Certificates = []tls.Certificate{}
		if o.Auth.MTLS.CertFile != "" && o.Auth.MTLS.KeyFile != "" {
			cert, err := tls.LoadX509KeyPair(o.Auth.MTLS.CertFile, o.Auth.MTLS.KeyFile)
			if err != nil {
				return nil, fmt.Errorf("load x509 key pair: %w", err)
			}
			config.Certificates = append(config.Certificates, cert)
		}
		if o.Auth.MTLS.CertData != "" && o.Auth.MTLS.KeyData != "" {
			cert, err := base64.StdEncoding.DecodeString(o.Auth.MTLS.CertData)
			if err != nil {
				return nil, fmt.Errorf("decode cert data: %w", err)
			}
			key, err := base64.StdEncoding.DecodeString(o.Auth.MTLS.KeyData)
			if err != nil {
				return nil, fmt.Errorf("decode key data: %w", err)
			}
			tlscert, err := tls.X509KeyPair(cert, key)
			if err != nil {
				return nil, fmt.Errorf("x509 key pair: %w", err)
			}
			config.Certificates = append(config.Certificates, tlscert)
		}
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		slog.Default().Warn("failed to load system cert pool", slog.String("error", err.Error()))
		pool = x509.NewCertPool()
	}
	if o.TLS.CAFile != "" {
		ca, err := os.ReadFile(o.TLS.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read ca file: %w", err)
		}
		if ok := pool.AppendCertsFromPEM(ca); !ok {
			return nil, fmt.Errorf("append certs from pem")
		}
	}
	if o.TLS.CAData != "" {
		data, err := base64.StdEncoding.DecodeString(o.TLS.CAData)
		if err != nil {
			return nil, fmt.Errorf("decode ca data: %w", err)
		}
		if ok := pool.AppendCertsFromPEM(data); !ok {
			return nil, fmt.Errorf("append certs from pem")
		}
	}
	config.RootCAs = pool
	if o.TLS.VerifyChainOnly {
		config.InsecureSkipVerify = true
		config.VerifyPeerCertificate = util.VerifyChainOnly
	} else if o.TLS.InsecureSkipVerify {
		config.InsecureSkipVerify = true
	}
	return &config, nil
}