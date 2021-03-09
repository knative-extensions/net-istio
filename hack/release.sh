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

# Documentation about this script and how to use it can be found
# at https://github.com/knative/hack#using-the-releasesh-helper-script

source $(dirname $0)/../vendor/knative.dev/hack/release.sh
source $(dirname $0)/../third_party/download-istio.sh

# Yaml files to generate, and the source config dir for them.
declare -A COMPONENTS
COMPONENTS=(
  ["net-istio.yaml"]="config"
)
readonly COMPONENTS

declare -A RELEASES
RELEASES=(
  ["release.yaml"]="net-istio.yaml"
)
readonly RELEASES

function build_release() {
  # Update release labels if this is a tagged release
  if [[ -n "${TAG}" ]]; then
    echo "Tagged release, updating release labels to serving.knative.dev/release: \"${TAG}\""
    LABEL_YAML_CMD=(sed -e "s|serving.knative.dev/release: devel|serving.knative.dev/release: \"${TAG}\"|")
  else
    echo "Untagged release, will NOT update release labels"
    LABEL_YAML_CMD=(cat)
  fi

  # Build the components
  local all_yamls=()
  for yaml in "${!COMPONENTS[@]}"; do
    local config="${COMPONENTS[${yaml}]}"
    echo "Building Knative net-istio - ${config}"
    echo "# Generated when HEAD was $(git rev-parse HEAD)" > ${yaml}
    echo "#" >> ${yaml}
    ko resolve --strict --platform=all ${KO_FLAGS} -f ${config}/ | "${LABEL_YAML_CMD[@]}" >> ${yaml}
    all_yamls+=(${yaml})
  done
  # Assemble the release
  for yaml in "${!RELEASES[@]}"; do
    echo "Assembling Knative net-istio - ${yaml}"
    echo "" > ${yaml}
    for component in ${RELEASES[${yaml}]}; do
      echo "---" >> ${yaml}
      echo "# ${component}" >> ${yaml}
      cat ${component} >> ${yaml}
    done
    all_yamls+=(${yaml})
  done

  # Build Istio YAML
  ISTIO_STABLE="$(dirname $0)/../third_party/istio-stable"
  ISTIO_VERSION=$(awk '/download_istio/ {print $2}' ${ISTIO_STABLE}/install-istio.sh)
  ISTIO_YAML="istio.yaml"

  echo "Downloading Istio"
  download_istio $ISTIO_VERSION
  trap cleanup_istio EXIT

  echo "Assembling istio.yaml"
  # `istiocl manifest generate` doesn't create the `istio-system` namespace
  cat "${ISTIO_STABLE}/extra/istio-namespace.yaml" > ${ISTIO_YAML}
  ${ISTIO_DIR}/bin/istioctl manifest generate -f $(realpath "${ISTIO_STABLE}/istio-ci-no-mesh.yaml") >> ${ISTIO_YAML}
  all_yamls+=(${ISTIO_YAML})

  ARTIFACTS_TO_PUBLISH="${all_yamls[@]}"
}

main $@
