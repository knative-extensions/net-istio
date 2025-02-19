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

set -o errexit
set -o nounset
set -o pipefail

source $(dirname $0)/../vendor/knative.dev/hack/codegen-library.sh

# hack's codegen shell library overrides GOBIN
# we need it on the path so run_go_tool works
export PATH="${PATH}:${GOBIN}"


echo "=== Update Codegen for $MODULE_NAME"

group "Knative Codegen"

# Knative Injection (for istio)
${KNATIVE_CODEGEN_PKG}/hack/generate-knative.sh "injection" \
  knative.dev/net-istio/pkg/client/istio istio.io/client-go/pkg/apis \
  "networking:v1beta1" \
  --go-header-file ${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt

source "${CODEGEN_PKG}/kube_codegen.sh"

group "Kubernetes Codegen"

# Generate our own client for istio (otherwise injection won't work)
kube::codegen::gen_client \
  --boilerplate "${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt" \
  --output-dir "${REPO_ROOT_DIR}/pkg/client/istio" \
  --output-pkg "knative.dev/net-istio/pkg/client/istio" \
  --one-input-api "networking" \
  --with-watch \
  "${REPO_ROOT_DIR}/vendor/istio.io/client-go/pkg/apis"

group "Deepcopy Gen"

kube::codegen::gen_helpers \
  --boilerplate "${REPO_ROOT_DIR}/hack/boilerplate/boilerplate.go.txt" \
  --extra-peer-dir "reconciler/ingress/config" \
  --extra-peer-dir "defaults" \
  "${REPO_ROOT_DIR}/pkg"

group "Update deps post-codegen"
# Make sure our dependencies are up-to-date
${REPO_ROOT_DIR}/hack/update-deps.sh
