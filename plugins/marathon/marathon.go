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

// Package marathon provides an integration with Marathon. See
// "rotor help marathon" for usage.
package marathon

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/turbinelabs/api"
	"github.com/turbinelabs/cli/command"
	tbnflag "github.com/turbinelabs/nonstdlib/flag"
	"github.com/turbinelabs/nonstdlib/log/console"
	"github.com/turbinelabs/rotor"
	"github.com/turbinelabs/rotor/constants"
	"github.com/turbinelabs/rotor/plugins/kubernetes/labels"
	"github.com/turbinelabs/rotor/updater"

	marathon "github.com/gambol99/go-marathon"
)

const (
	marathonAppPrefix = "app/"

	marathonDescription = `Connects to a Marathon API server and updates
Clusters stored in the Turbine Labs API at startup and periodically thereafter.

Application labels are used to determine which API cluster a particular task
belongs to. The default label name is "` + constants.DefaultClusterLabelName + `", but
can be overridden by a flag (see -cluster-label). By default all applications
are watched, but you may also provide a label selector.

Each task is examined for service ports. The first exposed TCP port is used.
If no ports are exposed, the task is ignored.

All application labels besides the cluster label are captured as instance
metadata for routing.
`
)

type marathonCollectorSettings struct {
	groupPrefix      string
	clusterLabelName string
	selector         string
}

type marathonRunner struct {
	marathonCollectorSettings

	updaterFlags rotor.UpdaterFromFlags
	clientFlags  clientFromFlags

	errorf func(string, ...interface{}) command.CmdErr
}

func Cmd(updaterFlags rotor.UpdaterFromFlags) *command.Cmd {
	runner := &marathonRunner{}

	cmd := &command.Cmd{
		Name:        "marathon",
		Summary:     "marathon collector",
		Usage:       "[OPTIONS]",
		Description: marathonDescription,
		Runner:      runner,
	}

	flags := tbnflag.Wrap(&cmd.Flags)

	flags.StringVar(
		&runner.groupPrefix,
		"group-prefix",
		"",
		"Marathon `group` prefix naming applications to expose. By default, all groups.",
	)

	flags.StringVar(
		&runner.selector,
		"selector",
		"",
		"A label selector for filtering applications.",
	)

	flags.StringVar(
		&runner.clusterLabelName,
		"cluster-label",
		constants.DefaultClusterLabelName,
		"The name of the Marathon `label` specifying to which cluster a Mesos task belongs.",
	)

	runner.clientFlags = newClientFromFlags(flags)
	runner.updaterFlags = updaterFlags
	runner.errorf = cmd.Errorf

	return cmd
}

// makeFilter creates a filter in accordance with the subject runner's parameters.
func (r *marathonRunner) makeFilter() (marathonFilter, error) {
	var filters []marathonFilter

	if r.selector != "" {
		filter, err := makeLabelFilter(r.selector)
		if err != nil {
			return nil, err
		}
		filters = append(filters, filter)
	}

	return makeFilterStack(filters), nil
}

func (r *marathonRunner) Run(cmd *command.Cmd, args []string) command.CmdErr {
	var (
		collector marathonCollector
		filter    marathonFilter
		err       error
	)

	// Validate input
	err = r.updaterFlags.Validate()
	if err != nil {
		return cmd.BadInput(err.Error())
	}

	err = r.clientFlags.Validate()
	if err != nil {
		return cmd.BadInput(err.Error())
	}

	// Initialize filters
	if filter, err = r.makeFilter(); err != nil {
		return cmd.BadInput(err.Error())
	}

	// Make updater and client
	u, err := r.updaterFlags.Make()
	if err != nil {
		return cmd.Error(err.Error())
	}
	mclient, err := r.clientFlags.Make()
	if err != nil {
		return cmd.Errorf("Unable to instantiate Marathon client: %s", err.Error())
	}

	// Initialize the collector and start updating
	collector = marathonCollector{
		marathonCollectorSettings: r.marathonCollectorSettings,
		client:                    mclient,
		filter:                    filter,
	}

	updater.Loop(u, collector.getClusters)

	return command.NoError()
}

type marathonFilter func(
	*marathonCollectorSettings,
	*marathon.Application,
	*marathon.Task,
	map[string]string) bool

func makeFilterStack(filters []marathonFilter) marathonFilter {
	return func(
		settings *marathonCollectorSettings,
		app *marathon.Application,
		task *marathon.Task,
		labels map[string]string,
	) bool {
		for _, filter := range filters {
			if !filter(settings, app, task, labels) {
				return false
			}
		}
		return true
	}
}

func makeLabelFilter(query string) (marathonFilter, error) {
	selector, err := labels.Parse(query)
	if err != nil {
		return nil, fmt.Errorf("Error parsing selector: %s", err.Error())
	}
	return func(
		settings *marathonCollectorSettings,
		app *marathon.Application,
		task *marathon.Task,
		lmap map[string]string,
	) bool {
		return selector.Matches(labels.Set(lmap))
	}, nil
}

