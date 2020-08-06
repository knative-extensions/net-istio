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

# Download and unpack Istio
if [[ ${ISTIO_VERSION} == "stable" ]]; then
  ISTIO_VERSION="1.5.7"
elif [[ ${ISTIO_VERSION} == "latest" ]]; then
  ISTIO_VERSION="1.6.4"
else
  ISTIO_VERSION="${ISTIO_VERSION:-"1.5.7"}"
fi

if [[ $# != 1 ]]; then
  echo "usage: $0 [ISTIO_OPERATOR_YAML_PATH|ISTIO_OPERATOR_YAML_URL]"; exit 1
fi

if [[ $1 =~ ^https?:// ]]; then
  ISTIO_MANIFEST=$(curl -s -k $1)
else
  ISTIO_MANIFEST=$(cat $1)
fi

SUBCOMMAND="manifest apply"
ISTIO_TARBALL=istio-${ISTIO_VERSION}-linux.tar.gz
if [[ ${ISTIO_VERSION} =~ ^1\.6 ]]; then
  SUBCOMMAND="install"
  ISTIO_TARBALL=istio-${ISTIO_VERSION}-linux-amd64.tar.gz
fi

DOWNLOAD_URL=https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/${ISTIO_TARBALL}

wget --no-check-certificate $DOWNLOAD_URL
if [ $? != 0 ]; then
  echo "Failed to download Istio package"
  exit 1
fi
tar xzf ${ISTIO_TARBALL}

# Install Istio
echo "${ISTIO_MANIFEST}" | ./istio-${ISTIO_VERSION}/bin/istioctl ${SUBCOMMAND} -f -

# Clean up
rm -rf istio-${ISTIO_VERSION}
rm ${ISTIO_TARBALL}
