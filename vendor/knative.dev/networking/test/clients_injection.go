/*
Copyright 2020 The Knative Authors

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

// This file contains an object which encapsulates k8s clients which are useful for e2e tests.

package test

import (
	"context"

	// Allow E2E to run against a cluster using OpenID.
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"

	"knative.dev/pkg/injection/clients/dynamicclient"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	networkingclient "knative.dev/networking/pkg/client/injection/client"
)

// NewClientsFromCtx instantiates and returns several clientsets required for making request to the
// Knative Serving cluste. Networking clients can make requests within namespace.
func NewClientsFromCtx(ctx context.Context, namespace string) (*Clients, error) {
	cs := networkingclient.Get(ctx)
	clients := &Clients{
		KubeClient:       kubeclient.Get(ctx),
		NetworkingClient: &NetworkingClients{
			ServerlessServices: cs.NetworkingV1alpha1().ServerlessServices(namespace),
			Ingresses:          cs.NetworkingV1alpha1().Ingresses(namespace),
			Certificates:       cs.NetworkingV1alpha1().Certificates(namespace),
		},
		Dynamic:          dynamicclient.Get(ctx),
	}

	return clients, nil
}
