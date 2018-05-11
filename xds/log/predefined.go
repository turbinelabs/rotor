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

package log

var (
	// TbnAccessFormat is a Turbine Labs specific Envoy access log
	// format.
	TbnAccessFormat = Format{
		timestamp(timestampField),
		ignored(requestIDField),
		ignored(requestRemoteAddrField),
		tag(methodField),
		ignored(requestURIField),
		counter(responseStatusField),
		ignored(responseLengthField),
		duration(responseTimeField),
		quoted(tag(upstreamClusterField)),
		quoted(tag(upstreamInstanceField)),
		quoted(tag(domainField)),
		quoted(tag(routeField)),
		quoted(tag(ruleField)),
		quoted(tag(sharedRuleField)),
		quoted(tag(constraintField)),
		quoted(ignored(requestReferrerField)),
		quoted(ignored(requestUserAgentField)),
	}

	// TbnUpstreamFormat is a Turbine Labs specific Envoy upstream log
	// format.
	TbnUpstreamFormat = Format{
		timestamp(timestampField),
		ignored(requestIDField),
		counter(upstreamResponseStatusField),
		quoted(tag(upstreamInstanceField)),
		tag(methodField),
		ignored(upstreamResponseLengthField),
		duration(upstreamResponseTimeField),
		quoted(tag(upstreamClusterField)),
		quoted(tag(domainField)),
		quoted(tag(routeField)),
		quoted(tag(ruleField)),
		quoted(tag(sharedRuleField)),
		quoted(tag(constraintField)),
	}
)
