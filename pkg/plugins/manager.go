package plugins

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"strings"

	"github.com/hashicorp/raft"
	v1 "github.com/webmeshproj/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/webmeshproj/webmesh/pkg/context"
	"github.com/webmeshproj/webmesh/pkg/plugins/builtins/ipam"
	"github.com/webmeshproj/webmesh/pkg/plugins/clients"
	"github.com/webmeshproj/webmesh/pkg/storage"
)

var (
	// ErrUnsupported is returned when a plugin capability is not supported
	// by any of the registered plugins.
	ErrUnsupported = status.Error(codes.Unimplemented, "unsupported plugin capability")
)

// Options are the options for creating a new plugin manager.
type Options struct {
	// Storage is the storage backend to use for plugins.
	Storage storage.MeshStorage
	// Plugins is a map of plugin names to plugin configs.
	Plugins map[string]Plugin
}

// Plugin represents a plugin client and its configuration.
type Plugin struct {
	// Client is the plugin client.
	Client clients.PluginClient
	// Config is the plugin configuration.
	Config map[string]any

	// capabilities discovered from the plugin when we started.
	capabilities []v1.PluginInfo_PluginCapability
	// name is the name returned by the plugin.
	name string
}

// hasCapability returns true if the plugin has the given capability.
func (p *Plugin) hasCapability(cap v1.PluginInfo_PluginCapability) bool {
	for _, c := range p.capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// Manager is the interface for managing plugins.
type Manager interface {
	// Get returns the plugin with the given name.
	Get(name string) (clients.PluginClient, bool)
	// HasAuth returns true if the manager has an auth plugin.
	HasAuth() bool
	// HasWatchers returns true if the manager has any watch plugins.
	HasWatchers() bool
	// AuthUnaryInterceptor returns a unary interceptor for the configured auth plugin.
	// If no plugin is configured, the returned function is a pass-through.
	AuthUnaryInterceptor() grpc.UnaryServerInterceptor
	// AuthStreamInterceptor returns a stream interceptor for the configured auth plugin.
	// If no plugin is configured, the returned function is a pass-through.
	AuthStreamInterceptor() grpc.StreamServerInterceptor
	// AllocateIP calls the configured IPAM plugin to allocate an IP address for the given request.
	// If the requested version does not have a registered plugin, ErrUnsupported is returned.
	AllocateIP(ctx context.Context, req *v1.AllocateIPRequest) (netip.Prefix, error)
	// ApplyRaftLog applies a raft log entry to all storage plugins. Responses are still returned
	// even if an error occurs.
	ApplyRaftLog(ctx context.Context, entry *v1.StoreLogRequest) ([]*v1.RaftApplyResponse, error)
	// ApplySnapshot applies a snapshot to all storage plugins.
	ApplySnapshot(ctx context.Context, meta *raft.SnapshotMeta, data io.ReadCloser) error
	// Emit emits an event to all watch plugins.
	Emit(ctx context.Context, ev *v1.Event) error
	// Close closes all plugins.
	Close() error
}

// NewManager creates a new plugin manager.
func NewManager(ctx context.Context, opts Options) (Manager, error) {
	// Create the manager.
	log := context.LoggerFrom(ctx).With("component", "plugin-manager")
	plugins := make(map[string]*Plugin, len(opts.Plugins))
	for n, plugin := range opts.Plugins {
		name := n
		plugins[name] = &plugin
	}
	// Query each plugin for its capabilities.
	for name, plugin := range plugins {
		log.Debug("Querying plugin capabilities", "plugin", name)
		resp, err := plugin.Client.GetInfo(ctx, &emptypb.Empty{})
		if err != nil {
			return nil, fmt.Errorf("get plugin info: %w", err)
		}
		log.Debug("Plugin info", slog.Any("info", resp))
		plugin.capabilities = resp.GetCapabilities()
		plugin.name = resp.GetName()
		// Configure the plugin
		conf, err := structpb.NewStruct(plugin.Config)
		if err != nil {
			return nil, fmt.Errorf("convert plugin config to structpb: %w", err)
		}
		_, err = plugin.Client.Configure(ctx, &v1.PluginConfiguration{
			Config: conf,
		})
		if err != nil {
			return nil, fmt.Errorf("configure plugin: %w", err)
		}
	}
	handleErr := func(cause error) error {
		// Make sure we close all plugins if we fail to start.
		for _, plugin := range plugins {
			_, err := plugin.Client.Close(context.Background(), &emptypb.Empty{})
			if err != nil {
				// Don't report unimplemented close methods.
				if status.Code(err) != codes.Unimplemented {
					log.Error("close plugin", "plugin", plugin.name, "error", err)
				}
			}
		}
		return cause
	}
	// We only support a single auth and IPv4 mechanism for now. So only
	// track the first ones we see
	var auth *Plugin
	var ipamv4 *Plugin
	for name, plugin := range plugins {
		if plugin.hasCapability(v1.PluginInfo_AUTH) {
			if auth != nil {
				return nil, handleErr(fmt.Errorf("multiple auth plugins found: %s, %s", auth.name, name))
			}
			auth = plugin
		}
		if plugin.hasCapability(v1.PluginInfo_IPAMV4) {
			if ipamv4 != nil {
				return nil, handleErr(fmt.Errorf("multiple IPAM plugins found: %s, %s", ipamv4.name, name))
			}
			ipamv4 = plugin
		}
	}
	// If we didn't find any IPAM plugins, register the default one
	if ipamv4 == nil {
		plug := &Plugin{
			Client: clients.NewInProcessClient(&ipam.Plugin{}),
			capabilities: []v1.PluginInfo_PluginCapability{
				v1.PluginInfo_IPAMV4,
				v1.PluginInfo_STORAGE,
			},
			name: "simple-ipam",
		}
		_, err := plug.Client.Configure(ctx, &v1.PluginConfiguration{})
		if err != nil {
			return nil, handleErr(fmt.Errorf("configure default IPAM plugin: %w", err))
		}
		plugins[plug.name] = plug
		ipamv4 = plug
	}
	m := &manager{
		storage: opts.Storage,
		plugins: plugins,
		auth:    auth,
		ipamv4:  ipamv4,
		log:     log,
	}
	go m.handleQueries(opts.Storage)
	return m, nil
}

type manager struct {
	storage storage.MeshStorage
	plugins map[string]*Plugin
	auth    *Plugin
	ipamv4  *Plugin
	log     context.Logger
}

// Get returns the plugin with the given name.
func (m *manager) Get(name string) (clients.PluginClient, bool) {
	p, ok := m.plugins[name]
	return p.Client, ok
}

// HasAuth returns true if the manager has an auth plugin.
func (m *manager) HasAuth() bool {
	return m.auth != nil
}

// HasWatchers returns true if the manager has any watch plugins.
func (m *manager) HasWatchers() bool {
	for _, plugin := range m.plugins {
		if plugin.hasCapability(v1.PluginInfo_WATCH) {
			return true
		}
	}
	return false
}

// AuthUnaryInterceptor returns a unary interceptor for the configured auth plugin.
// If no plugin is configured, the returned function is a no-op.
func (m *manager) AuthUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if m.auth == nil {
			return handler(ctx, req)
		}
		resp, err := m.auth.Client.Auth().Authenticate(ctx, m.newAuthRequest(ctx))
		if err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "authenticate: %v", err)
		}
		log := context.LoggerFrom(ctx).With("caller", resp.GetId())
		ctx = context.WithAuthenticatedCaller(ctx, resp.GetId())
		ctx = context.WithLogger(ctx, log)
		return handler(ctx, req)
	}
}

