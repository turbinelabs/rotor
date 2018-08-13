/*
Copyright 2018 Turbine Labs, Inc.

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

package adapter

import (
	"errors"
	"fmt"
	"net"
	"strings"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoyauth "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	envoylistener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	envoyals "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v2"
	envoylog "github.com/envoyproxy/go-control-plane/envoy/config/filter/accesslog/v2"
	envoyrouter "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/router/v2"
	envoyhcm "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/types"

	tbnapi "github.com/turbinelabs/api"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/rotor/xds/log"
	"github.com/turbinelabs/rotor/xds/poller"
)

const (
	grpcAccessLogID   = "tbn.access"
	grpcUpstreamLogID = "tbn.upstream"
)

type lds struct {
	loggingCluster string
}

// adapt turns poller.Objects into Listener cache.Resources
func (s lds) adapt(objects *poller.Objects) (cache.Resources, error) {
	resources := map[string]cache.Resource{}
	for _, port := range objects.AllPorts() {
		if domains := objects.DomainsPerPort(port); domains != nil {
			listener, err := s.mkListener(objects.Proxy, port, domains, objects.ListenerForPort(port))
			if err != nil {
				return cache.Resources{}, err
			}
			resources[listener.GetName()] = listener
		}
	}
	return cache.Resources{Version: objects.TerribleHash(), Items: resources}, nil
}

func (s lds) inject(l *envoyapi.Listener) error {
	if l == nil {
		return nil
	}
	if len(l.FilterChains) == 0 {
		return nil
	}
	for i := range l.FilterChains {
		fc := &l.FilterChains[i]
		for j := range fc.Filters {
			f := &fc.Filters[j]
			if f.Name != util.HTTPConnectionManager {
				continue
			}

			httpFilter := &envoyhcm.HttpConnectionManager{}
			if err := util.StructToMessage(f.Config, httpFilter); err != nil {
				console.Error().Printf(
					"failed to deserialize HttpConnectionManager for listener %q: %s",
					l.GetName(),
					err,
				)
				continue
			}

			// install upstream log
			upstreamLog, err := s.mkUpstreamLog()
			if err == nil && upstreamLog != nil {
				console.Debug().Printf("installing upstream log for listener %q", l.GetName())
				for k := range httpFilter.HttpFilters {
					hf := httpFilter.HttpFilters[k]
					if hf.Name != util.Router {
						continue
					}

					router := &envoyrouter.Router{}
					if err := util.StructToMessage(hf.Config, router); err != nil {
						console.Error().Printf(
							"failed to deserialize router for listener %q: %s",
							l.GetName(),
							err,
						)
						continue
					}

					router.UpstreamLog = append(router.UpstreamLog, upstreamLog)

					if hf.Config, err = util.MessageToStruct(router); err != nil {
						console.Error().Printf(
							"failed to serialize router for listener %q: %s",
							l.GetName(),
							err,
						)
					}
				}
			}

			// install access log
			accessLog, err := s.mkAccessLog()
			if err == nil && accessLog != nil {
				console.Debug().Printf("installing access log for listener %q", l.GetName())
				httpFilter.AccessLog = append(httpFilter.AccessLog, accessLog)
			}

			// install domain and route headers
			routeConfig := httpFilter.GetRouteConfig()
			if routeConfig != nil {
				console.Debug().Printf("installing domain and route headers for listener %q", l.GetName())
				for k := range routeConfig.VirtualHosts {
					vh := &routeConfig.VirtualHosts[k]

					host, port, err := hostPortForListener(l)
					addr := host
					if len(vh.Domains) > 0 {
						addr = vh.Domains[0]
					}
					if err == nil {
						addr = mkListenerName(addr, port)
					}
					addr = valueOrDefault(addr)

					vh.RequestHeadersToAdd = addHeaderIfMissing(
						headerDomainKey,
						addr,
						vh.RequestHeadersToAdd,
					)

					for m := range vh.Routes {
						rt := &vh.Routes[m]
						match := rt.GetMatch()
						var routeStr string
						switch {
						case match.GetPath() != "":
							routeStr = match.GetPath()
						case match.GetPrefix() != "":
							routeStr = match.GetPrefix()
						case match.GetRegex() != "":
							routeStr = match.GetRegex()
						}

						if routeStr == "" {
							routeStr = "/DEFAULT"
						}

						if rt.GetRoute() != nil {
							rt.GetRoute().RequestHeadersToAdd = addHeaderIfMissing(
								headerRouteKey,
								addr+routeStr,
								rt.GetRoute().RequestHeadersToAdd,
							)
						}
					}
				}
			}

			if f.Config, err = util.MessageToStruct(httpFilter); err != nil {
				console.Error().Printf(
					"failed to serialize HttpConnectionManager for Listener %q: %s",
					l.GetName(),
					err,
				)
			}
		}
	}

	return nil
}

// listenerMap encapsulates the act of making sure that multiple listeners
// don't overlap by name or by host/port (including 0.0.0.0 and ::). It has two modes:
// in strict mode, inserting an overlapping listener will produce an error.
// in non-strict; duplicate names are ignored, and duplicate host/ports overwrite,
// except in the case of a collision with a superset host (eg 0.0.0.0), in which
// case the overlapping host is ignored.
type listenerMap struct {
	listeners map[string]cache.Resource
	portMap   map[int]map[string]*envoyapi.Listener
	strict    bool
}

func newListenerMap(strict bool) listenerMap {
	return listenerMap{
		listeners: map[string]cache.Resource{},
		portMap:   map[int]map[string]*envoyapi.Listener{},
		strict:    strict,
	}
}

func (m listenerMap) resourceMap() map[string]cache.Resource {
	return m.listeners
}

// addListener inserts a listener into the listenerMap. See type doc for behavior
func (m listenerMap) addListener(l *envoyapi.Listener) error {
	// if a name collides, we either produce an error, or ignore the listener
	if m.listeners[l.Name] != nil {
		if m.strict {
			return fmt.Errorf("duplicate listener name: %q", l.Name)
		}
		console.Info().Printf("ignoring listener with duplicate name: %q", l.Name)
		return nil
	}

	// if we can't get a host/port, we either produce an error, or ignore the listener
	host, port, err := hostPortForListener(l)
	if err != nil {
		if m.strict {
			return err
		}
		console.Info().Printf("ignoring listener %q with bad port: %s", l.Name, err)
		return nil
	}

	if m.portMap[port] == nil {
		m.portMap[port] = map[string]*envoyapi.Listener{}
	}

	// if the host/port already exists in the map, we either produce an error,
	// or replace the existing listener
	if ol, exists := m.portMap[port][host]; exists {
		if m.strict {
			return fmt.Errorf(
				"cannot add listener %q because listener %q already declared on %s:%d",
				l.Name,
				ol.Name,
				host,
				port,
			)
		}
		console.Info().Printf(
			"replacing listener %q with %q on %s:%d",
			ol.Name,
			l.Name,
			host,
			port,
		)
		delete(m.listeners, ol.Name)
	}

	// if trying to listen on 0:0:0:0 or ::, make sure nothing else is listening on
	// _any_ interface on that port.
	if hostMap, exists := m.portMap[port]; (host == "0.0.0.0" || host == "::") &&
		exists &&
		len(hostMap) > 0 {
		lsStrs := make([]string, 0, len(hostMap))
		for h, ol := range hostMap {
			lsStrs = append(lsStrs, fmt.Sprintf("%q on %s:%d", ol.Name, h, port))
		}

		if host == "::" {
			host = "[::]"
		}

		str := fmt.Sprintf(
			"cannot add listener %q on %s:%d, because one or more listeners exist for port %d: %s",
			l.Name,
			host,
			port,
			port,
			strings.Join(lsStrs, ", "),
		)
		if m.strict {
			return errors.New(str)
		}
		console.Info().Print(str)
		return nil
	}

	allAddrsErrFmt :=
		"cannot add listener %q on %s:%d because listener %q already declared on %s:%d"

	// if 0.0.0.0:<port> is already present in the map, we either produce an
	// error, or ignore the listener
	if ol, exists := m.portMap[port]["0.0.0.0"]; exists {
		str := fmt.Sprintf(allAddrsErrFmt, l.Name, host, port, ol.Name, "0.0.0.0", port)
		if m.strict {
			return errors.New(str)
		}
		console.Info().Print(str)
		return nil
	}

	// if ::/<port> is already present in the map, we either produce an error, or
	// ignore the listener
	if ol, exists := m.portMap[port]["::"]; exists {
		str := fmt.Sprintf(allAddrsErrFmt, l.Name, host, port, ol.Name, "[::]", port)
		if m.strict {
			return errors.New(str)
		}
		console.Info().Print(str)
		return nil
	}

	m.listeners[l.Name] = l
	m.portMap[port][host] = l

	return nil
}

func mkListenerName(name string, port int) string {
	return fmt.Sprintf("%s:%d", name, port)
}

func mkFileDataSource(filename string) *envoycore.DataSource {
	return &envoycore.DataSource{
		Specifier: &envoycore.DataSource_Filename{
			Filename: filename,
		},
	}
}

func mkGRPCAccessLog(id string, cluster string, format log.Format) (*envoylog.AccessLog, error) {
	grpcAccessLog, err := util.MessageToStruct(&envoyals.HttpGrpcAccessLogConfig{
		CommonConfig: &envoyals.CommonGrpcAccessLogConfig{
			LogName: id,
			GrpcService: &envoycore.GrpcService{
				TargetSpecifier: &envoycore.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &envoycore.GrpcService_EnvoyGrpc{
						ClusterName: cluster,
					},
				},
			},
		},
		AdditionalRequestHeadersToLog: log.EnvoyGRPCRequestHeaders(format),
	})
	if err != nil {
		return nil, err
	}

	return &envoylog.AccessLog{
		Name:   util.HTTPGRPCAccessLog,
		Config: grpcAccessLog,
	}, nil
}

func (s lds) mkAccessLog() (*envoylog.AccessLog, error) {
	if s.loggingCluster != "" {
		return mkGRPCAccessLog(grpcAccessLogID, s.loggingCluster, log.TbnAccessFormat)
	}

	return nil, nil
}

func (s lds) mkTracingConfig(listener *tbnapi.Listener) (*envoyhcm.HttpConnectionManager_Tracing, error) {
	if listener != nil && listener.TracingConfig != nil {
		var tracingOperationName envoyhcm.HttpConnectionManager_Tracing_OperationName
		if listener.TracingConfig.Ingress {
			tracingOperationName = envoyhcm.INGRESS
		} else {
			tracingOperationName = envoyhcm.EGRESS
		}

		return &envoyhcm.HttpConnectionManager_Tracing{
			RequestHeadersForTags: listener.TracingConfig.RequestHeadersForTags,
			OperationName:         tracingOperationName,
		}, nil
	}
	return nil, nil
}

func (s lds) mkUpstreamLog() (*envoylog.AccessLog, error) {
	if s.loggingCluster != "" {
		return mkGRPCAccessLog(grpcUpstreamLogID, s.loggingCluster, log.TbnUpstreamFormat)
	}

	return nil, nil
}

func (s lds) mkHTTPRouterStruct() (*types.Struct, error) {
	upstreamLog, err := s.mkUpstreamLog()
	if err != nil {
		return nil, err
	}

	if upstreamLog != nil {
		return util.MessageToStruct(&envoyrouter.Router{
			UpstreamLog: []*envoylog.AccessLog{upstreamLog},
		})
	}

	// Avoid emitting an empty struct since it causes nil/empty-map
	// comparison error in deep equals during tests.
	return nil, nil
}

func (s lds) mkHTTPConnectionManager(
	proxyName string,
	port int,
	tracing *envoyhcm.HttpConnectionManager_Tracing,
) (*envoyhcm.HttpConnectionManager, error) {
	router, err := s.mkHTTPRouterStruct()
	if err != nil {
		return nil, err
	}

	accessLog, err := s.mkAccessLog()
	if err != nil {
		return nil, err
	}

	var accessLogs []*envoylog.AccessLog
	if accessLog != nil {
		accessLogs = []*envoylog.AccessLog{accessLog}
	}

	return &envoyhcm.HttpConnectionManager{
		CodecType:  envoyhcm.AUTO,
		StatPrefix: mkStatsName(proxyName, port),
		// N.B. Not only does ordering matter here but the `envoy.cors`
		// filter relies on the existence of a `CorsPolicy` defined
		// either at the `Route.RouteAction` or
		// `RouteConfiguration.VirtualHost` level. If neither are
		// defined, this filter is a no-op.
		HttpFilters: []*envoyhcm.HttpFilter{
			{
				Name:   util.CORS,
				Config: &types.Struct{},
			},
			{
				Name:   util.Router,
				Config: router,
			},
		},
		RouteSpecifier: &envoyhcm.HttpConnectionManager_Rds{
			Rds: &envoyhcm.Rds{
				RouteConfigName: mkListenerName(proxyName, port),
				ConfigSource:    xdsClusterConfig,
			},
		},
		Tracing:   tracing,
		AccessLog: accessLogs,
	}, nil
}

func (s lds) mkListener(
	proxy tbnapi.Proxy,
	port int,
	domains tbnapi.Domains,
	listener *tbnapi.Listener,
) (*envoyapi.Listener, error) {
	// non-SSL domains for a given listener must all go in a single HTTP connection manager
	name := mkListenerName(proxy.Name, port)

	tracingConfig, err := s.mkTracingConfig(listener)
	if err != nil {
		return nil, err
	}

	var lfs []envoylistener.ListenerFilter
	var nonSSLFilterChains []envoylistener.FilterChain
	var sslFilterChains []envoylistener.FilterChain

	for _, d := range domains {
		var tlsContext *envoyauth.DownstreamTlsContext

		if d.SSLConfig != nil {
			var certs []*envoyauth.TlsCertificate

			if len(lfs) == 0 {
				lfs = append(lfs, envoylistener.ListenerFilter{
					Name:   "envoy.listener.tls_inspector",
					Config: &types.Struct{},
				})
			}

			for _, pair := range d.SSLConfig.CertKeyPairs {
				certs = append(certs, &envoyauth.TlsCertificate{
					CertificateChain: mkFileDataSource(pair.CertificatePath),
					PrivateKey:       mkFileDataSource(pair.KeyPath),
				})
			}

			if len(certs) > 0 {
				tlsContext = &envoyauth.DownstreamTlsContext{
					CommonTlsContext: &envoyauth.CommonTlsContext{
						TlsCertificates: certs,
					},
				}
			}

			httpConnManager, err := s.mkHTTPConnectionManager(proxy.Name, port, tracingConfig)
			if err != nil {
				return nil, err
			}

			httpFilter, err := util.MessageToStruct(httpConnManager)
			if err != nil {
				return nil, err
			}

			sslFilterChains = append(sslFilterChains, envoylistener.FilterChain{
				FilterChainMatch: &envoylistener.FilterChainMatch{
					ServerNames: []string{d.Name},
				},
				TlsContext: tlsContext,
				Filters: []envoylistener.Filter{
					{
						Name:   util.HTTPConnectionManager,
						Config: httpFilter,
					},
				},
			})
		} else if len(nonSSLFilterChains) == 0 {
			httpConnManager, err := s.mkHTTPConnectionManager(proxy.Name, port, tracingConfig)
			if err != nil {
				return nil, err
			}

			httpFilter, err := util.MessageToStruct(httpConnManager)
			if err != nil {
				return nil, err
			}

			nonSSLFilterChains = append(nonSSLFilterChains, envoylistener.FilterChain{
				FilterChainMatch: &envoylistener.FilterChainMatch{
					ServerNames: []string{d.Name},
				},
				Filters: []envoylistener.Filter{
					{
						Name:   util.HTTPConnectionManager,
						Config: httpFilter,
					},
				},
			})
		} else {
			nonSSLFilterChains[0].FilterChainMatch.ServerNames = append(
				nonSSLFilterChains[0].FilterChainMatch.ServerNames,
				d.Name,
			)
		}
	}

	// Envoy doesn't allow a FilterChain for non-SSL ServerNames to explicitly
	// list the ServerNames unless there is also at least one FilterChain
	// corresponding to an SSL ServerName on the same port.
	if len(nonSSLFilterChains) > 0 &&
		len(sslFilterChains) == 0 &&
		len(nonSSLFilterChains[0].FilterChainMatch.ServerNames) < 2 {
		nonSSLFilterChains[0].FilterChainMatch.ServerNames = nil
	}

	addr := mkEnvoyAddress("0.0.0.0", port)

	return &envoyapi.Listener{
		Name:            name,
		Address:         *addr,
		ListenerFilters: lfs,
		FilterChains:    append(nonSSLFilterChains, sslFilterChains...),
	}, nil
}

// hostPortForListener will make a best-effort attempt to obtain the
// IP and port for a listener. In the case of an error, the IP
// may still be populated.
func hostPortForListener(l *envoyapi.Listener) (string, int, error) {
	if l == nil {
		return "", 0, errors.New("could not lookup port for nil listener")
	}

	addr := l.GetAddress()
	saddr := addr.GetSocketAddress()
	if saddr == nil {
		return "", 0, fmt.Errorf("could not lookup port for listener %q: no socket address found", l.Name)
	}
	host := saddr.GetAddress()

	if saddr.GetPortValue() != 0 {
		return host, int(saddr.GetPortValue()), nil
	}
	if saddr.GetNamedPort() != "" {
		proto := strings.ToLower(saddr.GetProtocol().String())
		port, err := net.LookupPort(proto, saddr.GetNamedPort())
		if err != nil {
			return host, 0, fmt.Errorf("could not lookup port for listener %q: %s", l.Name, err)
		}
		return host, port, nil
	}
	return host, 0, fmt.Errorf("could not lookup port for listener %q: has neither a fixed nor named port", l.Name)
}

// addHeaderIfMissing will add a header if the exact key/value is not already
// present. If will leave headers with the same key but different values alone.
func addHeaderIfMissing(
	key, value string,
	headers []*envoycore.HeaderValueOption,
) []*envoycore.HeaderValueOption {
	for _, hvo := range headers {
		if hvo.GetHeader() == nil {
			continue
		}
		if hvo.GetHeader().GetKey() != key {
			continue
		}
		if hvo.GetHeader().GetValue() == value {
			return headers
		}
	}

	return append(headers, &envoycore.HeaderValueOption{
		Header: &envoycore.HeaderValue{
			Key:   key,
			Value: value,
		},
		Append: boolValue(false),
	})
}
