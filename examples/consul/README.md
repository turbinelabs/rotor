[//]: # ( Copyright 2018 Turbine Labs, Inc.                                   )
[//]: # ( you may not use this file except in compliance with the License.    )
[//]: # ( You may obtain a copy of the License at                             )
[//]: # (                                                                     )
[//]: # (     http://www.apache.org/licenses/LICENSE-2.0                      )
[//]: # (                                                                     )
[//]: # ( Unless required by applicable law or agreed to in writing, software )
[//]: # ( distributed under the License is distributed on an "AS IS" BASIS,   )
[//]: # ( WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or     )
[//]: # ( implied. See the License for the specific language governing        )
[//]: # ( permissions and limitations under the License.                      )

# Rotor Consul demo

This demo shows Envoy running with Rotor as its xDS server. Consul is also
running with two services, which Rotor discovers, and sends to Envoy as
clusters.

To run the demo, use:

```console
$ docker-compose up
```

in this directory to build and run the `docker-compose.yaml`.

Rotor, Envoy, and Consul should start up.
The [Envoy admin console](http://localhost:9999)
shows configuration and log information for Envoy and the configuration from
Rotor.

You can either visit /clusters on the Envoy admin console to see the Consul
servcies horse.turbinelabs.io and cow.turbinelabs.io are loaded, or curl:

```console
$ curl localhost:9999/clusters
```

You can also view the [Consul web UI](http://localhost:8500/ui).

Rotor creates a listener at localhost:80 for Envoy's clusters.

Try:

```
$ curl -H "Host:cow.turbinelabs.io" localhost:80
```

or

```
curl -H "Host:horse.turbinelabs.io" localhost:80
```

to see the results.

## Notes

This example registers two domains in Consul as service instances:
horse.turbinelabs.io and cow.turbinelabs.io. Normally, your service instances
would be registered as IP addresses, not hostname. To handle this case, Rotor
can take the flag `ENV ROTOR_XDS_RESOLVE_DNS=true`, and it will resolve DNS
before passing the instance IPs to Envoy. If you plan to use Envoy to route to
hostname, make sure to set this flag.
