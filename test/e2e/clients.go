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
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	nettest "knative.dev/networking/test"
	"knative.dev/pkg/test"
	servingv1 "knative.dev/serving/pkg/client/clientset/versioned/typed/serving/v1"
	istioclientset "knative.dev/serving/pkg/client/istio/clientset/versioned"
)

// Clients holds instances of interfaces for making requests to Knative Serving.
type Clients struct {
	KubeClient       *test.KubeClient
	NetworkingClient *nettest.NetworkingClients
	Dynamic          dynamic.Interface
	IstioClient      istioclientset.Interface
	ServingClient    *servingClients
	nettest.Client
}

type Interface interface {
	ServingV1() servingv1.ServingV1Interface
}

// servingClients holds instances of interfaces for making requests to knative serving clients
type servingClients struct {
	Routes    servingv1.RouteInterface
	Configs   servingv1.ConfigurationInterface
	Revisions servingv1.RevisionInterface
	Services  servingv1.ServiceInterface
}

// NewClients instantiates and returns several clientsets required for making request to the
// Knative Serving cluster specified by the combination of clusterName and configPath. Clients can
// make requests within namespace.
func NewClients(configPath string, clusterName string, namespace string) (*Clients, error) {
	cfg, err := BuildClientConfig(configPath, clusterName)
	if err != nil {
		return nil, err
	}

	// We poll, so set our limits high.
	cfg.QPS = 100
	cfg.Burst = 200

	return NewClientsFromConfig(cfg, namespace)
}

// NewClientsFromConfig instantiates and returns several clientsets required for making request to the
// Knative Serving cluster specified by the rest Config. Clients can make requests within namespace.
func NewClientsFromConfig(cfg *rest.Config, namespace string) (*Clients, error) {
	clients := &Clients{}
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	clients.KubeClient = &test.KubeClient{Kube: kubeClient}

	clients.Dynamic, err = dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	clients.IstioClient, err = istioclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	clients.ServingClient, err = newServingClients(cfg, namespace)
	if err != nil {
		return nil, err
	}

	return clients, nil
}

// newNetworkingClients instantiates and returns the networking clientset required to make requests
// to Networking resources on the Knative service cluster
func newServingClients(cfg *rest.Config, namespace string) (*servingClients, error) {
	cs, err := servingv1.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &servingClients{
		Configs:   cs.Configurations(namespace),
		Revisions: cs.Revisions(namespace),
		Routes:    cs.Routes(namespace),
		Services:  cs.Services(namespace),
	}, nil
}

// BuildClientConfig builds client config for testing.
func BuildClientConfig(kubeConfigPath string, clusterName string) (*rest.Config, error) {
	overrides := clientcmd.ConfigOverrides{}
	// Override the cluster name if provided.
	if clusterName != "" {
		overrides.Context.Cluster = clusterName
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfigPath},
		&overrides).ClientConfig()
}
