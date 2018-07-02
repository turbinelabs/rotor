package adapter

import (
	"errors"
	"testing"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/golang/mock/gomock"

	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	tbnos "github.com/turbinelabs/nonstdlib/os"
	"github.com/turbinelabs/nonstdlib/ptr"
	"github.com/turbinelabs/test/assert"
	"github.com/turbinelabs/test/tempfile"
)

func TestNewResourcesFromFlags(t *testing.T) {
	flagset := tbnflag.NewTestFlagSet()

	ff := newResourcesFromFlags(flagset).(*resFromFlags)
	assert.Equal(t, ff.conflictBehavior.String(), "merge")
	assert.Equal(t, ff.filename, "")
	assert.Equal(t, ff.format.String(), "yaml")
	assert.NonNil(t, ff.os)

	flagset.Parse([]string{
		"-conflict-behavior", "overwrite",
		"-filename", "the-file",
		"-format", "json",
	})

	assert.Equal(t, ff.conflictBehavior.String(), "overwrite")
	assert.Equal(t, ff.filename, "the-file")
	assert.Equal(t, ff.format.String(), "json")
}

func TestResFromFlagsValidateEmptyFilename(t *testing.T) {
	ff := &resFromFlags{}
	assert.Nil(t, ff.Validate())
}

func TestResFromFlagsValidateBadStat(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	err := errors.New("boom")
	mockOS := tbnos.NewMockOS(ctrl)
	mockOS.EXPECT().Stat("the-filename").Return(nil, err)

	ff := &resFromFlags{
		filename: "the-filename",
		os:       mockOS,
	}

	assert.Equal(t, ff.Validate(), err)
}

func TestResFromFlagsValidateIsDir(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockOS := tbnos.NewMockOS(ctrl)
	mockInfo := tbnos.NewMockFileInfo(ctrl)
	gomock.InOrder(
		mockOS.EXPECT().Stat("the-filename").Return(mockInfo, nil),
		mockInfo.EXPECT().IsDir().Return(true),
	)

	ff := &resFromFlags{
		filename: "the-filename",
		os:       mockOS,
	}

	assert.ErrorContains(t, ff.Validate(), "filename is a directory: the-filename")
}

func TestResFromFlagsValidateOK(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	mockOS := tbnos.NewMockOS(ctrl)
	mockInfo := tbnos.NewMockFileInfo(ctrl)
	gomock.InOrder(
		mockOS.EXPECT().Stat("the-filename").Return(mockInfo, nil),
		mockInfo.EXPECT().IsDir().Return(false),
	)

	ff := &resFromFlags{
		filename: "the-filename",
		os:       mockOS,
	}

	assert.Nil(t, ff.Validate())
}

func TestResFromFlagsMakeNoFilename(t *testing.T) {
	ff := &resFromFlags{}
	provider, gotErr := ff.Make()
	assert.Nil(t, gotErr)
	if assert.NonNil(t, provider) {
		assert.DeepEqual(t, provider.StaticResources(), staticResources{})
		assert.Equal(t, provider.StaticResources().conflictBehavior, mergeBehavior)
	}
}

func TestResFromFlagsMakeBadFilename(t *testing.T) {
	ctrl := gomock.NewController(assert.Tracing(t))
	defer ctrl.Finish()

	err := errors.New("boom")
	mockOS := tbnos.NewMockOS(ctrl)
	mockOS.EXPECT().Open("the-filename").Return(nil, err)

	ff := &resFromFlags{
		filename: "the-filename",
		os:       mockOS,
	}

	provider, gotErr := ff.Make()
	assert.Equal(t, gotErr, err)
	assert.Nil(t, provider)
}

func TestResFromFlagsMakeBadJSON(t *testing.T) {
	file, close := tempfile.Write(t, "not json at all")
	defer close()

	ff := &resFromFlags{
		filename: file,
		os:       tbnos.New(),
	}

	provider, gotErr := ff.Make()
	assert.ErrorContains(t, gotErr, "could not deserialize static resources: invalid character 'o' in literal null (expecting 'u')")
	assert.Nil(t, provider)
}

func TestResFromFlagsMakeBadYAML(t *testing.T) {
	file, close := tempfile.Write(t, "\tillegal yaml")
	defer close()

	ff := &resFromFlags{
		filename: file,
		os:       tbnos.New(),
		format:   tbnflag.Choice{Choice: ptr.String("yaml")},
	}

	provider, gotErr := ff.Make()
	assert.ErrorContains(t, gotErr, "yaml: found character that cannot start any token")
	assert.Nil(t, provider)
}

func TestResFromFlagsMakeJSONBadTypes(t *testing.T) {
	file, close := tempfile.Write(t, `{"bad":"json"}`)
	defer close()

	ff := &resFromFlags{
		filename: file,
		os:       tbnos.New(),
	}

	provider, gotErr := ff.Make()
	assert.ErrorContains(t, gotErr, `could not deserialize static resources: unknown field "bad" in adapter.resFromFile`)
	assert.Nil(t, provider)
}

func TestResFromFlagsMakeYAMLBadTypes(t *testing.T) {
	file, close := tempfile.Write(t, `-bad: yaml`)
	defer close()

	ff := &resFromFlags{
		filename: file,
		os:       tbnos.New(),
	}

	provider, gotErr := ff.Make()
	assert.ErrorContains(t, gotErr, "could not deserialize static resources: invalid character 'b' in numeric literal")
	assert.Nil(t, provider)
}

func TestResFromFlagsNonEDSClusterTemplate(t *testing.T) {
	json := `{
    "cluster_template": {
      "type": "LOGICAL_DNS"
    }
  }`

	file, close := tempfile.Write(t, json)
	defer close()

	ff := &resFromFlags{
		filename:         file,
		os:               tbnos.New(),
		conflictBehavior: tbnflag.Choice{Choice: ptr.String("overwrite")},
	}

	provider, gotErr := ff.Make()
	assert.ErrorContains(t, gotErr, "malformed static resources: cluster template must be of type EDS")
	assert.Nil(t, provider)
}

