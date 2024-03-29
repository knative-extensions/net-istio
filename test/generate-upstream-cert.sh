#!/usr/bin/env bash

# Copyright 2023 The Knative Authors
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

ISTIO_NAMESPACE=istio-system
TEST_NAMESPACE=serving-tests
out_dir="$(mktemp -d /tmp/certs-XXX)"
activatorSAN="kn-routing"
serviceSAN="kn-user-$TEST_NAMESPACE"

kubectl create ns $TEST_NAMESPACE
kubectl create ns $ISTIO_NAMESPACE

# Generate Root key and cert.
openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:2048 -subj '/O=Example/CN=Example' -keyout "${out_dir}"/root.key -out "${out_dir}"/root.crt

# Create activator key + cert
openssl req -out "${out_dir}"/activator-tls.csr -newkey rsa:2048 -nodes -keyout "${out_dir}"/activator-tls.key -subj "/CN=Example/O=Example" -addext "subjectAltName = DNS:$activatorSAN"
openssl x509 -req -extfile <(printf "subjectAltName=DNS:$activatorSAN") -days 365 -in "${out_dir}"/activator-tls.csr -CA "${out_dir}"/root.crt -CAkey "${out_dir}"/root.key -CAcreateserial -out "${out_dir}"/activator-tls.crt

# Create test service key + cert
openssl req -out "${out_dir}"/service-tls.csr -newkey rsa:2048 -nodes -keyout "${out_dir}"/service-tls.key -subj "/CN=Example/O=Example" -addext "subjectAltName = DNS:$serviceSAN"
openssl x509 -req -extfile <(printf "subjectAltName=DNS:$serviceSAN") -days 365 -in "${out_dir}"/service-tls.csr -CA "${out_dir}"/root.crt -CAkey "${out_dir}"/root.key -CAcreateserial -out "${out_dir}"/service-tls.crt

# Override certificate in istio-system namespace with the generated CA
# Delete it first, otherwise istio reconciliation does not work properly
kubectl delete -n ${ISTIO_NAMESPACE} secret routing-serving-certs
kubectl create -n ${ISTIO_NAMESPACE} secret generic routing-serving-certs \
    --from-file=ca.crt="${out_dir}"/root.crt \
    --dry-run=client -o yaml |  \
    sed  '/^metadata:/a\ \ labels: {"networking.internal.knative.dev/certificate-uid":"test-id"}' | kubectl apply -f -

# Create test service secret for system-internal-tls
kubectl create -n ${TEST_NAMESPACE} secret tls serving-certs \
    --key="${out_dir}"/service-tls.key \
    --cert="${out_dir}"/service-tls.crt --dry-run=client -o yaml | kubectl apply -f -
