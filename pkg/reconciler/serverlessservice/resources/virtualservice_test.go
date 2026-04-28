/*
Copyright 2025 The Knative Authors

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

package resources

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	istiov1beta1 "istio.io/api/networking/v1beta1"
	istiov1beta1clientset "istio.io/client-go/pkg/apis/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/pkg/http/header"
	"knative.dev/pkg/kmeta"
	pkgnetwork "knative.dev/pkg/network"
)

const (
	testNamespace = "test-namespace"
	testSksName   = "test-sks"
	testSvcName   = "test-svc"
)

func TestMakeVirtualService(t *testing.T) {
	sks := &v1alpha1.ServerlessService{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testSksName,
			UID:       "test-uid",
		},
		Spec: v1alpha1.ServerlessServiceSpec{
			Mode: v1alpha1.SKSOperationModeProxy,
		},
		Status: v1alpha1.ServerlessServiceStatus{
			PrivateServiceName: testSvcName,
		},
	}

	expectedHost := pkgnetwork.GetServiceHostname(testSvcName, testNamespace)
	expected := &istiov1beta1clientset.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:            testSvcName,
			Namespace:       testNamespace,
			Labels:          map[string]string{networking.IngressLabelKey: testSksName},
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(sks)},
		},
		Spec: istiov1beta1.VirtualService{
			Hosts: []string{expectedHost},
			Http: []*istiov1beta1.HTTPRoute{{
				Match: []*istiov1beta1.HTTPMatchRequest{{
					Headers: map[string]*istiov1beta1.StringMatch{
						header.PassthroughLoadbalancingKey: {
							MatchType: &istiov1beta1.StringMatch_Exact{
								Exact: "true",
							},
						},
					},
				}},
				Route: []*istiov1beta1.HTTPRouteDestination{{
					Destination: &istiov1beta1.Destination{
						Host:   expectedHost,
						Subset: subsetDirect,
					},
				}},
			}, {
				Route: []*istiov1beta1.HTTPRouteDestination{{
					Destination: &istiov1beta1.Destination{
						Host:   expectedHost,
						Subset: subsetNormal,
					},
				}},
			}},
		},
	}

	got := MakeVirtualService(sks)

	if diff := cmp.Diff(expected, got, protocmp.Transform()); diff != "" {
		t.Errorf("MakeVirtualService (-want, +got):\n%s", diff)
	}
}
