//go:build e2e
// +build e2e

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
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/net-istio/pkg/reconciler/ingress/config"
	"knative.dev/net-istio/pkg/reconciler/ingress/resources"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/test"
	"knative.dev/networking/test/conformance/ingress"
	"knative.dev/pkg/system"
)

func TestExposition(t *testing.T) {
	clients := Setup(t)
	namespace := system.Namespace()

	// Save the current config to restore it at the end of the test
	oldConfig, err := clients.KubeClient.CoreV1().ConfigMaps(namespace).Get(context.Background(), config.IstioConfigName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get configmap %s/%s", namespace, config.IstioConfigName)
	}

	// After the test ends, restore the old gateway
	test.EnsureCleanup(t, func() {
		curConfig, err := clients.KubeClient.CoreV1().ConfigMaps(namespace).Get(context.Background(), config.IstioConfigName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get configmap %s/%s", namespace, config.IstioConfigName)
		}

		curConfig.Data = oldConfig.Data
		if _, err := clients.KubeClient.CoreV1().ConfigMaps(namespace).Update(context.Background(), curConfig, metav1.UpdateOptions{}); err != nil {
			t.Fatalf("Failed to restore configmap %s/%s: %v", namespace, config.IstioConfigName, err)
		}
	})

	cases := []struct {
		name                     string
		configMapData            map[string]string
		ingressAnnotation        string
		expectedPrivateIngresses []v1alpha1.LoadBalancerIngressStatus
		expectedPublicIngresses  []v1alpha1.LoadBalancerIngressStatus
	}{
		{
			name: "no exposition",
			configMapData: map[string]string{
				"gateway.knative-serving.knative-ingress-gateway":     "istio-ingressgateway.istio-system.svc.cluster.local",
				"local-gateway.knative-serving.knative-local-gateway": "istio-ingressgateway.istio-system.svc.cluster.local",
			},
			expectedPrivateIngresses: []v1alpha1.LoadBalancerIngressStatus{
				{MeshOnly: false, DomainInternal: "istio-ingressgateway.istio-system.svc.cluster.local"},
			},
			expectedPublicIngresses: []v1alpha1.LoadBalancerIngressStatus{
				{MeshOnly: false, DomainInternal: "istio-ingressgateway.istio-system.svc.cluster.local"},
			},
		},
		{
			name: "match all",
			configMapData: map[string]string{
				"gateway.knative-serving.knative-ingress-gateway":     "istio-ingressgateway.istio-system.svc.cluster.local",
				"local-gateway.knative-serving.knative-local-gateway": "istio-ingressgateway.istio-system.svc.cluster.local",
				"exposition.knative-serving.knative-ingress-gateway":  "expo",
				"exposition.knative-serving.knative-local-gateway":    "expo",
			},
			ingressAnnotation: "expo",
			expectedPrivateIngresses: []v1alpha1.LoadBalancerIngressStatus{
				{MeshOnly: false, DomainInternal: "istio-ingressgateway.istio-system.svc.cluster.local"},
			},
			expectedPublicIngresses: []v1alpha1.LoadBalancerIngressStatus{
				{MeshOnly: false, DomainInternal: "istio-ingressgateway.istio-system.svc.cluster.local"},
			},
		},
		{
			name: "match only public",
			configMapData: map[string]string{
				"gateway.knative-serving.knative-ingress-gateway":     "istio-ingressgateway.istio-system.svc.cluster.local",
				"local-gateway.knative-serving.knative-local-gateway": "istio-ingressgateway.istio-system.svc.cluster.local",
				"exposition.knative-serving.knative-ingress-gateway":  "expo-public",
				"exposition.knative-serving.knative-local-gateway":    "expo-private",
			},
			ingressAnnotation: "expo-public",
			expectedPrivateIngresses: []v1alpha1.LoadBalancerIngressStatus{
				{MeshOnly: true},
			},
			expectedPublicIngresses: []v1alpha1.LoadBalancerIngressStatus{
				{MeshOnly: false, DomainInternal: "istio-ingressgateway.istio-system.svc.cluster.local"},
			},
		},
		{
			name: "match only private",
			configMapData: map[string]string{
				"gateway.knative-serving.knative-ingress-gateway":     "istio-ingressgateway.istio-system.svc.cluster.local",
				"local-gateway.knative-serving.knative-local-gateway": "istio-ingressgateway.istio-system.svc.cluster.local",
				"exposition.knative-serving.knative-ingress-gateway":  "expo-public",
				"exposition.knative-serving.knative-local-gateway":    "expo-private",
			},
			ingressAnnotation: "expo-private",
			expectedPrivateIngresses: []v1alpha1.LoadBalancerIngressStatus{
				{MeshOnly: false, DomainInternal: "istio-ingressgateway.istio-system.svc.cluster.local"},
			},
			expectedPublicIngresses: []v1alpha1.LoadBalancerIngressStatus{
				{MeshOnly: true},
			},
		},
		{
			name: "match nothing",
			configMapData: map[string]string{
				"gateway.knative-serving.knative-ingress-gateway":     "istio-ingressgateway.istio-system.svc.cluster.local",
				"local-gateway.knative-serving.knative-local-gateway": "istio-ingressgateway.istio-system.svc.cluster.local",
				"exposition.knative-serving.knative-ingress-gateway":  "expo",
				"exposition.knative-serving.knative-local-gateway":    "expo",
			},
			ingressAnnotation: "expo-unknown",
			expectedPrivateIngresses: []v1alpha1.LoadBalancerIngressStatus{
				{MeshOnly: true},
			},
			expectedPublicIngresses: []v1alpha1.LoadBalancerIngressStatus{
				{MeshOnly: true},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Update configmap
			cm, err := clients.KubeClient.CoreV1().ConfigMaps(namespace).Get(context.Background(), config.IstioConfigName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get configmap %s/%s", namespace, config.IstioConfigName)
			}

			cm.Data = c.configMapData

			if _, err := clients.KubeClient.CoreV1().ConfigMaps(namespace).Update(context.Background(), cm, metav1.UpdateOptions{}); err != nil {
				t.Fatalf("Failed to update configmap %s/%s: %v", namespace, config.IstioConfigName, err)
			}

			// Create ingress
			name, port, cancel := ingress.CreateRuntimeService(context.Background(), t, clients.NetworkingClient, networking.ServicePortNameHTTP1)
			hosts := []string{name + ".example.com"}

			ing, _, cancel2 := ingress.CreateIngressReady(context.Background(), t, clients.NetworkingClient, v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts:      hosts,
					Visibility: v1alpha1.IngressVisibilityExternalIP,
					HTTP: &v1alpha1.HTTPIngressRuleValue{
						Paths: []v1alpha1.HTTPIngressPath{{
							Splits: []v1alpha1.IngressBackendSplit{{
								IngressBackend: v1alpha1.IngressBackend{
									ServiceName:      name,
									ServiceNamespace: test.ServingNamespace,
									ServicePort:      intstr.FromInt(port),
								},
							}},
						}},
					},
				}},
			})

			// Update annotation
			if c.ingressAnnotation != "" {
				ing.Annotations[resources.ExpositionAnnotation] = c.ingressAnnotation

				// Change the spec to trigger a new generation
				ing.Spec.Rules[0].Hosts = []string{name + ".modified.com"}

				_, err = clients.NetworkingClient.NetworkingClient.Ingresses.Update(context.Background(), ing, metav1.UpdateOptions{})
				if err != nil {
					t.Fatal("Failed to update ingress")
				}
			}

			// Wait ingress to be ready and get its status
			for {
				ing, err = clients.NetworkingClient.NetworkingClient.Ingresses.Get(context.Background(), ing.Name, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Failed to get ingress %s", ing.Name)
				}

				if ing.IsReady() {
					break
				}
			}

			// Check
			diff := cmp.Diff(c.expectedPrivateIngresses, ing.Status.PrivateLoadBalancer.Ingress)
			if diff != "" {
				t.Error("Unexpected private ingresses (-want, +got):", diff)
			}

			diff = cmp.Diff(c.expectedPublicIngresses, ing.Status.PublicLoadBalancer.Ingress)
			if diff != "" {
				t.Error("Unexpected public ingresses (-want, +got):", diff)
			}

			cancel2()
			cancel()
		})
	}
}
