/*
Copyright 2023 The Knative Authors

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
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/kmeta"
)

var (
	host = "myservice-private.svc.cluster.local"
	ing  = &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-ingress",
			Namespace: "my-namespace",
			Annotations: map[string]string{
				"my-annotation": "my-value",
			},
			Labels: map[string]string{
				"my-label":             "my-value-ignored",
				RouteLabelKey:          "my-route",
				RouteNamespaceLabelKey: "my-route-namespace",
			},
		},
	}
)

func TestMakeInternalEncryptionDestinationRuleHttp1(t *testing.T) {
	dr := MakeInternalEncryptionDestinationRule(host, ing, false)
	expected := &v1beta1.DestinationRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:            host,
			Namespace:       ing.Namespace,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing)},
			Annotations: map[string]string{
				"my-annotation": "my-value",
			},
			Labels: map[string]string{
				networking.IngressLabelKey: "my-ingress",
				RouteLabelKey:              "my-route",
				RouteNamespaceLabelKey:     "my-route-namespace",
			},
		},
		Spec: istiov1beta1.DestinationRule{
			Host: host,
			TrafficPolicy: &istiov1beta1.TrafficPolicy{
				Tls: &istiov1beta1.ClientTLSSettings{
					Mode:            istiov1beta1.ClientTLSSettings_SIMPLE,
					CredentialName:  knativeServingCertsSecret,
					SubjectAltNames: []string{knativeFakeDNSName},
				},
			},
		},
	}

	if diff := cmp.Diff(expected, dr, protocmp.Transform()); diff != "" {
		t.Error("Unexpected DestinationRule (-want +got):", diff)
	}
}

func TestMakeInternalEncryptionDestinationRuleHttp2(t *testing.T) {
	dr := MakeInternalEncryptionDestinationRule(host, ing, true)
	expected := &v1beta1.DestinationRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:            host,
			Namespace:       ing.Namespace,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing)},
			Annotations: map[string]string{
				"my-annotation": "my-value",
			},
			Labels: map[string]string{
				networking.IngressLabelKey: "my-ingress",
				RouteLabelKey:              "my-route",
				RouteNamespaceLabelKey:     "my-route-namespace",
			},
		},
		Spec: istiov1beta1.DestinationRule{
			Host: host,
			TrafficPolicy: &istiov1beta1.TrafficPolicy{
				Tls: &istiov1beta1.ClientTLSSettings{
					Mode:            istiov1beta1.ClientTLSSettings_SIMPLE,
					CredentialName:  knativeServingCertsSecret,
					SubjectAltNames: []string{knativeFakeDNSName},
				},
				ConnectionPool: &istiov1beta1.ConnectionPoolSettings{
					Http: &istiov1beta1.ConnectionPoolSettings_HTTPSettings{
						H2UpgradePolicy: istiov1beta1.ConnectionPoolSettings_HTTPSettings_UPGRADE},
				},
			},
		},
	}

	if diff := cmp.Diff(expected, dr, protocmp.Transform()); diff != "" {
		t.Error("Unexpected DestinationRule (-want +got):", diff)
	}
}
