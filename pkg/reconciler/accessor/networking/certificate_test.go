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

package networking

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	kaccessor "knative.dev/net-istio/pkg/reconciler/accessor"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	clientset "knative.dev/networking/pkg/client/clientset/versioned"
	fakenetworkingclient "knative.dev/networking/pkg/client/injection/client/fake"
	fakecertinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/certificate/fake"
	listers "knative.dev/networking/pkg/client/listers/networking/v1alpha1"
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

	origin = &v1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "cert",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: v1alpha1.CertificateSpec{
			DNSNames:   []string{"origin.example.com"},
			SecretName: "secret0",
		},
	}

	desired = &v1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "cert",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: v1alpha1.CertificateSpec{
			DNSNames:   []string{"desired.example.com"},
			SecretName: "secret0",
		},
	}

	notOwned = &v1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cert",
			Namespace: "default",
		},
		Spec: v1alpha1.CertificateSpec{
			DNSNames:   []string{"origin.example.com"},
			SecretName: "secret0",
		},
	}
)

type FakeAccessor struct {
	client     clientset.Interface
	certLister listers.CertificateLister
}

func (f *FakeAccessor) GetNetworkingClient() clientset.Interface {
	return f.client
}

func (f *FakeAccessor) GetCertificateLister() listers.CertificateLister {
	return f.certLister
}

func TestReconcileCertificateCreate(t *testing.T) {
	ctx, cancel, _ := SetupFakeContextWithCancel(t)

	client := fakenetworkingclient.Get(ctx)

	h := NewHooks()
	h.OnCreate(&client.Fake, "certificates", func(obj runtime.Object) HookResult {
		got := obj.(*v1alpha1.Certificate)
		if diff := cmp.Diff(got, desired); diff != "" {
			t.Log("Unexpected Certificate (-want, +got):", diff)
			return HookIncomplete
		}
		return HookComplete
	})

	accessor, waitInformers := setup(ctx, []*v1alpha1.Certificate{}, client, t)
	defer func() {
		cancel()
		waitInformers()
	}()

	ReconcileCertificate(ctx, ownerObj, desired, accessor)

	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error("Failed to Reconcile Certificate:", err)
	}
}

func TestReconcileCertificateUpdate(t *testing.T) {
	ctx, cancel, _ := SetupFakeContextWithCancel(t)

	client := fakenetworkingclient.Get(ctx)
	accessor, waitInformers := setup(ctx, []*v1alpha1.Certificate{origin}, client, t)
	defer func() {
		cancel()
		waitInformers()
	}()

	h := NewHooks()
	h.OnUpdate(&client.Fake, "certificates", func(obj runtime.Object) HookResult {
		got := obj.(*v1alpha1.Certificate)
		if diff := cmp.Diff(got, desired); diff != "" {
			t.Log("Unexpected Certificate (-want, +got):", diff)
			return HookIncomplete
		}
		return HookComplete
	})

	ReconcileCertificate(ctx, ownerObj, desired, accessor)
	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error("Failed to Reconcile Certificate:", err)
	}
}

func TestNotOwnedFailure(t *testing.T) {
	ctx, cancel, _ := SetupFakeContextWithCancel(t)

	client := fakenetworkingclient.Get(ctx)
	accessor, waitInformers := setup(ctx, []*v1alpha1.Certificate{notOwned}, client, t)
	defer func() {
		cancel()
		waitInformers()
	}()

	_, err := ReconcileCertificate(ctx, ownerObj, desired, accessor)
	if err == nil {
		t.Error("Expected to get error when calling ReconcileCertificate, but got no error.")
	}
	if !kaccessor.IsNotOwned(err) {
		t.Error("Expected to get NotOwnedError but got", err)
	}
}

func setup(ctx context.Context, certs []*v1alpha1.Certificate,
	client clientset.Interface, t *testing.T,
) (*FakeAccessor, func()) {
	fake := fakenetworkingclient.Get(ctx)
	certInformer := fakecertinformer.Get(ctx)

	for _, cert := range certs {
		fake.NetworkingV1alpha1().Certificates(cert.Namespace).Create(ctx, cert, metav1.CreateOptions{})
		certInformer.Informer().GetIndexer().Add(cert)
	}

	waitInformers, err := RunAndSyncInformers(ctx, certInformer.Informer())
	if err != nil {
		t.Fatal("failed to start Certificate informer:", err)
	}

	return &FakeAccessor{
		client:     client,
		certLister: certInformer.Lister(),
	}, waitInformers
}
