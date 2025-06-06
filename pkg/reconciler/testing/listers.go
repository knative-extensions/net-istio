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

package istio

import (
	istiov1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	fakeistioclientset "istio.io/client-go/pkg/clientset/versioned/fake"
	istiolisters "istio.io/client-go/pkg/listers/networking/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	networking "knative.dev/networking/pkg/apis/networking/v1alpha1"
	fakenetworkingclientset "knative.dev/networking/pkg/client/clientset/versioned/fake"
	networkinglisters "knative.dev/networking/pkg/client/listers/networking/v1alpha1"
	"knative.dev/pkg/reconciler/testing"
)

var clientSetSchemes = []func(*runtime.Scheme) error{
	fakenetworkingclientset.AddToScheme,
	fakeistioclientset.AddToScheme,
	fakekubeclientset.AddToScheme,
}

type Listers struct {
	sorter testing.ObjectSorter
}

func NewListers(objs []runtime.Object) Listers {
	scheme := NewScheme()

	ls := Listers{
		sorter: testing.NewObjectSorter(scheme),
	}

	ls.sorter.AddObjects(objs...)

	return ls
}

func NewScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()

	for _, addTo := range clientSetSchemes {
		addTo(scheme)
	}
	return scheme
}

func (*Listers) NewScheme() *runtime.Scheme {
	return NewScheme()
}

// IndexerFor returns the indexer for the given object.
func (l *Listers) IndexerFor(obj runtime.Object) cache.Indexer {
	return l.sorter.IndexerForObjectType(obj)
}

func (l *Listers) GetNetworkingObjects() []runtime.Object {
	return l.sorter.ObjectsForSchemeFunc(fakenetworkingclientset.AddToScheme)
}

func (l *Listers) GetIstioObjects() []runtime.Object {
	return l.sorter.ObjectsForSchemeFunc(fakeistioclientset.AddToScheme)
}

func (l *Listers) GetKubeObjects() []runtime.Object {
	return l.sorter.ObjectsForSchemeFunc(fakekubeclientset.AddToScheme)
}

// GetIngressLister get lister for Ingress resource.
func (l *Listers) GetIngressLister() networkinglisters.IngressLister {
	return networkinglisters.NewIngressLister(l.IndexerFor(&networking.Ingress{}))
}

// GetServerlessServiceLister get lister for ServerlessService resource.
func (l *Listers) GetServerlessServiceLister() networkinglisters.ServerlessServiceLister {
	return networkinglisters.NewServerlessServiceLister(l.IndexerFor(&networking.ServerlessService{}))
}

// GetGatewayLister get lister for Gateway resource.
func (l *Listers) GetGatewayLister() istiolisters.GatewayLister {
	return istiolisters.NewGatewayLister(l.IndexerFor(&istiov1beta1.Gateway{}))
}

// GetVirtualServiceLister get lister for istio VirtualService resource.
func (l *Listers) GetVirtualServiceLister() istiolisters.VirtualServiceLister {
	return istiolisters.NewVirtualServiceLister(l.IndexerFor(&istiov1beta1.VirtualService{}))
}

// GetDestinationRuleLister get lister for istio DestinationRule resource.
func (l *Listers) GetDestinationRuleLister() istiolisters.DestinationRuleLister {
	return istiolisters.NewDestinationRuleLister(l.IndexerFor(&istiov1beta1.DestinationRule{}))
}

// GetK8sServiceLister get lister for K8s Service resource.
func (l *Listers) GetK8sServiceLister() corev1listers.ServiceLister {
	return corev1listers.NewServiceLister(l.IndexerFor(&corev1.Service{}))
}

// GetEndpointsLister get lister for K8s Endpoints resource.
func (l *Listers) GetEndpointsLister() corev1listers.EndpointsLister {
	return corev1listers.NewEndpointsLister(l.IndexerFor(&corev1.Endpoints{}))
}

// GetSecretLister get lister for K8s Secret resource.
func (l *Listers) GetSecretLister() corev1listers.SecretLister {
	return corev1listers.NewSecretLister(l.IndexerFor(&corev1.Secret{}))
}
