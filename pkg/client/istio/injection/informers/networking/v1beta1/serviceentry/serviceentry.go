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

// Code generated by injection-gen. DO NOT EDIT.

package serviceentry

import (
	context "context"

	apisnetworkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	cache "k8s.io/client-go/tools/cache"
	versioned "knative.dev/net-istio/pkg/client/istio/clientset/versioned"
	v1beta1 "knative.dev/net-istio/pkg/client/istio/informers/externalversions/networking/v1beta1"
	client "knative.dev/net-istio/pkg/client/istio/injection/client"
	factory "knative.dev/net-istio/pkg/client/istio/injection/informers/factory"
	networkingv1beta1 "knative.dev/net-istio/pkg/client/istio/listers/networking/v1beta1"
	controller "knative.dev/pkg/controller"
	injection "knative.dev/pkg/injection"
	logging "knative.dev/pkg/logging"
)

func init() {
	injection.Default.RegisterInformer(withInformer)
	injection.Dynamic.RegisterDynamicInformer(withDynamicInformer)
}

// Key is used for associating the Informer inside the context.Context.
type Key struct{}

func withInformer(ctx context.Context) (context.Context, controller.Informer) {
	f := factory.Get(ctx)
	inf := f.Networking().V1beta1().ServiceEntries()
	return context.WithValue(ctx, Key{}, inf), inf.Informer()
}

func withDynamicInformer(ctx context.Context) context.Context {
	inf := &wrapper{client: client.Get(ctx), resourceVersion: injection.GetResourceVersion(ctx)}
	return context.WithValue(ctx, Key{}, inf)
}

// Get extracts the typed informer from the context.
func Get(ctx context.Context) v1beta1.ServiceEntryInformer {
	untyped := ctx.Value(Key{})
	if untyped == nil {
		logging.FromContext(ctx).Panic(
			"Unable to fetch knative.dev/net-istio/pkg/client/istio/informers/externalversions/networking/v1beta1.ServiceEntryInformer from context.")
	}
	return untyped.(v1beta1.ServiceEntryInformer)
}

type wrapper struct {
	client versioned.Interface

	namespace string

	resourceVersion string
}

var _ v1beta1.ServiceEntryInformer = (*wrapper)(nil)
var _ networkingv1beta1.ServiceEntryLister = (*wrapper)(nil)

func (w *wrapper) Informer() cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(nil, &apisnetworkingv1beta1.ServiceEntry{}, 0, nil)
}

func (w *wrapper) Lister() networkingv1beta1.ServiceEntryLister {
	return w
}

func (w *wrapper) ServiceEntries(namespace string) networkingv1beta1.ServiceEntryNamespaceLister {
	return &wrapper{client: w.client, namespace: namespace, resourceVersion: w.resourceVersion}
}

// SetResourceVersion allows consumers to adjust the minimum resourceVersion
// used by the underlying client.  It is not accessible via the standard
// lister interface, but can be accessed through a user-defined interface and
// an implementation check e.g. rvs, ok := foo.(ResourceVersionSetter)
func (w *wrapper) SetResourceVersion(resourceVersion string) {
	w.resourceVersion = resourceVersion
}

func (w *wrapper) List(selector labels.Selector) (ret []*apisnetworkingv1beta1.ServiceEntry, err error) {
	lo, err := w.client.NetworkingV1beta1().ServiceEntries(w.namespace).List(context.TODO(), v1.ListOptions{
		LabelSelector:   selector.String(),
		ResourceVersion: w.resourceVersion,
	})
	if err != nil {
		return nil, err
	}
	for idx := range lo.Items {
		ret = append(ret, lo.Items[idx])
	}
	return ret, nil
}

func (w *wrapper) Get(name string) (*apisnetworkingv1beta1.ServiceEntry, error) {
	return w.client.NetworkingV1beta1().ServiceEntries(w.namespace).Get(context.TODO(), name, v1.GetOptions{
		ResourceVersion: w.resourceVersion,
	})
}
