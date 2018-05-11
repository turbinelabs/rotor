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

if [[ -z "${ROTOR_CMD}" ]]; then
  echo "error: required environment variable ROTOR_CMD not set" >/dev/stderr
  exit 1
fi

/usr/local/bin/rotor ${ROTOR_CMD}
