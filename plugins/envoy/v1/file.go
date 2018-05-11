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

package v1

import (
	"path/filepath"

	"github.com/turbinelabs/cli/command"
	"github.com/turbinelabs/codec"
	"github.com/turbinelabs/rotor"
	"github.com/turbinelabs/rotor/plugins/file"
)

type fileRunner struct {
	updaterFlags rotor.UpdaterFromFlags
	codecFlags   codec.FromFlags
}

func (fr *fileRunner) Run(cmd *command.Cmd, args []string) command.CmdErr {
	if err := fr.updaterFlags.Validate(); err != nil {
		return cmd.BadInput(err)
	}

	if err := fr.codecFlags.Validate(); err != nil {
		return cmd.BadInput(err)
	}

	if len(args) != 1 {
		return cmd.BadInput("takes a single file as an argument")
	}

	updater, err := fr.updaterFlags.Make()
	if err != nil {
		return cmd.Error(err)
	}

	parser := newFileParser(fr.codecFlags.Make(), defaultResolverFactory()).parse
	collector := file.NewCollector(
		filepath.Clean(args[0]),
		updater,
		parser,
	)

	if err := collector.Run(); err != nil {
		return cmd.Error(err)
	}

	return command.NoError()
}
