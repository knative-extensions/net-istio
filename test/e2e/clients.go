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
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	istioclientset "knative.dev/net-istio/pkg/client/istio/clientset/versioned"
	nettest "knative.dev/networking/test"

	// Required to run e2e tests against OpenID based clusters.
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

// Clients holds instances of interfaces for making requests to Knative Serving.
type Clients struct {
	KubeClient       kubernetes.Interface
	NetworkingClient *nettest.Clients
	Dynamic          dynamic.Interface
	IstioClient      istioclientset.Interface
}

// NewClientsFromConfig instantiates and returns several clientsets required for making request to the
// Knative Serving cluster specified by the rest Config. Clients can make requests within namespace.
func NewClientsFromConfig(cfg *rest.Config, namespace string) (*Clients, error) {
	// We poll, so set our limits high.
	cfg.QPS = 100
	cfg.Burst = 200

	var (
		err     error
		clients Clients
	)

	clients.KubeClient, err = kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	clients.Dynamic, err = dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	clients.IstioClient, err = istioclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	clients.NetworkingClient, err = nettest.NewClientsFromConfig(cfg, nettest.ServingNamespace)
	if err != nil {
		return nil, err
	}

	return &clients, nil
}
