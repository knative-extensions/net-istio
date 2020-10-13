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
package e2e

import (
	"context"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	kubeclient "knative.dev/pkg/client/injection/kube/client"
	istioclientset "knative.dev/net-istio/pkg/client/istio/clientset/versioned"
	"knative.dev/pkg/injection/clients/dynamicclient"

	// Required to run e2e tests against OpenID based clusters.
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"

	nettest "knative.dev/networking/test"
	istioclient "knative.dev/net-istio/pkg/client/istio/injection/client"
)

// Clients holds instances of interfaces for making requests to Knative Serving.
type Clients struct {
	KubeClient       kubernetes.Interface
	NetworkingClient *nettest.Clients
	Dynamic          dynamic.Interface
	IstioClient      istioclientset.Interface
}

// NewClients instantiates and returns several clientsets required for making request to the
// Knative Serving cluster specified by the combination of clusterName and configPath. Clients can
// make requests within namespace.
func NewClients(ctx context.Context, namespace string) (*Clients, error) {
	clients := &Clients{
		KubeClient:       kubeclient.Get(ctx),
		Dynamic:          dynamicclient.Get(ctx),
		IstioClient:      istioclient.Get(ctx),
	}
	var err error
	clients.NetworkingClient, err = nettest.NewClientsFromCtx(ctx, namespace)
	if err != nil {
		return nil, err
	}

	return clients, nil
}

