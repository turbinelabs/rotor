
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

# turbinelabs/idgen

[![Apache 2.0](https://img.shields.io/badge/license-apache%202.0-blue.svg)](LICENSE)
[![GoDoc](https://godoc.org/github.com/turbinelabs/idgen?status.svg)](https://godoc.org/github.com/turbinelabs/idgen)
[![CircleCI](https://circleci.com/gh/turbinelabs/idgen.svg?style=shield)](https://circleci.com/gh/turbinelabs/idgen)
[![Go Report Card](https://goreportcard.com/badge/github.com/turbinelabs/idgen)](https://goreportcard.com/report/github.com/turbinelabs/idgen)
[![codecov](https://codecov.io/gh/turbinelabs/idgen/branch/master/graph/badge.svg)](https://codecov.io/gh/turbinelabs/idgen)

The idgen project defines an ID type, and the IDGen type, which can be used to
generate IDs. It also defines several IDGen implementations, including UUID and
counter-based.

## Requirements

- Go 1.9 or later (previous versions may work, but we don't build or test against them)

## Dependencies

The idgen project has no external dependencies; the tests depend on our
[test package](https://github.com/turbinelabs/test).
It should always be safe to use HEAD of all master branches of Turbine Labs
open source projects together, or to vendor them with the same git tag.

Additionally, we vendor
[github.com/nu7hatch/gouuid]("github.com/nu7hatch/gouuid"). This should be
considered an opaque implementation detail, see
[Vendoring](http://github.com/turbinelabs/developer/blob/master/README.md#vendoring)
for more discussion.

## Install

```
go get -u github.com/turbinelabs/idgen/...
```

## Clone/Test

```
mkdir -p $GOPATH/src/turbinelabs
git clone https://github.com/turbinelabs/idgen.git > $GOPATH/src/turbinelabs/idgen
go test github.com/turbinelabs/idgen/...
```

## Godoc

[`idgen`](https://godoc.org/github.com/turbinelabs/idgen)

## Versioning

Please see [Versioning of Turbine Labs Open Source Projects](http://github.com/turbinelabs/developer/blob/master/README.md#versioning).

## Pull Requests

Patches accepted! Please see [Contributing to Turbine Labs Open Source Projects](http://github.com/turbinelabs/developer/blob/master/README.md#contributing).

## Code of Conduct

All Turbine Labs open-sourced projects are released with a
[Contributor Code of Conduct](CODE_OF_CONDUCT.md). By participating in our
projects you agree to abide by its terms, which will be carefully enforced.
