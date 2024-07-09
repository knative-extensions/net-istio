/*
Copyright 2021 The Knative Authors

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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	istiov1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	istioclientset "knative.dev/net-istio/pkg/client/istio/clientset/versioned"
	fakeistioclient "knative.dev/net-istio/pkg/client/istio/injection/client/fake"
	fakedrinformer "knative.dev/net-istio/pkg/client/istio/injection/informers/networking/v1beta1/destinationrule/fake"
	istiolisters "knative.dev/net-istio/pkg/client/istio/listers/networking/v1beta1"
	kaccessor "knative.dev/net-istio/pkg/reconciler/accessor"

	. "knative.dev/pkg/reconciler/testing"
)

var (
	originDR = &v1beta1.DestinationRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "dr",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: istiov1beta1.DestinationRule{
			Host: "origin.example.com",
		},
	}

	desiredDR = &v1beta1.DestinationRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "dr",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: istiov1beta1.DestinationRule{
			Host: "desired.example.com",
		},
	}

	notOwnedDR = &v1beta1.DestinationRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dr",
			Namespace: "default",
		},
		Spec: istiov1beta1.DestinationRule{
			Host: "origin.example.com",
		},
	}
)

type FakeDestinatioRuleAccessor struct {
	client   istioclientset.Interface
	drLister istiolisters.DestinationRuleLister
}

func (f *FakeDestinatioRuleAccessor) GetIstioClient() istioclientset.Interface {
	return f.client
}

func (f *FakeDestinatioRuleAccessor) GetDestinationRuleLister() istiolisters.DestinationRuleLister {
	return f.drLister
}

func TestReconcileDestinationRule_Create(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t)

	istio := fakeistioclient.Get(ctx)
	drInformer := fakedrinformer.Get(ctx)

	waitInformers, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		t.Fatal("Failed to start informers")
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	accessor := &FakeDestinatioRuleAccessor{
		client:   istio,
		drLister: drInformer.Lister(),
	}

	h := NewHooks()
	h.OnCreate(&istio.Fake, "destinationrules", func(obj runtime.Object) HookResult {
		got := obj.(*v1beta1.DestinationRule)
		if diff := cmp.Diff(got, desiredDR, protocmp.Transform()); diff != "" {
			t.Log("Unexpected DestinationRule (-want, +got):", diff)
			return HookIncomplete
		}
		return HookComplete
	})

	ReconcileDestinationRule(ctx, ownerObj, desiredDR, accessor)

	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error("Failed to Reconcile DestinationRule:", err)
	}
}

func TestReconcileDestinationRule_Update(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t)

	istio := fakeistioclient.Get(ctx)
	drInformer := fakedrinformer.Get(ctx)

	waitInformers, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		t.Fatal("Failed to start informers")
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	accessor := &FakeDestinatioRuleAccessor{
		client:   istio,
		drLister: drInformer.Lister(),
	}

	istio.NetworkingV1beta1().DestinationRules(origin.Namespace).Create(ctx, originDR, metav1.CreateOptions{})
	drInformer.Informer().GetIndexer().Add(originDR)

	h := NewHooks()
	h.OnUpdate(&istio.Fake, "destinationrules", func(obj runtime.Object) HookResult {
		got := obj.(*v1beta1.DestinationRule)
		if diff := cmp.Diff(got, desiredDR, protocmp.Transform()); diff != "" {
			t.Log("Unexpected DestinationRule (-want, +got):", diff)
			return HookIncomplete
		}
		return HookComplete
	})

	ReconcileDestinationRule(ctx, ownerObj, desiredDR, accessor)
	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error("Failed to Reconcile DestinationRule:", err)
	}
}

func TestReconcileDestinationRule_NotOwnedFailure(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t)

	istio := fakeistioclient.Get(ctx)
	drInformer := fakedrinformer.Get(ctx)

	waitInformers, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		t.Fatal("Failed to start informers")
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	accessor := &FakeDestinatioRuleAccessor{
		client:   istio,
		drLister: drInformer.Lister(),
	}

	istio.NetworkingV1beta1().DestinationRules(origin.Namespace).Create(ctx, notOwnedDR, metav1.CreateOptions{})
	drInformer.Informer().GetIndexer().Add(notOwnedDR)

	_, err = ReconcileDestinationRule(ctx, ownerObj, desiredDR, accessor)
	if err == nil {
		t.Error("Expected to get error when calling ReconcileDestinationRule, but got no error.")
	}
	if !kaccessor.IsNotOwned(err) {
		t.Error("Expected to get NotOwnedError but got", err)
	}
}
