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

// Package file provides a means for providing service discovery information
// via a YAML or JSON file. Changes to the file are propagated regularly.
// See "rotor help file" for usage.
package file

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/cli/command"
	"github.com/turbinelabs/codec"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/rotor"
)

const fileDescription = `Watches the given JSON or YAML file and updates Clusters
stored in the Turbine Labs API at startup and whenever the file changes.

The file can be specified as a flag or as the only argument (but not both).

The structure of the JSON and YAML formats is equivalent. Each contains 0 or
more clusters identified by name, each containing 0 or more instances. For
example, as YAML:

    - cluster: c1
      instances:
      - host: h1
        port: 8000
        metadata:
        - key: stage
          value: prod

Alternatively as JSON:

    [
      {
        "cluster": "c1",
        "instances": [
          {
            "host": "h1",
            "port": 8000,
            "metadata": [
              { "key": "stage", "value": "prod" }
            ]
          }
        ]
      }
    ]

Note that when updating the file, care should be taken to make the modification
atomic. In practice, this means writing the updated file to a temporary location and
then moving/renaming the file to the watched path. Alternatively, the watched path
may be a symbolic link that is replaced with a reference to the updated file.`

// Cmd creates the file based collector sub command
func Cmd(updaterFlags rotor.UpdaterFromFlags) *command.Cmd {
	cmd := &command.Cmd{
		Name:        "file",
		Summary:     "file-based collector",
		Usage:       "[OPTIONS] <file>",
		Description: fileDescription,
	}

	flags := tbnflag.Wrap(&cmd.Flags)
	r := &fileRunner{
		codecFlags:   codec.NewFromFlags(flags),
		updaterFlags: updaterFlags,
	}
	cmd.Runner = r

	cmd.Flags.StringVar(&r.file, "filename", "", "The file from which to collect")

	return cmd
}

type fileRunner struct {
	file         string
	updaterFlags rotor.UpdaterFromFlags
	codecFlags   codec.FromFlags
}

func (r *fileRunner) Run(cmd *command.Cmd, args []string) command.CmdErr {
	if err := r.updaterFlags.Validate(); err != nil {
		return cmd.BadInput(err)
	}

	var file string
	if r.file == "" {
		if len(args) != 1 {
			return cmd.BadInput("must specify filename as either flag or single argument")
		}
		file = filepath.Clean(args[0])
	} else {
		if len(args) != 0 {
			return cmd.BadInput("cannot specify filename as both flag and argument")
		}
		file = filepath.Clean(r.file)
	}

	if err := r.codecFlags.Validate(); err != nil {
		return cmd.BadInput(err)
	}

	updater, err := r.updaterFlags.Make()
	if err != nil {
		return cmd.Error(err)
	}

	collector := NewCollector(file, updater, mkParser(r.codecFlags.Make()))
	if err := collector.Run(); err != nil {
		return cmd.Error(err)
	}

	return command.NoError()
}

type fileCluster struct {
	ClusterName string        `json:"cluster"`
	Instances   api.Instances `json:"instances"`
}

func mkParser(codec codec.Codec) func(io.Reader) ([]api.Cluster, error) {
	return func(reader io.Reader) ([]api.Cluster, error) {
		fileClusters := []fileCluster{}

		err := codec.Decode(reader, &fileClusters)
		if err != nil {
			return nil, err
		}

		clusters := make(map[string]*api.Cluster, len(fileClusters))
		for _, fc := range fileClusters {
			if _, exists := clusters[fc.ClusterName]; exists {
				return nil, fmt.Errorf("duplicate cluster: %s", fc.ClusterName)
			}

			cluster := &api.Cluster{
				Name:      fc.ClusterName,
				Instances: fc.Instances,
			}
			clusters[cluster.Name] = cluster
		}

		result := make([]api.Cluster, 0, len(clusters))
		for _, cluster := range clusters {
			result = append(result, *cluster)
		}
		return result, nil
	}
}
