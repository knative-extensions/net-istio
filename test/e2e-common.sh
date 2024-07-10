#!/usr/bin/env bash

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
source $(dirname $0)/../vendor/knative.dev/hack/e2e-tests.sh

# Default to Istio 'latest' version.
ISTIO_VERSION="latest"

# Parse our custom flags.
function parse_flags() {
  case "$1" in
    --istio-version)
      readonly ISTIO_VERSION=$2
      readonly INGRESS_CLASS="istio.ingress.networking.knative.dev"
      return 2
      ;;
    --mesh)
      readonly MESH=1
      return 1
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
  ko apply --platform=linux/amd64 ${KO_FLAGS} -f test/config/ || return 1
  if (( MESH )); then
    kubectl label namespace serving-tests istio-injection=enabled
  fi

  # Bringing up controllers.
  echo ">> Bringing up Istio"
  local istio_dir=third_party/istio-${ISTIO_VERSION}
  local istio_profile=istio-ci-no-mesh

  if (( MESH )); then
    istio_profile=istio-ci-mesh
  fi

  kubectl apply -f ${istio_dir}/${istio_profile} || return 1

  echo ">> Bringing up net-istio Ingress Controller"
  # Do not install Knative Certificate for e2e tests, as we are running without Serving CRs
  ko apply --platform=linux/amd64 -l knative.dev/install-knative-certificate!=true -f config/ || return 1

  if [[ -f "${istio_dir}/${istio_profile}/config-istio.yaml" ]]; then
    kubectl apply -f "${istio_dir}/${istio_profile}/config-istio.yaml"
  fi

  scale_controlplane net-istio-controller net-istio-webhook

  # Wait for pods to be running.
  echo ">> Waiting for Istio components to be running..."
  wait_until_pods_running istio-system || return 1
  echo ">> Waiting for Serving components to be running..."
  wait_until_pods_running knative-serving || return 1

  # Wait for static IP to be through
  wait_until_service_has_external_http_address istio-system istio-ingressgateway
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
