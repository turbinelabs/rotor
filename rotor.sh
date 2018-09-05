#!/usr/bin/env bash

# Copyright 2018 Turbine Labs, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# TODO(#5245) remove mapping after deprecation cycle
if [ -f /usr/local/bin/envcheck.sh ]; then
  source /usr/local/bin/envcheck.sh
  rewrite_vars TBNCOLLECT_ ROTOR_
fi

require_vars ROTOR_CMD

# eg ROTOR_LOCAL_CLUSTERS=all-in-one-server:8080,all-in-one-client:8083
if [[ -n "$ROTOR_LOCAL_CLUSTERS" ]]; then
  if [[ -n "$ROTOR_XDS_STATIC_RESOURCES_FILENAME" ]]; then
    echo "cannot specify both ROTOR_XDS_STATIC_RESOURCES_FILENAME and ROTOR_LOCAL_CLUSTERS"
    exit 1
  fi

  export ROTOR_XDS_STATIC_RESOURCES_FILENAME=/etc/envoy/local_clusters.json
  export ROTOR_XDS_STATIC_RESOURCES_FORMAT=json

  IFS=',' read -ra PAIRS <<< "${ROTOR_LOCAL_CLUSTERS}"
  for PAIR in "${PAIRS[@]}"; do
    CLUSTER=$(echo $PAIR | cut -d ':' -f1)
    PORT=$(echo $PAIR | cut -d ':' -f2)

    echo "routing $CLUSTER to localhost:$PORT"

    if [[ -z "$CLUSTER" ]] || [[ -z "$PORT" ]]; then
      echo "malformed cluster:port pair: $PAIR"
      exit 1
    fi

    CLUSTERS=$(cat << EOF
$CLUSTERS
    {
      "name": "$CLUSTER",
      "type": "EDS",
      "edsClusterConfig": {
        "edsConfig": {
          "apiConfigSource": {
            "apiType": "GRPC",
            "grpcServices": [
              {
                "envoyGrpc": {
                  "clusterName": "tbn-xds"
                }
              }
            ],
            "refreshDelay": "30s"
          }
        },
        "serviceName": "$CLUSTER"
      },
      "connectTimeout": "10s",
      "lbPolicy": "LEAST_REQUEST",
      "lbSubsetConfig": {
        "fallbackPolicy": "ANY_ENDPOINT"
      }
    },
EOF
)

      LOAD_ASSIGNMENTS=$(cat << EOF
$LOAD_ASSIGNMENTS
    {
      "clusterName": "$CLUSTER",
      "endpoints": [
        {
          "lbEndpoints": [
            {
              "endpoint": {
                "address": {
                  "socketAddress": {
                    "address": "127.0.0.1",
                    "portValue": ${PORT}
                  }
                }
              },
              "healthStatus": "HEALTHY"
            }
          ]
        }
      ]
    },
EOF
)

  done

  (cat << EOF
{
  "clusters": [${CLUSTERS%,}
  ],
  "loadAssignments": [${LOAD_ASSIGNMENTS%,}
  ]
}
EOF
) > "$ROTOR_XDS_STATIC_RESOURCES_FILENAME"

fi

/usr/local/bin/rotor "${ROTOR_CMD}"
