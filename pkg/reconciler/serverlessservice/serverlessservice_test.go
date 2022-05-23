/*
Copyright 2021 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
istributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package serverlessservice

import (
	"context"
	"testing"

	// Inject our fakes
	istioclient "knative.dev/net-istio/pkg/client/istio/injection/client"
	fakenetworkingclient "knative.dev/networking/pkg/client/injection/client/fake"

	istiov1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"
	"knative.dev/net-istio/pkg/reconciler/ingress/config"
	"knative.dev/net-istio/pkg/reconciler/serverlessservice/resources"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	sksreconciler "knative.dev/networking/pkg/client/injection/reconciler/networking/v1alpha1/serverlessservice"
	netconfig "knative.dev/networking/pkg/config"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"

	. "knative.dev/net-istio/pkg/reconciler/testing"
	. "knative.dev/pkg/reconciler/testing"
)

func sks(name string) *netv1alpha1.ServerlessService {
	sks := &netv1alpha1.ServerlessService{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "testing",
			Name:      name,
		},
		Status: netv1alpha1.ServerlessServiceStatus{
			PrivateServiceName: name + "-foo",
		},
	}

	// Status has no effect on this reconciler, so just default it.
	sks.Status.InitializeConditions()

	return sks
}

func vs(name string) *istiov1alpha3.VirtualService {
	return resources.MakeVirtualService(sks(name))
}

func dr(name string) *istiov1alpha3.DestinationRule {
	return resources.MakeDestinationRule(sks(name))
}

func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name: "bad workqueue key",
		Key:  "too/many/parts",
	}, {
		Name: "key not found",
		Key:  "foo/not-found",
	}, {
		Name: "stable state",
		Key:  "testing/test",
		Objects: []runtime.Object{
			sks("test"),
			vs("test"),
			dr("test"),
		},
	}, {
		Name: "create both",
		Key:  "testing/test",
		Objects: []runtime.Object{
			sks("test"),
		},
		WantCreates: []runtime.Object{
			vs("test"),
			dr("test"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "test-foo"),
			Eventf(corev1.EventTypeNormal, "Created", "Created DestinationRule %q", "test-foo"),
		},
	}, {
		Name: "create only VirtualService",
		Key:  "testing/test",
		Objects: []runtime.Object{
			sks("test"),
			dr("test"),
		},
		WantCreates: []runtime.Object{
			vs("test"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "test-foo"),
		},
	}, {
		Name: "create only DestinationRule",
		Key:  "testing/test",
		Objects: []runtime.Object{
			sks("test"),
			vs("test"),
		},
		WantCreates: []runtime.Object{
			dr("test"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created DestinationRule %q", "test-foo"),
		},
	}, {
		Name: "fix both",
		Key:  "testing/test",
		Objects: []runtime.Object{
			sks("test"),
			func() *istiov1alpha3.VirtualService {
				virtualService := vs("test")
				virtualService.Spec.Hosts = []string{"foo"}
				return virtualService
			}(),
			func() *istiov1alpha3.DestinationRule {
				destinationRule := dr("test")
				destinationRule.Spec.Host = "foo"
				return destinationRule
			}(),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: vs("test"),
		}, {
			Object: dr("test"),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", "Updated VirtualService %s", "testing/test-foo"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated DestinationRule %s", "testing/test-foo"),
		},
	}, {
		Name:    "failure for VirtualService",
		Key:     "testing/test",
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "virtualservices"),
		},
		Objects: []runtime.Object{
			sks("test"),
			dr("test"),
		},
		WantCreates: []runtime.Object{
			vs("test"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "CreationFailed", "Failed to create VirtualService %s: inducing failure for create virtualservices", "testing/test-foo"),
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to reconcile VirtualService: failed to create VirtualService: inducing failure for create virtualservices"),
		},
	}, {
		Name:    "failure for DestinationRule",
		Key:     "testing/test",
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "destinationrules"),
		},
		Objects: []runtime.Object{
			sks("test"),
			vs("test"),
		},
		WantCreates: []runtime.Object{
			dr("test"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "CreationFailed", "Failed to create DestinationRule %s: inducing failure for create destinationrules", "testing/test-foo"),
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to reconcile DestinationRule: failed to create DestinationRule: inducing failure for create destinationrules"),
		},
	}}
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		r := &reconciler{
			istioclient:           istioclient.Get(ctx),
			virtualServiceLister:  listers.GetVirtualServiceLister(),
			destinationRuleLister: listers.GetDestinationRuleLister(),
		}

		return sksreconciler.NewReconciler(ctx, logging.FromContext(ctx), fakenetworkingclient.Get(ctx),
			listers.GetServerlessServiceLister(), controller.GetEventRecorder(ctx), r, controller.Options{
				ConfigStore: &testConfigStore{
					config: &config.Config{
						Istio: &config.Istio{},
						Network: &netconfig.Config{
							EnableMeshPodAddressability: true,
						},
					},
				},
			})
	}))
}

type testConfigStore struct {
	config *config.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, t.config)
}
