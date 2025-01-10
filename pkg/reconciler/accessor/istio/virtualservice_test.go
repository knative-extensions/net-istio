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

package istio

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	istiov1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	istioclientset "knative.dev/net-istio/pkg/client/istio/clientset/versioned"
	istiofake "knative.dev/net-istio/pkg/client/istio/clientset/versioned/fake"
	istioinformers "knative.dev/net-istio/pkg/client/istio/informers/externalversions"
	fakeistioclient "knative.dev/net-istio/pkg/client/istio/injection/client/fake"
	istiolisters "knative.dev/net-istio/pkg/client/istio/listers/networking/v1beta1"
	kaccessor "knative.dev/net-istio/pkg/reconciler/accessor"
	"knative.dev/pkg/ptr"

	. "knative.dev/pkg/reconciler/testing"
)

var (
	ownerObj = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ownerObj",
			Namespace: "default",
			UID:       "abcd",
		},
	}

	ownerRef = metav1.OwnerReference{
		Kind:       ownerObj.Kind,
		Name:       ownerObj.Name,
		UID:        ownerObj.UID,
		Controller: ptr.Bool(true),
	}

	origin = &v1beta1.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "vs",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: istiov1beta1.VirtualService{
			Hosts: []string{"origin.example.com"},
		},
	}

	desired = &v1beta1.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "vs",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: istiov1beta1.VirtualService{
			Hosts: []string{"desired.example.com"},
		},
	}

	notOwned = &v1beta1.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vs",
			Namespace: "default",
		},
		Spec: istiov1beta1.VirtualService{
			Hosts: []string{"origin.example.com"},
		},
	}
)

type FakeAccessor struct {
	client   istioclientset.Interface
	vsLister istiolisters.VirtualServiceLister
}

func (f *FakeAccessor) GetIstioClient() istioclientset.Interface {
	return f.client
}

func (f *FakeAccessor) GetVirtualServiceLister() istiolisters.VirtualServiceLister {
	return f.vsLister
}

func TestReconcileVirtualService_Create(t *testing.T) {
	ctx, _ := SetupFakeContext(t)
	ctx, cancel := context.WithCancel(ctx)

	istioClient := fakeistioclient.Get(ctx)

	h := NewHooks()
	h.OnCreate(&istioClient.Fake, "virtualservices", func(obj runtime.Object) HookResult {
		got := obj.(*v1beta1.VirtualService)
		if diff := cmp.Diff(got, desired, protocmp.Transform()); diff != "" {
			t.Log("Unexpected VirtualService (-want, +got):", diff)
			return HookIncomplete
		}
		return HookComplete
	})

	accessor, waitInformers := setup(ctx, []*v1beta1.VirtualService{}, istioClient, t)
	defer func() {
		cancel()
		waitInformers()
	}()

	ReconcileVirtualService(ctx, ownerObj, desired, accessor)

	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error("Failed to Reconcile VirtualService:", err)
	}
}

func TestReconcileVirtualService_Update(t *testing.T) {
	ctx, _ := SetupFakeContext(t)
	ctx, cancel := context.WithCancel(ctx)

	istioClient := fakeistioclient.Get(ctx)
	accessor, waitInformers := setup(ctx, []*v1beta1.VirtualService{origin}, istioClient, t)
	defer func() {
		cancel()
		waitInformers()
	}()

	h := NewHooks()
	h.OnUpdate(&istioClient.Fake, "virtualservices", func(obj runtime.Object) HookResult {
		got := obj.(*v1beta1.VirtualService)
		if diff := cmp.Diff(got, desired, protocmp.Transform()); diff != "" {
			t.Log("Unexpected VirtualService (-want, +got):", diff)
			return HookIncomplete
		}
		return HookComplete
	})

	ReconcileVirtualService(ctx, ownerObj, desired, accessor)
	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error("Failed to Reconcile VirtualService:", err)
	}
}

func TestReconcileVirtualService_NotOwnedFailure(t *testing.T) {
	ctx, _ := SetupFakeContext(t)
	ctx, cancel := context.WithCancel(ctx)

	istioClient := fakeistioclient.Get(ctx)
	accessor, waitInformers := setup(ctx, []*v1beta1.VirtualService{notOwned}, istioClient, t)
	defer func() {
		cancel()
		waitInformers()
	}()

	_, err := ReconcileVirtualService(ctx, ownerObj, desired, accessor)
	if err == nil {
		t.Error("Expected to get error when calling ReconcileVirtualService, but got no error.")
	}
	if !kaccessor.IsNotOwned(err) {
		t.Error("Expected to get NotOwnedError but got", err)
	}
}

func setup(ctx context.Context, vses []*v1beta1.VirtualService,
	istioClient istioclientset.Interface, t *testing.T,
) (*FakeAccessor, func()) {
	fake := istiofake.NewSimpleClientset()
	informer := istioinformers.NewSharedInformerFactory(fake, 0)
	vsInformer := informer.Networking().V1beta1().VirtualServices()

	for _, vs := range vses {
		fake.NetworkingV1beta1().VirtualServices(vs.Namespace).Create(ctx, vs, metav1.CreateOptions{})
		vsInformer.Informer().GetIndexer().Add(vs)
	}

	waitInformers, err := RunAndSyncInformers(ctx, vsInformer.Informer())
	if err != nil {
		t.Fatal("failed to start virtualservice informer:", err)
	}

	return &FakeAccessor{
		client:   istioClient,
		vsLister: vsInformer.Lister(),
	}, waitInformers
}
