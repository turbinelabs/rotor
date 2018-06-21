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
	"fmt"

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

// resourceAdapter turns poller.Objects into Listener cache.Resources
func (s lds) resourceAdapter(objects *poller.Objects) (cache.Resources, error) {
	resources := map[string]cache.Resource{}
	for _, port := range objects.AllPorts() {
		if domains := objects.DomainsPerPort(port); domains != nil {
			listener, err := s.mkListener(objects.Proxy, port, domains)
			if err != nil {
				return cache.Resources{}, err
			}
			resources[listener.GetName()] = listener
		}
	}
	return cache.Resources{Version: objects.TerribleHash(), Items: resources}, nil
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
		AccessLog: accessLogs,
	}, nil
}

func mkSNIDomainsAndTLSContext(domains tbnapi.Domains) ([]string, *envoyauth.DownstreamTlsContext) {
	var sniDomains []string
	var certs []*envoyauth.TlsCertificate

	for _, d := range domains {
		if d.SSLConfig != nil {
			sniDomains = append(sniDomains, d.Name)
			for _, pair := range d.SSLConfig.CertKeyPairs {
				certs = append(certs, &envoyauth.TlsCertificate{
					CertificateChain: mkFileDataSource(pair.CertificatePath),
					PrivateKey:       mkFileDataSource(pair.KeyPath),
				})
			}
		}
	}

	var tlsContext *envoyauth.DownstreamTlsContext
	if len(certs) > 0 {
		tlsContext = &envoyauth.DownstreamTlsContext{
			CommonTlsContext: &envoyauth.CommonTlsContext{
				TlsCertificates: certs,
			},
		}
	}

	return sniDomains, tlsContext
}

func (s lds) mkListener(
	proxy tbnapi.Proxy,
	port int,
	domains tbnapi.Domains,
) (*envoyapi.Listener, error) {
	name := mkListenerName(proxy.Name, port)
	httpConnManager, err := s.mkHTTPConnectionManager(proxy.Name, port)
	if err != nil {
		return nil, err
	}

	httpFilter, err := util.MessageToStruct(httpConnManager)
	if err != nil {
		return nil, err
	}

	sniDomains, tlsContext := mkSNIDomainsAndTLSContext(domains)

	addr := mkEnvoyAddress("0.0.0.0", port)

	return &envoyapi.Listener{
		Name:    name,
		Address: *addr,
		FilterChains: []envoylistener.FilterChain{
			{
				FilterChainMatch: &envoylistener.FilterChainMatch{
					SniDomains: sniDomains,
				},
				TlsContext: tlsContext,
				Filters: []envoylistener.Filter{
					{
						Name:   util.HTTPConnectionManager,
						Config: httpFilter,
					},
				},
			},
		},
	}, nil
}