// AuthStreamInterceptor returns a stream interceptor for the configured auth plugin.
// If no plugin is configured, the returned function is a no-op.
func (m *manager) AuthStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if m.auth == nil {
			return handler(srv, ss)
		}
		resp, err := m.auth.Client.Auth().Authenticate(ss.Context(), m.newAuthRequest(ss.Context()))
		if err != nil {
			return status.Errorf(codes.Unauthenticated, "authenticate: %v", err)
		}
		log := context.LoggerFrom(ss.Context()).With("caller", resp.GetId())
		ctx := context.WithAuthenticatedCaller(ss.Context(), resp.GetId())
		ctx = context.WithLogger(ctx, log)
		return handler(srv, &authenticatedServerStream{ss, ctx})
	}
}

// AllocateIP calls the configured IPAM plugin to allocate an IP address for the given request.
// If the requested version does not have a registered plugin, ErrUnsupported is returned.
func (m *manager) AllocateIP(ctx context.Context, req *v1.AllocateIPRequest) (netip.Prefix, error) {
	var addr netip.Prefix
	var err error
	if m.ipamv4 == nil {
		return addr, ErrUnsupported
	}
	res, err := m.ipamv4.Client.IPAM().Allocate(ctx, req)
	if err != nil {
		return addr, fmt.Errorf("allocate IPv4: %w", err)
	}
	addr, err = netip.ParsePrefix(res.GetIp())
	if err != nil {
		return addr, fmt.Errorf("parse IPv4 address: %w", err)
	}
	return addr, err
}

