package adapter

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	envoyapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"

	"github.com/turbinelabs/codec"
	"github.com/turbinelabs/idgen"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/nonstdlib/log/console"
	tbnos "github.com/turbinelabs/nonstdlib/os"
)

//go:generate $TBN_HOME/scripts/mockgen_internal.sh -type resourcesFromFlags -type staticResourcesProvider -source $GOFILE -destination mock_$GOFILE -package $GOPACKAGE --write_package_comment=false

type conflictBehavior int

const (
	mergeBehavior conflictBehavior = iota
	overwriteBehavior
)

const (
	conflictBehaviorDesc = `
How to handle conflicts between configuration types. If "overwrite"
configuration types overwrite defaults. For example, if one were to include
"listeners" in the static resources configuration file, all existing listeners
would be overwritten. If the value is "merge", listeners would be merged
together, with collisions favoring the statically configured listener. Clusters
are differentiated by name, while listeners are differentiated by IP/port.
Listeners on 0.0.0.0 (or ::) on a given port will collide with any other IP
with the same port. Specifying colliding static resources will produce a
startup error.`

	filenameDesc = `
Path to a file containing static resources. The contents of the file should be
either a JSON or YAML fragment (as configured by the corresponding --format flag)
containing any combination of
"clusters" (an array of https://www.envoyproxy.io/docs/envoy/latest/api-v2/api/v2/cds.proto#cluster),
"cluster_template" (a single cluster, which will be used as the prototype for
all clusters not specified statically), and/or listeners" (an array of
https://www.envoyproxy.io/docs/envoy/latest/api-v2/api/v2/lds.proto#listener).
The file is read once at startup. Only the v2 API is parsed. Enum strings such
as "ROUND_ROBIN" must
be capitalized.
`
)

type staticResourcesProvider interface {
	StaticResources() staticResources
}

type fixedStaticResourcesProvider struct {
	res staticResources
}

func (p fixedStaticResourcesProvider) StaticResources() staticResources {
	return p.res
}

type staticResources struct {
	clusters         []*envoyapi.Cluster
	clusterTemplate  *envoyapi.Cluster
	listeners        []*envoyapi.Listener
	conflictBehavior conflictBehavior
	version          string
}

type resourcesFromFlags interface {
	Make() (staticResourcesProvider, error)
	Validate() error
}

func newResourcesFromFlags(flags tbnflag.FlagSet) resourcesFromFlags {
	ff := &resFromFlags{
		conflictBehavior: tbnflag.NewChoice("overwrite", "merge").WithDefault("merge"),
		format:           tbnflag.NewChoice("json", "yaml").WithDefault("yaml"),
		os:               tbnos.New(),
	}

	flags.Var(
		&ff.conflictBehavior,
		"conflict-behavior",
		conflictBehaviorDesc,
	)

	flags.Var(
		&ff.format,
		"format",
		"The format of the static resources file",
	)

	flags.StringVar(
		&ff.filename,
		"filename",
		"",
		filenameDesc,
	)

	return ff
}

type resFromFlags struct {
	conflictBehavior tbnflag.Choice
	filename         string
	format           tbnflag.Choice
	os               tbnos.OS
}

type resFromFile struct {
	Clusters        []*envoyapi.Cluster  `protobuf:"bytes,1,rep,name=clusters,json=clusters" json:"clusters"`
	ClusterTemplate *envoyapi.Cluster    `protobuf:"bytes,2,rep,name=cluster_template,json=clusterTemplate" json:"cluster_template"`
	Listeners       []*envoyapi.Listener `protobuf:"bytes,3,rep,name=listeners,json=listeners" json:"listeners"`
}

func (m *resFromFile) Reset()         { *m = resFromFile{} }
func (m *resFromFile) String() string { return proto.CompactTextString(m) }
func (*resFromFile) ProtoMessage()    {}

func (ff *resFromFlags) Validate() error {
	if ff.filename == "" {
		return nil
	}

	info, err := ff.os.Stat(ff.filename)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return fmt.Errorf("filename is a directory: %s", ff.filename)
	}

	return nil
}

func (ff *resFromFlags) Make() (staticResourcesProvider, error) {
	if ff.filename == "" {
		return fixedStaticResourcesProvider{}, nil
	}

	var reader io.Reader

	file, err := ff.os.Open(ff.filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if ff.format.String() == "yaml" {
		buf := &bytes.Buffer{}
		if err := codec.YAMLToJSON(file, buf); err != nil {
			return nil, err
		}
		reader = buf
	} else {
		reader = file
	}

	rff := resFromFile{}
	if err := jsonpb.Unmarshal(reader, &rff); err != nil {
		return nil, fmt.Errorf("could not deserialize static resources: %s", err)
	}

	if rff.ClusterTemplate != nil {
		console.Info().Println("using static cluster template")
	}

	if rff.Clusters != nil {
		console.Info().Println("installing static clusters")
	}

	if rff.Listeners != nil {
		console.Info().Println("installing static listeners")
	}

	conflictBehavior := mergeBehavior
	if ff.conflictBehavior.String() == "overwrite" {
		conflictBehavior = overwriteBehavior
	}

	uuid, err := idgen.NewUUID()()
	if err != nil {
		return nil, err
	}

	res := staticResources{
		clusters:         rff.Clusters,
		clusterTemplate:  rff.ClusterTemplate,
		listeners:        rff.Listeners,
		conflictBehavior: conflictBehavior,
		version:          string(uuid),
	}

	if err := validateStaticResources(res); err != nil {
		return nil, fmt.Errorf("malformed static resources: %s", err)
	}

	return fixedStaticResourcesProvider{res}, nil
}

func validateStaticResources(res staticResources) error {
	if res.clusterTemplate != nil && res.clusterTemplate.Type != envoyapi.Cluster_EDS {
		return errors.New("cluster template must be of type EDS")
	}
	m := newListenerMap(true)

	for _, l := range res.listeners {
		if err := m.addListener(l); err != nil {
			return err
		}
	}

	return nil
}