func TestResFromFlagsMakeJSON(t *testing.T) {
	json := `{
    "cluster_template": {
      "type": "EDS",
      "edsClusterConfig": {
        "edsConfig": {
          "apiConfigSource": {
            "apiType": "GRPC",
            "clusterNames": [
              "tbn-xds"
            ],
            "refreshDelay": "120.000s"
          }
        }
      },
      "connectTimeout": "230.000s",
      "lbPolicy": "RING_HASH"
    },
    "clusters": [
      {
        "name": "all-in-one-server",
        "type": "EDS",
        "edsClusterConfig": {
          "edsConfig": {
            "apiConfigSource": {
              "apiType": "GRPC",
              "clusterNames": [
                "tbn-xds"
              ],
              "refreshDelay": "90.000s"
            }
          },
          "serviceName": "all-in-one-server"
        },
        "connectTimeout": "40.000s",
        "lbPolicy": "LEAST_REQUEST"
      }
    ],
    "listeners": [
      {
        "name": "default-cluster:8080",
        "address": {
          "socket_address": {
            "address": "0.0.0.0",
            "port_value": 8080
          }
        },
        "filter_chains": [
          {
            "filters": [
              {
                "name": "envoy.http_connection_manager",
                "config": {
                  "codec_type": "AUTO",
                  "stat_prefix": "all_in_one_demo",
                  "route_config": {
                    "name": "local_route",
                    "virtual_hosts": [
                      {
                        "name": "localhost:8080",
                        "domains": [
                          "localhost"
                        ],
                        "routes": [
                          {
                            "match": {
                              "prefix": "/api"
                            },
                            "route": {
                              "cluster": "all-in-one-server"
                            }
                          },
                          {
                            "match": {
                              "prefix": "/"
                            },
                            "route": {
                              "cluster": "all-in-one-client"
                            }
                          }
                        ]
                      }
                    ]
                  },
                  "http_filters": [
                    {
                      "name": "envoy.router",
                      "config": {}
                    }
                  ]
                }
              }
            ]
          }
        ]
      }
    ]
  }`

	file, close := tempfile.Write(t, json)
	defer close()

	ff := &resFromFlags{
		filename:         file,
		os:               tbnos.New(),
		conflictBehavior: tbnflag.Choice{Choice: ptr.String("overwrite")},
	}

	provider, gotErr := ff.Make()
	assert.Nil(t, gotErr)
	if assert.NonNil(t, provider) {
		res := provider.StaticResources()
		assert.NonNil(t, res.clusterTemplate)
		assert.Equal(t, len(res.clusters), 1)
		assert.Equal(t, len(res.listeners), 1)
		assert.Equal(t, res.conflictBehavior, overwriteBehavior)
	}
}

func TestResFromFlagsMakeYAML(t *testing.T) {
	yaml := `---
cluster_template:
  type: EDS
  edsClusterConfig:
    edsConfig:
      apiConfigSource:
        apiType: GRPC
        clusterNames:
        - tbn-xds
        refreshDelay: 120.000s
  connectTimeout: 230.000s
  lbPolicy: RING_HASH
clusters:
- name: all-in-one-server
  type: EDS
  edsClusterConfig:
    edsConfig:
      apiConfigSource:
        apiType: GRPC
        clusterNames:
        - tbn-xds
        refreshDelay: 90.000s
    serviceName: all-in-one-server
  connectTimeout: 40.000s
  lbPolicy: LEAST_REQUEST
listeners:
- name: default-cluster:8080
  address:
    socket_address:
      address: 0.0.0.0
      port_value: 8080
  filter_chains:
  - filters:
    - name: envoy.http_connection_manager
      config:
        codec_type: AUTO
        stat_prefix: all_in_one_demo
        route_config:
          name: local_route
          virtual_hosts:
          - name: localhost:8080
            domains:
            - localhost
            routes:
            - match:
                prefix: "/api"
              route:
                cluster: all-in-one-server
            - match:
                prefix: "/"
              route:
                cluster: all-in-one-client
        http_filters:
        - name: envoy.router
          config: {}`

	file, close := tempfile.Write(t, yaml)
	defer close()

	ff := &resFromFlags{
		filename: file,
		os:       tbnos.New(),
		format:   tbnflag.Choice{Choice: ptr.String("yaml")},
	}

	provider, gotErr := ff.Make()
	assert.Nil(t, gotErr)
	if assert.NonNil(t, provider) {
		res := provider.StaticResources()
		assert.NonNil(t, res.clusterTemplate)
		assert.Equal(t, len(res.clusters), 1)
		assert.Equal(t, len(res.listeners), 1)
		assert.Equal(t, res.conflictBehavior, mergeBehavior)
	}
}

func TestResFromFileReset(t *testing.T) {
	rff := &resFromFile{
		Clusters:        []*v2.Cluster{},
		ClusterTemplate: &v2.Cluster{},
		Listeners:       []*v2.Listener{},
	}
	rff.Reset()
	assert.Nil(t, rff.Clusters)
	assert.Nil(t, rff.ClusterTemplate)
	assert.Nil(t, rff.Listeners)
}

func TestResFromFileString(t *testing.T) {
	rff := &resFromFile{
		Clusters:        []*v2.Cluster{},
		ClusterTemplate: &v2.Cluster{Name: "the-cluster"},
		Listeners:       []*v2.Listener{},
	}
	assert.Equal(t, rff.String(), `cluster_template:<name:"the-cluster" connect_timeout:<> > `)
}
