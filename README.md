
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

# turbinelabs/rotor

[![Apache 2.0](https://img.shields.io/badge/license-apache%202.0-blue.svg)](LICENSE)
[![GoDoc](https://godoc.org/github.com/turbinelabs/rotor?status.svg)](https://godoc.org/github.com/turbinelabs/rotor)
[![CircleCI](https://circleci.com/gh/turbinelabs/rotor.svg?style=shield)](https://circleci.com/gh/turbinelabs/rotor)
[![Go Report Card](https://goreportcard.com/badge/github.com/turbinelabs/rotor)](https://goreportcard.com/report/github.com/turbinelabs/rotor)
[![codecov](https://codecov.io/gh/turbinelabs/rotor/branch/master/graph/badge.svg)](https://codecov.io/gh/turbinelabs/rotor)

The Rotor project provides a mechanism for synchronizing service discovery
information with the Turbine Labs public API. We support the following service
discovery integrations:

- [Kubernetes](https://docs.turbinelabs.io/guides/kubernetes.md)
- [DC/OS](https://docs.turbinelabs.io/guides/dcos.md)
- [Consul](https://docs.turbinelabs.io/guides/consul.md)
- [EC2](https://docs.turbinelabs.io/guides/ec2.md)
- [ECS](https://docs.turbinelabs.io/guides/ecs.md)

Additionally, Rotor can poll a file for service discovery information. This
provides a lowest-common-denominator interface if you have a mechanism for
service discovery that we don't yet support. We plan to add support for other
common service discovery mechanisms in the future, and we'd
[love your help](http://github.com/turbinelabs/developer/blob/master/README.md#contributing).

## Requirements

- Go 1.10.1 or later (previous versions may work, but we don't build or test against them)

## Dependencies

The Rotor project depends on these packages:

- [api](https://github.com/turbinelabs/api)
- [cache](https://github.com/turbinelabs/cache)
- [cli](https://github.com/turbinelabs/cli)
- [codec](https://github.com/turbinelabs/codec)
- [nonstdlib](https://github.com/turbinelabs/nonstdlib)
- [stats](https://github.com/turbinelabs/stats)

The tests depend on our [test package](https://github.com/turbinelabs/test),
and on [gomock](https://github.com/golang/mock), and gomock-based Mocks of
most interfaces are provided.

The Rotor plugins depend on many packages, none of which is
exposed in the public interfaces. This should be considered an opaque
implementation detail,
see [Vendoring](http://github.com/turbinelabs/developer/blob/master/README.md#vendoring)
for more discussion.

It should always be safe to use HEAD of all master branches of Turbine Labs
open source projects together, or to vendor them with the same git tag.

## Install

```
go get -u github.com/turbinelabs/rotor/...
go install github.com/turbinelabs/rotor/...
```

## Clone/Test

```
mkdir -p $GOPATH/src/turbinelabs
git clone https://github.com/turbinelabs/rotor.git > $GOPATH/src/turbinelabs/rotor
go test github.com/turbinelabs/rotor/...
```

## Godoc

[Rotor](https://godoc.org/github.com/turbinelabs/rotor)

## Versioning

Please see [Versioning of Turbine Labs Open Source Projects](http://github.com/turbinelabs/developer/blob/master/README.md#versioning).

## Pull Requests

Patches accepted! In particular we'd love to support other mechanisms of
service discovery. Please see
[Contributing to Turbine Labs Open Source Projects](http://github.com/turbinelabs/developer/blob/master/README.md#contributing).

## Code of Conduct

All Turbine Labs open-sourced projects are released with a
[Contributor Code of Conduct](CODE_OF_CONDUCT.md). By participating in our
projects you agree to abide by its terms, which will be carefully enforced.
