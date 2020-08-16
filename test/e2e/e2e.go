/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@.
	// https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"knative.dev/networking/test"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logstream"
)

// Setup creates client to run Knative Service requests
func Setup(t pkgTest.TLegacy) *Clients {
	t.Helper()

	cancel := logstream.Start(t)
	t.Cleanup(cancel)

	clients, err := NewClients(pkgTest.Flags.Kubeconfig, pkgTest.Flags.Cluster, test.ServingNamespace)
	if err != nil {
		t.Fatal("Couldn't initialize clients", "error", err.Error())
	}
	return clients
}
