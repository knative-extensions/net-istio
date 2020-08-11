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

# This script includes common functions for testing setup and teardown.
source $(dirname $0)/../vendor/knative.dev/test-infra/scripts/e2e-tests.sh

# Default to Istio 'stable' version.
ISTIO_VERSION="stable"

# Parse our custom flags.
function parse_flags() {
  case "$1" in
    --istio-version)
      readonly ISTIO_VERSION=$2
      readonly INGRESS_CLASS="istio.ingress.networking.knative.dev"
      return 2
      ;;
  esac
  return 0
}

# Setup resources.
function test_setup() {
  echo ">> Setting up logging..."
  # Install kail if needed.
  if ! which kail > /dev/null; then
    bash <( curl -sfL https://raw.githubusercontent.com/boz/kail/master/godownloader.sh) -b "$GOPATH/bin"
  fi
  # Capture all logs.
  kail > ${ARTIFACTS}/k8s.log.txt &
  local kail_pid=$!
  # Clean up kail so it doesn't interfere with job shutting down
  add_trap "kill $kail_pid || true" EXIT

  # Setting up test resources.
  echo ">> Publishing test images"
  $(dirname $0)/upload-test-images.sh || fail_test "Error uploading test images"
  echo ">> Creating test resources (test/config/)"
  ko apply ${KO_FLAGS} -f test/config/ || return 1

  # Bringing up controllers.
  echo ">> Bringing up Istio"
  local istio_dir=third_party/istio-${ISTIO_VERSION}
  #kubectl apply -f ${istio_dir}/istio-crds.yaml || return 1
  #echo ">> Running Istio"
  ${istio_dir}/install-istio.sh istio-ci-no-mesh.yaml || return 1
  echo ">> Bringing up net-istio Ingress Controller"
  ko apply -f config/ || return 1

  scale_controlplane networking-istio istio-webhook
  
  # Wait for pods to be running.
  echo ">> Waiting for Istio components to be running..."
  wait_until_pods_running istio-system || return 1
  echo ">> Waiting for Serving components to be running..."
  wait_until_pods_running knative-serving || return 1

  # Wait for static IP to be through
  wait_until_service_has_external_http_address istio-system istio-ingressgateway
}

function install_serving_nightly() {
  echo ">> Setting up Knative Serving..."
  kubectl apply --filename https://storage.googleapis.com/knative-nightly/serving/latest/serving-crds.yaml || return 1
  kubectl wait --for=condition=Established crd services.serving.knative.dev || return 1

  kubectl apply --filename https://storage.googleapis.com/knative-nightly/serving/latest/serving-core.yaml || return 1
  echo ">> Waiting for Serving components to be running..."
  wait_until_pods_running knative-serving || return 1
}

function scale_controlplane() {
  for deployment in "$@"; do
    # Make sure all pods run in leader-elected mode.
    kubectl -n knative-serving scale deployment "$deployment" --replicas=0 || failed=1
    # Give it time to kill the pods.
    sleep 5
    # Scale up components for HA tests
    kubectl -n knative-serving scale deployment "$deployment" --replicas=2 || failed=1
  done
}

# Add function call to trap
# Parameters: $1 - Function to call
#             $2...$n - Signals for trap
function add_trap() {
  local cmd=$1
  shift
  for trap_signal in $@; do
    local current_trap="$(trap -p $trap_signal | cut -d\' -f2)"
    local new_cmd="($cmd)"
    [[ -n "${current_trap}" ]] && new_cmd="${current_trap};${new_cmd}"
    trap -- "${new_cmd}" $trap_signal
  done
}
