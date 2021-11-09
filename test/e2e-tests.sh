#!/bin/bash

# Copyright 2020 The Knative Authors
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

source $(dirname $0)/e2e-common.sh


# Script entry point.
initialize $@  --skip-istio-addon

# TODO Re-enable conformance tests for mesh when everything's been fixed
# https://github.com/knative-sandbox/net-istio/issues/584
if [[ $MESH -eq 0 ]]; then
  go_test_e2e -timeout=60m ./test/conformance --parallel 12 \
    --enable-beta --enable-alpha || fail_test
fi

go_test_e2e -timeout=10m \
  ./test/e2e || fail_test

success
