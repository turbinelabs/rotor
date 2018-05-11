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
	"bytes"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/golang/mock/gomock"
	fsnotify "gopkg.in/fsnotify.v1"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/codec"
	tbnos "github.com/turbinelabs/nonstdlib/os"
	"github.com/turbinelabs/rotor/updater"
	"github.com/turbinelabs/test/assert"
	"github.com/turbinelabs/test/tempfile"
)

const (
	YamlInput = `
- cluster: c1
  instances:
  - host: c1h1
    port: 8000
    metadata:
    - key: c1h1m1
      value: c1h1v1
    - key: c1h1m2
      value: c1h1v2
  - host: c1h2
    port: 8001
    metadata:
      - key: c1h2m1
        value: c1h2v1
- cluster: c2
  instances:
  - host: c2h1
    port: 8000
    metadata:
    - key: c2h1m1
      value: c2h1v1
`

	YamlInputWithDuplicateCluster = `
- cluster: c1
  instances:
  - host: c1h1
    port: 8000
- cluster: c1
  instances:
  - host: c1h2
    port: 8001
`

	SimpleYamlInput = `
- cluster: c1
  instances:
  - host: c1h1
    port: 8000
    metadata:
    - key: c1h1m1
      value: c1h1v1
`

	JsonInput = `
[
  {
    "cluster": "c1",
    "instances": [
      {
        "host": "c1h1",
        "port": 8000,
        "metadata": [
          { "key": "c1h1m1", "value": "c1h1v1" },
          { "key": "c1h1m2", "value": "c1h1v2" }
        ]
      },
      {
        "host": "c1h2",
        "port": 8001,
        "metadata": [
          { "key": "c1h2m1", "value": "c1h2v1" }
        ]
      }
    ]
  },
  {
    "cluster": "c2",
    "instances": [
      {
        "host": "c2h1",
        "port": 8000,
        "metadata": [
          { "key": "c2h1m1", "value": "c2h1v1" }
        ]
      }
    ]
  }
]`
)

var (
	simpleTestClusters = []api.Cluster{
		{
			Name: "c1",
			Instances: []api.Instance{
				{
					Host: "c1h1",
					Port: 8000,
					Metadata: api.Metadata{
						{Key: "c1h1m1", Value: "c1h1v1"},
					},
				},
			},
		},
	}

	expectedClusters = []api.Cluster{
		{
			Name: "c1",
			Instances: []api.Instance{
				{
					Host: "c1h1",
					Port: 8000,
					Metadata: []api.Metadatum{
						{
							Key:   "c1h1m1",
							Value: "c1h1v1",
						},
						{
							Key:   "c1h1m2",
							Value: "c1h1v2",
						},
					},
				},
				{
					Host: "c1h2",
					Port: 8001,
					Metadata: []api.Metadatum{
						{
							Key:   "c1h2m1",
							Value: "c1h2v1",
						},
					},
				},
			},
		},
		{
			Name: "c2",
			Instances: []api.Instance{
				{
					Host: "c2h1",
					Port: 8000,
					Metadata: []api.Metadatum{
						{
							Key:   "c2h1m1",
							Value: "c2h1v1",
						},
					},
				},
			},
		},
	}
)

func makeYamlFileCollector() *fileCollector {
	return &fileCollector{parser: mkParser(codec.NewYaml()), os: tbnos.New()}
}

func makeJSONFileCollector() *fileCollector {
	return &fileCollector{parser: mkParser(codec.NewJson()), os: tbnos.New()}
}

func makeFileCollectorAndMock(
	t *testing.T,
) (*fileCollector, *gomock.Controller, *updater.MockUpdater) {
	fileCollector := makeYamlFileCollector()

	ctrl := gomock.NewController(assert.Tracing(t))
	mockUpdater := updater.NewMockUpdater(ctrl)

	fileCollector.updater = mockUpdater

	return fileCollector, ctrl, mockUpdater
}

func TestFileCollectorParseYaml(t *testing.T) {
	fileCollector := makeYamlFileCollector()

	clusters, err := fileCollector.parser(bytes.NewBufferString(YamlInput))
	assert.Nil(t, err)
	assert.HasSameElements(t, clusters, expectedClusters)
}