func ftoa(f float64) string {
	return strconv.FormatFloat(f, 'f', 3, 64)
}

func labelsForApp(app *marathon.Application) map[string]string {
	labels := map[string]string{
		marathonAppPrefix + "id":            app.ID,
		marathonAppPrefix + "version":       app.Version,
		marathonAppPrefix + "required_cpus": ftoa(app.CPUs),
	}
	if app.Mem != nil {
		labels[marathonAppPrefix+"required_mem"] = ftoa(*app.Mem)
	}
	if app.Disk != nil {
		labels[marathonAppPrefix+"required_disk"] = ftoa(*app.Disk)
	}

	// Additional metadata
	if app.Labels != nil {
		for k, v := range *app.Labels {
			labels[k] = v
		}
	}

	return labels
}

type marathonCollector struct {
	marathonCollectorSettings

	client marathon.Marathon
	filter marathonFilter
}

func (m *marathonCollector) getClusters() ([]api.Cluster, error) {
	groups, err := m.client.Groups()
	if err != nil {
		return nil, fmt.Errorf("Error fetching groups: %s", err.Error())
	}

	clusters := map[string]*api.Cluster{}

	m.collect(clusters, groups.Groups)

	proposed := make(api.Clusters, 0, len(clusters))
	for _, cluster := range clusters {
		proposed = append(proposed, *cluster)
	}

	if len(proposed) == 0 {
		return nil, errors.New("no clusters found, skipping update")
	}

	return proposed, nil
}

func (m *marathonCollector) collect(
	clusters map[string]*api.Cluster,
	groups []*marathon.Group,
) {
	if len(groups) == 0 {
		return
	}

	for _, group := range groups {
		console.Debug().Println("examining group: ", group.ID)

		// The group we are looking at is a child of the group prefix
		groupIsChildOfPrefix := strings.HasPrefix(group.ID, m.groupPrefix)

		// The group we are looking at is part of the group prefix
		groupIsPartOfPrefix := strings.HasPrefix(m.groupPrefix, group.ID)

		// The group we are looking at is the same as the group prefix
		groupIsPrefix := m.groupPrefix == group.ID

		if !(groupIsChildOfPrefix || groupIsPartOfPrefix) {
			continue
		}

		if len(group.Groups) > 0 {
			m.collect(clusters, group.Groups)
		}

		if !(groupIsChildOfPrefix || groupIsPrefix) {
			continue
		}

		for _, app := range group.Apps {
			tasks, err := m.client.Tasks(app.ID)
			if err != nil {
				console.Error().Printf("Failed to fetch tasks for app '%s'", app.ID)
				continue
			}

			if err := m.handleApp(clusters, app, tasks); err != nil {
				console.Error().Println("Processing", app.ID, "failed:", err.Error())
			}
		}
	}
}

func (m *marathonCollector) handleApp(
	clusters map[string]*api.Cluster,
	app *marathon.Application,
	tasks *marathon.Tasks,
) error {
	var totalCount, filteredCount int

	labels := labelsForApp(app)
	clusterName := labels[m.clusterLabelName]

	if len(clusterName) == 0 {
		// don't bother with apps that don't have the cluster label set
		console.Debug().Printf("Ignoring '%s': missing/empty cluster label '%s'",
			app.ID, m.clusterLabelName)
		return nil
	}

	var cluster *api.Cluster
	cluster, ok := clusters[clusterName]
	if !ok {
		cluster = &api.Cluster{Name: clusterName}
		clusters[clusterName] = cluster
	}

	for _, task := range tasks.Tasks {
		totalCount++
		if m.filter != nil {
			if !m.filter(&m.marathonCollectorSettings, app, &task, labels) {
				// If the filter fails for this task, continue to the next task.
				filteredCount++
				continue
			}
		}

		host, port, err := m.findAddr(task)
		if err != nil {
			console.Error().Printf("Task '%s': %s", task.ID, err.Error())
		} else {
			cluster.Instances = append(cluster.Instances, m.makeInstance(host, port, labels))
		}
	}

	console.Debug().Printf("App '%s' (cluster '%s') has %d instances (%d considered, %d filtered)",
		app.ID, clusterName, len(cluster.Instances), totalCount, filteredCount)

	return nil
}

// errNoAddr is returned when an internet address does not exist.
var errNoAddr = fmt.Errorf("Task has no address")

// findAddr gets the internet address of an instance. errNoAddr is returned if no suitable address
// can be determined.
func (m *marathonCollector) findAddr(task marathon.Task) (string, int, error) {
	if len(task.Ports) > 0 {
		// TODO: respect "portIndex"?
		return task.Host, task.Ports[0], nil
	}
	return "", 0, errNoAddr
}

func (m *marathonCollector) makeInstance(
	host string,
	port int,
	labels map[string]string,
) api.Instance {
	instance := api.Instance{Host: host, Port: port}
	var keys []string
	for key := range labels {
		if key != m.clusterLabelName {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := labels[key]
		datum := api.Metadatum{Key: key, Value: value}
		instance.Metadata = append(instance.Metadata, datum)
	}
	return instance
}
