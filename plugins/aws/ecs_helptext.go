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

package aws

const ecsDescription = `Connects to one or more ECS clusters and updates
API Clusters stored with the Turbine Labs API at startup and periodically
thereafter.

Within ECS, items are marked as Turbine Labs Cluster members through the use of
a configurable Docker label (defaulting to ` + ecsDefaultClusterTag + `). This
tag has the format of a comma-separated series of 'cluster:port' declarations.

When a collection pass runs it first examines the ECS Services defined in the
specified ECS Clusters and the associated Tasks. The ContainerInstances for
each Task are examined and, using the Container definition, any container with
the specified tag is marked as handling traffic for the indicated cluster on
the indicated port (as taken from the '<cluster>:<port>' annotation).  If
multiple values are included each pairing indicates an additional instance that
will be bound to the corresponding API Cluster. Note that the port from this
label should indicate container port. Validation is done to ensure that a port
specified in the tag values is defined as an exposed port on the container.

If a container has a port exposed multiple times assignment of the corresponding
host port will use each host mapping once then select randomly for each API
cluster that maps to that port.

To summarize, once the ECS cluster layout is ingested:

(1) running ECS Tasks are requested for each {ECS cluster, ECS Service} pairing

(2) ContainerInstance data is pulled from each Task

(3) Containers within each ContainerInstance are inspected and API Clusters are
created with API Cluster Instances added as the 'cluster:port' label directs

(4)  Each instance attached to an API Cluster will include as metadata the
originating:

  - ECS cluster (short name)
  - ECS service (short name)
  - ECS service (ARN)
  - ECS Task Definition (ARN)
  - ECS Container Name within Task
  - ECS Cluster Instance (ARN)
  - ECS Task Instance (ARN)
  - EC2 Instance Id

An example:

  The ECS environment definition:

    Cluster 'prod'
      Service 'user-auth' (bound to task 'auth_task:4' with 1 instance)
      Service 'api-prod-frontend' (bound to task 'api_task:2' with 4 instances)

    Cluster 'beta'
      Service 'api-beta-frontend' (bound to task 'api_task:3' with 1 instance)

    Tasks
      auth_task:4
        Container 'user-service'
          exposes 9990 to port 0
          no labels
          linked to db-proxy
        Container 'db-proxy'
          no labels

      api_task:2
        Container 'api'
          exposes 80 to 0
          label "tbn-cluster: auth_api:80"
      api_task:3
        Container 'api'
          exposes 80 to 8000
          labels "tbn-cluster: auth_api:80"

This ECS environment is then run on the hosts configured for each cluster and
rotor is instructed to collect API Clusters from 'prod' and 'beta' ECS
clusters using the identifying tag of 'tbn-cluster'.

  The ECS execution environment:

    prod:
      10.0.1.10 running containers
        user-service with 18592 -> 9990/tcp
        db-proxy with no exposed ports
        api wih 18593 -> 80/tcp
      10.0.1.11:
        api wih 11248 -> 80/tcp
      10.0.1.12:
        api wih 23422 -> 80/tcp
        api wih 23423 -> 80/tcp

    beta:
      10.0.1.13:
        api with 8000 -> 80/tcp

The expected result would be:

  API Cluster 'api' with 5 Instances:
    10.0.1.10:18593, metadata{
      ecs-cluster -> prod
      ecs-service -> api-prod-frontend
      ecs-service-arn -> arn:aws:ecs:us-east-1:<acct-id>:service/api-prod-frontend
      ecs-task-definition -> arn:aws:ecs:us-east-1:<acct-id>:task-definition/api_task:2
      ecs-task-container -> api
      ecs-container-instance -> arn:aws:ecs:us-east-1:<acct-id>:container-instance/<ci-uuid-1>
      ecs-task-instance -> arn:aws:ecs:us-east-1:<acct-id>:task/<t-uuid-1>
      ec2-instance-id -> <ec2-id-1>
    }

    10.0.1.11:11248, metadata{
      ecs-cluster -> prod
      ecs-service -> api-prod-frontend
      ecs-service-arn -> arn:aws:ecs:us-east-1:<acct-id>:service/api-prod-frontend
      ecs-task-definition -> arn:aws:ecs:us-east-1:<acct-id>:task-definition/api_task:2
      ecs-task-container -> api
      ecs-container-instance -> arn:aws:ecs:us-east-1:<acct-id>:container-instance/<ci-uuid-2>
      ecs-task-instance -> arn:aws:ecs:us-east-1:<acct-id>:task/<t-uuid-2>
      ec2-instance-id -> <ec2-id-2>
    }

    10.0.1.12:23422, metadata{
      ecs-cluster -> prod
      ecs-service -> api-prod-frontend
      ecs-service-arn -> arn:aws:ecs:us-east-1:<acct-id>:service/api-prod-frontend
      ecs-task-definition -> arn:aws:ecs:us-east-1:<acct-id>:task-definition/api_task:2
      ecs-task-container -> api
      ecs-container-instance -> arn:aws:ecs:us-east-1:<acct-id>:container-instance/<ci-uuid-3>
      ecs-task-instance -> arn:aws:ecs:us-east-1:<acct-id>:task/<t-uuid-3>
      ec2-instance-id -> <ec2-id-3>
    }

    10.0.1.12:23423, metadata{
      ecs-cluster -> prod
      ecs-service -> api-prod-frontend
      ecs-service-arn -> arn:aws:ecs:us-east-1:<acct-id>:service/api-prod-frontend
      ecs-task-definition -> arn:aws:ecs:us-east-1:<acct-id>:task-definition/api_task:2
      ecs-task-container -> api
      ecs-container-instance -> arn:aws:ecs:us-east-1:<acct-id>:container-instance/<ci-uuid-3>
      ecs-task-instance -> arn:aws:ecs:us-east-1:<acct-id>:task/<t-uuid-3>
      ec2-instance-id -> <ec2-id-3>
    }

    10.0.1.13:8000, metadata{
      ecs-cluster -> beta
      ecs-service -> api-beta-frontend
      ecs-service-arn -> arn:aws:ecs:us-east-1:<acct-id>:service/api-beta-frontend
      ecs-task-definition -> arn:aws:ecs:us-east-1:<acct-id>:task-definition/api_task:3
      ecs-task-container -> api
      ecs-container-instance -> arn:aws:ecs:us-east-1:<acct-id>:container-instance/<ci-uuid-4>
      ecs-task-instance -> arn:aws:ecs:us-east-1:<acct-id>:task/<t-uuid-4>
      ec2-instance-id -> <ec2-id-4>
    }


Notice that:

When the exposed port is not bound in the ECS Task/Container configuration
(as in task 'api_task:2') rotor infers the correct host port based on
the port directs to the port specified in the ` + ecsDefaultClusterTag + `
tag.

If a host port is explicitly bound (as in 'api_task:3') it is used in the
constructed API Instance. If that port is not available it is an error an
the instance will not be included.

The names of the ECS Service and ECS Cluster do not impact the name of
the constructed API Cluster. Our examples are all within the 'api' cluster
because of the label despite being sourced from the 'prod' and 'beta' ECS
clusters. If the originating cluster is necessary for routing it will be
included in the instance metadata so constraints may be added.

No API Cluster was created to track containers running in support of
'user-auth' because the Task definition did not include any appropriately
labeled containers.`
