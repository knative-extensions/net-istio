#!/usr/bin/env bash

# Copyright 2019 The Knative Authors
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

source $(dirname $0)/../../vendor/knative.dev/test-infra/scripts/e2e-tests.sh

kubectl apply -f "$(dirname $0)/istio-crds.yaml" || return 1
wait_until_batch_job_complete istio-system || return 1
kubectl apply -f "$(dirname $0)/$1" || return 1