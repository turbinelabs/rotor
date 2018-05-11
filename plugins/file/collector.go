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

package file

import (
	"fmt"
	"io"
	"path/filepath"

	fsnotify "gopkg.in/fsnotify.v1"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/nonstdlib/log/console"
	tbnos "github.com/turbinelabs/nonstdlib/os"
	"github.com/turbinelabs/rotor/updater"
)

type clusterParser = func(io.Reader) ([]api.Cluster, error)

// Collector provides an exported interface for a fileCollector.
type Collector interface {
	Run() error
}

// NewCollector is a factory for a file based collector.
func NewCollector(
	file string,
	updater updater.Updater,
	parser clusterParser,
) Collector {
	return &fileCollector{
		file:    file,
		updater: updater,
		parser:  parser,
		os:      tbnos.New(),
	}
}

type fileCollector struct {
	file    string
	updater updater.Updater
	parser  clusterParser
	os      tbnos.OS
}

func (c *fileCollector) Run() error {
	if err := c.reload(); err != nil {
		return err
	}

	events, errors, closer, err := c.startWatcher()
	if err != nil {
		return err
	}
	defer closer.Close()

	return c.eventLoop(events, errors)
}

func (c *fileCollector) reload() error {
	console.Debug().Println("file: reload")

	file, err := c.os.Open(c.file)
	if err != nil {
		return err
	}

	clusters, err := c.parser(file)
	if err != nil {
		return err
	}

	c.updater.Replace(clusters)
	return nil
}

func (c *fileCollector) startWatcher() (chan fsnotify.Event, chan error, io.Closer, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		defer watcher.Close()
		return nil, nil, nil, fmt.Errorf("watch error: %s", err)
	}

	console.Info().Printf("watching %s", c.file)
	if err := watcher.Add(c.file); err != nil {
		defer watcher.Close()
		return nil, nil, nil, fmt.Errorf("watch file error: %s", err)
	}

	parent := filepath.Dir(c.file)
	console.Info().Printf("watching %s", parent)
	if err := watcher.Add(parent); err != nil {
		defer watcher.Close()
		return nil, nil, nil, fmt.Errorf("watch dir error: %s", err)
	}

	return watcher.Events, watcher.Errors, watcher, nil
}

func (c *fileCollector) eventLoop(events chan fsnotify.Event, errors chan error) error {
	for {
		select {
		case event := <-events:
			if event.Name == c.file {
				console.Info().Printf("%s changed %x", event.Name, event.Op)
				if event.Op&(fsnotify.Create|fsnotify.Write) != 0 {
					c.reload()
				} else if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
					console.Info().Printf("file %s disappeared", c.file)
				}
			}

		case err := <-errors:
			return fmt.Errorf("watch error: %s", err)
		}
	}
}
