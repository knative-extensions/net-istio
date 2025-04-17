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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	istiov1beta1 "istio.io/api/networking/v1beta1"
	istiov1beta1clientset "istio.io/client-go/pkg/apis/networking/v1beta1"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/kmeta"
	pkgnetwork "knative.dev/pkg/network"
)

// Constants defined in virtualservice_test.go can be used here as well
// if they are in the same package. Let's redefine them for clarity just in case,
// though in a real scenario they might be in a shared test helper.
const (
	testDRNamespace = "test-dr-namespace"
	testDRSksName   = "test-dr-sks"
	testDRSvcName   = "test-dr-svc"
)

func TestMakeDestinationRule(t *testing.T) {
	sks := &v1alpha1.ServerlessService{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testDRNamespace,
			Name:      testDRSksName,
			UID:       "test-dr-uid",
		},
		Status: v1alpha1.ServerlessServiceStatus{
			PrivateServiceName: testDRSvcName,
		},
	}

	expectedHost := pkgnetwork.GetServiceHostname(testDRSvcName, testDRNamespace)
	expected := &istiov1beta1clientset.DestinationRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:            testDRSvcName,
			Namespace:       testDRNamespace,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(sks)},
		},
		Spec: istiov1beta1.DestinationRule{
			Host: expectedHost,
			Subsets: []*istiov1beta1.Subset{{
				Name: subsetNormal,
				TrafficPolicy: &istiov1beta1.TrafficPolicy{
					LoadBalancer: &istiov1beta1.LoadBalancerSettings{
						LbPolicy: &istiov1beta1.LoadBalancerSettings_Simple{
							Simple: istiov1beta1.LoadBalancerSettings_LEAST_REQUEST,
						},
					},
				},
			}, {
				Name: subsetDirect,
				TrafficPolicy: &istiov1beta1.TrafficPolicy{
					LoadBalancer: &istiov1beta1.LoadBalancerSettings{
						LbPolicy: &istiov1beta1.LoadBalancerSettings_Simple{
							Simple: istiov1beta1.LoadBalancerSettings_PASSTHROUGH,
						},
					},
				},
			}},
		},
	}

	got := MakeDestinationRule(sks)

	if diff := cmp.Diff(expected, got, protocmp.Transform()); diff != "" {
		t.Errorf("MakeDestinationRule (-want, +got):\n%s", diff)
	}
}