func TestFileCollectorParseJson(t *testing.T) {
	fileCollector := makeJSONFileCollector()

	clusters, err := fileCollector.parser(bytes.NewBufferString(JsonInput))
	assert.Nil(t, err)
	assert.HasSameElements(t, clusters, expectedClusters)
}

func TestFileCollectorJsonYamlEqual(t *testing.T) {
	yamlCollector := makeYamlFileCollector()
	jsonCollector := makeJSONFileCollector()

	yamlClusters, err := yamlCollector.parser(bytes.NewBufferString(YamlInput))
	assert.Nil(t, err)

	jsonClusters, err := jsonCollector.parser(bytes.NewBufferString(JsonInput))
	assert.Nil(t, err)

	assert.HasSameElements(t, yamlClusters, jsonClusters)
}

func TestFileCollectorParseDuplicateClusters(t *testing.T) {
	yamlCollector := makeYamlFileCollector()

	clusters, err := yamlCollector.parser(bytes.NewBufferString(YamlInputWithDuplicateCluster))
	assert.NonNil(t, err)
	assert.Nil(t, clusters)
}

func TestFileCollectorParseError(t *testing.T) {
	yamlCollector := makeYamlFileCollector()

	clusters, err := yamlCollector.parser(bytes.NewBufferString("nope nope nope"))
	assert.Nil(t, clusters)
	assert.NonNil(t, err)
}

func testReload(t *testing.T, collector *fileCollector, data string) (bool, error) {
	tempFile, cleanup := tempfile.Write(t, data, "filecollector-reload")
	defer cleanup()
	collector.file = tempFile

	return true, collector.reload()
}

func TestFileCollectorReload(t *testing.T) {
	yamlCollector, ctrl, mockUpdater := makeFileCollectorAndMock(t)
	defer ctrl.Finish()

	mockUpdater.EXPECT().Replace(simpleTestClusters)

	testRan, error := testReload(t, yamlCollector, SimpleYamlInput)
	assert.True(t, testRan)
	assert.Nil(t, error)
}

func TestFileCollectorReloadParseError(t *testing.T) {
	yamlCollector, ctrl, _ := makeFileCollectorAndMock(t)
	defer ctrl.Finish()

	testRan, error := testReload(t, yamlCollector, "nope nope nope")
	assert.True(t, testRan)
	assert.ErrorContains(t, error, "cannot unmarshal")
}

func TestFileCollectorReloadReadFileError(t *testing.T) {
	yamlCollector := makeYamlFileCollector()

	tempFile, cleanup := tempfile.Make(t, "filecollector-reload")
	cleanup()

	yamlCollector.file = tempFile

	err := yamlCollector.reload()
	assert.True(t, os.IsNotExist(err))
}

func TestFileEventLoop(t *testing.T) {
	yamlCollector, ctrl, mockUpdater := makeFileCollectorAndMock(t)
	defer ctrl.Finish()

	mockUpdater.EXPECT().Replace(simpleTestClusters)

	tempFile, cleanup := tempfile.Write(t, SimpleYamlInput, "filecollector-reload")
	defer cleanup()

	yamlCollector.file = tempFile

	events := make(chan fsnotify.Event)
	errs := make(chan error)

	var waitGroup sync.WaitGroup
	waitGroup.Add(1)

	var eventLoopResult error
	go func() {
		eventLoopResult = yamlCollector.eventLoop(events, errs)
		waitGroup.Done()
	}()

	events <- fsnotify.Event{
		Name: "wrong file",
		Op:   fsnotify.Create,
	}
	events <- fsnotify.Event{
		Name: tempFile,
		Op:   fsnotify.Remove, // ignored op
	}
	events <- fsnotify.Event{
		Name: tempFile,
		Op:   fsnotify.Create,
	}
	errs <- errors.New("quit")

	waitGroup.Wait()

	assert.HasSuffix(t, eventLoopResult.Error(), "quit")
}