// ApplyRaftLog applies a raft log entry to all storage plugins.
func (m *manager) ApplyRaftLog(ctx context.Context, entry *v1.StoreLogRequest) ([]*v1.RaftApplyResponse, error) {
	raftstores := m.getRaftStores()
	if len(raftstores) == 0 {
		return nil, nil
	}
	out := make([]*v1.RaftApplyResponse, len(raftstores))
	errs := make([]error, 0)
	for i, store := range raftstores {
		resp, err := store.Client.Raft().Store(ctx, entry)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out[i] = resp
	}
	var err error
	if len(errs) > 0 {
		err = fmt.Errorf("apply raft log: %v", errs)
	}
	return out, err
}

// ApplySnapshot applies a snapshot to all storage plugins.
func (m *manager) ApplySnapshot(ctx context.Context, meta *raft.SnapshotMeta, data io.ReadCloser) error {
	raftstores := m.getRaftStores()
	if len(raftstores) == 0 {
		return nil
	}
	defer data.Close()
	snapshot, err := io.ReadAll(data)
	if err != nil {
		return fmt.Errorf("read snapshot: %w", err)
	}
	errs := make([]error, 0)
	for _, store := range raftstores {
		_, err := store.Client.Raft().RestoreSnapshot(ctx, &v1.DataSnapshot{
			Term:  meta.Term,
			Index: meta.Index,
			Data:  snapshot,
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("apply snapshot: %v", errs)
	}
	return nil
}

func (m *manager) getRaftStores() []*Plugin {
	var raftstores []*Plugin
	for _, plugin := range m.plugins {
		if plugin.hasCapability(v1.PluginInfo_RAFT) {
			raftstores = append(raftstores, plugin)
		}
	}
	return raftstores
}

// Emit emits an event to all watch plugins.
func (m *manager) Emit(ctx context.Context, ev *v1.Event) error {
	errs := make([]error, 0)
	for _, plugin := range m.plugins {
		if plugin.hasCapability(v1.PluginInfo_WATCH) {
			m.log.Debug("Emitting event", "plugin", plugin.name, "event", ev.String())
			_, err := plugin.Client.Events().Emit(ctx, ev)
			if err != nil {
				errs = append(errs, err)
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("emit: %w", errors.Join(errs...))
	}
	return nil
}

// Close closes all plugins.
func (m *manager) Close() error {
	errs := make([]error, 0)
	for _, p := range m.plugins {
		_, err := p.Client.Close(context.Background(), &emptypb.Empty{})
		if err != nil {
			// Don't report unimplemented close methods.
			if status.Code(err) != codes.Unimplemented {
				errs = append(errs, err)
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("close: %v", errs)
	}
	return nil
}

// handleQueries handles SQL queries from plugins.
func (m *manager) handleQueries(db storage.MeshStorage) {
	for plugin, client := range m.plugins {
		if !client.hasCapability(v1.PluginInfo_STORAGE) {
			return
		}
		ctx := context.Background()
		m.log.Info("Starting plugin query stream", "plugin", plugin)
		q, err := client.Client.Storage().InjectQuerier(ctx)
		if err != nil {
			if status.Code(err) == codes.Unimplemented {
				m.log.Debug("plugin does not implement queries", "plugin", plugin)
				return
			}
			m.log.Error("Start query stream", "plugin", plugin, "error", err)
			return
		}
		go m.handleQueryClient(plugin, db, q)
	}
}

// handleQueryClient handles a query client.
func (m *manager) handleQueryClient(plugin string, db storage.MeshStorage, queries v1.StoragePlugin_InjectQuerierClient) {
	defer func() {
		if err := queries.CloseSend(); err != nil {
			m.log.Error("close query stream", "plugin", plugin, "error", err)
		}
	}()
	// TODO: This does not support multiplexed queries yet.
	for {
		query, err := queries.Recv()
		if err != nil {
			if err == io.EOF {
				m.log.Debug("query stream closed cleanly", "plugin", plugin)
				return
			}
			// TODO: restart the stream?
			m.log.Error("receive query", "plugin", plugin, "error", err)
			return
		}
		m.log.Debug("handling plugin query", "plugin", plugin, "query", query.GetQuery(), "cmd", query.GetCommand().String())
		switch query.GetCommand() {
		case v1.PluginQuery_GET:
			var result v1.PluginQueryResult
			result.Id = query.GetId()
			result.Key = query.GetQuery()
			val, err := db.GetValue(queries.Context(), query.GetQuery())
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Value = []string{val}
			}
			err = queries.Send(&result)
			if err != nil {
				m.log.Error("send query result", "plugin", plugin, "error", err)
			}
		case v1.PluginQuery_LIST:
			var result v1.PluginQueryResult
			result.Id = query.GetId()
			result.Key = query.GetQuery()
			keys, err := db.List(queries.Context(), query.GetQuery())
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Value = keys
			}
			err = queries.Send(&result)
			if err != nil {
				m.log.Error("send query result", "plugin", plugin, "error", err)
			}
		case v1.PluginQuery_ITER:
			err := db.IterPrefix(queries.Context(), query.GetQuery(), func(key, val string) error {
				var result v1.PluginQueryResult
				result.Id = query.GetId()
				result.Key = key
				result.Value = []string{val}
				err := queries.Send(&result)
				return err
			})
			if err != nil {
				m.log.Error("stream query results", "plugin", plugin, "error", err)
				continue
			}
			var result v1.PluginQueryResult
			result.Id = query.GetId()
			result.Error = "EOF"
			err = queries.Send(&result)
			if err != nil {
				m.log.Error("send query results EOF", "plugin", plugin, "error", err)
			}
		case v1.PluginQuery_PUT:
			// TODO: Implement
			var result v1.PluginQueryResult
			result.Id = query.GetId()
			result.Error = fmt.Sprintf("unsupported command: %v", query.GetCommand())
			err = queries.Send(&result)
			if err != nil {
				m.log.Error("send query result", "plugin", plugin, "error", err)
			}
		case v1.PluginQuery_DELETE:
			// TODO: Implement
			var result v1.PluginQueryResult
			result.Id = query.GetId()
			result.Error = fmt.Sprintf("unsupported command: %v", query.GetCommand())
			err = queries.Send(&result)
			if err != nil {
				m.log.Error("send query result", "plugin", plugin, "error", err)
			}
		case v1.PluginQuery_SUBSCRIBE:
			// TODO: Implement (this will require wraping the stream in a multiplexer)
			var result v1.PluginQueryResult
			result.Id = query.GetId()
			result.Error = fmt.Sprintf("unsupported command: %v", query.GetCommand())
			err = queries.Send(&result)
			if err != nil {
				m.log.Error("send query result", "plugin", plugin, "error", err)
			}
		default:
			var result v1.PluginQueryResult
			result.Id = query.GetId()
			result.Error = fmt.Sprintf("unsupported command: %v", query.GetCommand())
			err = queries.Send(&result)
			if err != nil {
				m.log.Error("send query result", "plugin", plugin, "error", err)
			}
		}
	}
}

func (m *manager) newAuthRequest(ctx context.Context) *v1.AuthenticationRequest {
	var req v1.AuthenticationRequest
	if md, ok := context.MetadataFrom(ctx); ok {
		headers := make(map[string]string)
		for k, v := range md {
			headers[k] = strings.Join(v, ", ")
		}
		req.Headers = headers
	}
	if authInfo, ok := context.AuthInfoFrom(ctx); ok {
		if tlsInfo, ok := authInfo.(credentials.TLSInfo); ok {
			for _, cert := range tlsInfo.State.PeerCertificates {
				req.Certificates = append(req.Certificates, cert.Raw)
			}
		}
	}
	return &req
}

type authenticatedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authenticatedServerStream) Context() context.Context {
	return s.ctx
}
