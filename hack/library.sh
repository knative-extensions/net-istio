#!/usr/bin/env bash

# Copyright 2021 The Knative Authors
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

source $(dirname "$0")/../vendor/knative.dev/hack/library.sh

function download_istio() {
  # Find the right arch so we can download the correct istioctl version
  case "${OSTYPE}" in
    darwin*) ARCH=osx ;;
    linux*) ARCH=linux-amd64 ;;
    msys*) ARCH=win ;;
    *) echo "** unknown OS '${OSTYPE}'" ; exit 1 ;;
  esac

  # Download and unpack Istio

  if [ "$1" = "HEAD" ]; then
    ISTIO_VERSION=$(curl https://storage.googleapis.com/istio-build/dev/latest)
  else
    ISTIO_VERSION=$1
  fi

  ISTIO_TARBALL=istio-${ISTIO_VERSION}-${ARCH}.tar.gz

  if [ "$1" = "HEAD" ]; then
    DOWNLOAD_URL=https://storage.googleapis.com/istio-build/dev/${ISTIO_VERSION}/${ISTIO_TARBALL}
  else
    DOWNLOAD_URL=https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/${ISTIO_TARBALL}
  fi

  ISTIO_TMP=$(mktemp -d)
  pushd "$ISTIO_TMP"
  curl -LO "$DOWNLOAD_URL"
  if [ $? != 0 ]; then
    echo "Failed to download Istio release: $DOWNLOAD_URL"
    exit 1
  fi
  tar xzf "${ISTIO_TARBALL}"
  ISTIO_DIR="${ISTIO_TMP}/istio-${ISTIO_VERSION}"
  echo "Istio was downloaded to ${ISTIO_DIR}"
  popd || exit
}

function cleanup_istio() {
  echo "Deleting $ISTIO_TMP"
  rm -rf "$ISTIO_TMP"
}

function add_crd_label() {
  local lib_path="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  go_run github.com/vmware-tanzu/carvel-ytt/cmd/ytt@v0.43.0 \
    -f - \
    -f <(cat <<EOF
#@ load("@ytt:overlay", "overlay")

#@overlay/match by=overlay.subset({"kind":"CustomResourceDefinition"}), expects="1+"
---
metadata:
  labels:
    #@overlay/match missing_ok=True
    knative.dev/crd-install: "true"
EOF
)
}

function generate_manifests() {
  local dir=$1
  shift

  for file in $(find -L "$dir" -maxdepth 1 -name "istio-*.yaml"); do
    local filename=$(basename "$file")
    local filename=${filename%%.*}
    local target_dir="$(dirname "$file")/${filename}"

    mkdir -p "$target_dir"

    echo "Generating manifest from $(basename $(dirname "$file"))/$(basename "$file")"

    tmpfile=$(mktemp)
    rm "${target_dir}/istio.yaml" || true

    # manifest generate doesn't include the istio namespace
    cat <<EOF > "${tmpfile}"
apiVersion: v1
kind: Namespace
metadata:
  name: istio-system
---
EOF

    "${ISTIO_DIR}"/bin/istioctl manifest generate -f "$file" "$@" | add_crd_label >> "${tmpfile}"

    # Add any resources that weren't on our prioritized list
    cat "${tmpfile}" >> "${target_dir}/istio.yaml"

    local config_istio_extra="$dir/extra/config-istio.yaml"
    local istio_extra="$dir/extra/istio.yaml"

    if [[ "$file" == *"mesh"* ]] && [[ "$file" != *"no-mesh"* ]] ; then
      config_istio_extra="$dir/extra/config-istio-mesh.yaml"
      istio_extra="$dir/extra/istio-mesh.yaml"
    fi

    if [[ -f "${config_istio_extra}" ]]; then
      echo "  copying ${config_istio_extra}"
      cp "${config_istio_extra}" "${target_dir}/config-istio.yaml"
    fi

    if [[ -f "${istio_extra}" ]]; then
      echo "  appending ${istio_extra}"
      cat "${istio_extra}" >> "${target_dir}/istio.yaml"
    fi
  done
}

function generate() {
  local istio_version="$1"
  local path="$2"
  shift 2

  download_istio "$istio_version"
  trap cleanup_istio EXIT

  generate_manifests "$path" "$@"
}
