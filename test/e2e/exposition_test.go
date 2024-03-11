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
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/net-istio/pkg/reconciler/ingress/config"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/test"
	"knative.dev/networking/test/conformance/ingress"
	"knative.dev/pkg/system"
)

func TestExposition(t *testing.T) {
	mesh, meshSet := os.LookupEnv("MESH")
	if meshSet && mesh == "1" {
		return
	}

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
		ingressLabels            map[string]string
		expectedPrivateIngresses []v1alpha1.LoadBalancerIngressStatus
		expectedPublicIngresses  []v1alpha1.LoadBalancerIngressStatus
	}{
		{
			name: "no label",
			configMapData: map[string]string{
				"local-gateways": replaceTabs(`
				- namespace: "knative-serving"
				  name: "knative-local-gateway"
				  service: "istio-ingressgateway.istio-system.svc.cluster.local"`),

				"external-gateways": replaceTabs(`
				- namespace: "knative-serving"
				  name: "knative-ingress-gateway"
				  service: "istio-ingressgateway.istio-system.svc.cluster.local"`),
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
				"local-gateways": replaceTabs(`
				- namespace: "knative-serving"
				  name: "knative-local-gateway"
				  service: "istio-ingressgateway.istio-system.svc.cluster.local"`),

				"external-gateways": replaceTabs(`
				- namespace: "unused"
				  name: "unused"
				  service: "default.default.svc.cluster.local"
				- namespace: "knative-serving"
				  name: "knative-ingress-gateway"
				  service: "istio-ingressgateway.istio-system.svc.cluster.local"
				  labelSelector:
					matchExpressions:
					- key: "foo"
					  operator: "In"
					  values: ["bar"]`),
			},
			ingressLabels: map[string]string{"foo": "bar"},
			expectedPrivateIngresses: []v1alpha1.LoadBalancerIngressStatus{
				{MeshOnly: false, DomainInternal: "istio-ingressgateway.istio-system.svc.cluster.local"},
			},
			expectedPublicIngresses: []v1alpha1.LoadBalancerIngressStatus{
				{MeshOnly: false, DomainInternal: "istio-ingressgateway.istio-system.svc.cluster.local"},
			},
		},
		{
			name: "match only one",
			configMapData: map[string]string{
				"local-gateways": replaceTabs(`
				- namespace: "knative-serving"
				  name: "knative-local-gateway"
				  service: "istio-ingressgateway.istio-system.svc.cluster.local"`),

				"external-gateways": replaceTabs(`
				- namespace: "unused"
				  name: "unused"
				  service: "default.default.svc.cluster.local"
				- namespace: "knative-serving"
				  name: "knative-ingress-gateway"
				  service: "istio-ingressgateway-1.istio-system.svc.cluster.local"
				  labelSelector:
					matchExpressions:
					- key: "foo"
					  operator: "In"
					  values: ["bar"]
				- namespace: "knative-serving"
				  name: "knative-local-gateway"
				  service: "istio-ingressgateway-2.istio-system.svc.cluster.local"
				  labelSelector:
					matchExpressions:
					- key: "wont"
					  operator: "In"
					  values: ["match"]`),
			},
			ingressLabels: map[string]string{"foo": "bar"},
			expectedPrivateIngresses: []v1alpha1.LoadBalancerIngressStatus{
				{MeshOnly: false, DomainInternal: "istio-ingressgateway.istio-system.svc.cluster.local"},
			},
			expectedPublicIngresses: []v1alpha1.LoadBalancerIngressStatus{
				{MeshOnly: false, DomainInternal: "istio-ingressgateway-1.istio-system.svc.cluster.local"},
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

			spec := v1alpha1.IngressSpec{
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
			}

			ing, cancel2 := ingress.CreateIngress(context.Background(), t, clients.NetworkingClient, spec, func(target *v1alpha1.Ingress) {
				if target.ObjectMeta.Labels == nil {
					target.ObjectMeta.Labels = make(map[string]string)
				}

				for k, v := range c.ingressLabels {
					target.ObjectMeta.Labels[k] = v
				}
			})

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

			// Restore configmap
			cm, err = clients.KubeClient.CoreV1().ConfigMaps(namespace).Get(context.Background(), config.IstioConfigName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get configmap %s/%s", namespace, config.IstioConfigName)
			}

			cm.Data = oldConfig.Data

			if _, err := clients.KubeClient.CoreV1().ConfigMaps(namespace).Update(context.Background(), cm, metav1.UpdateOptions{}); err != nil {
				t.Fatalf("Failed to restore configmap %s/%s: %v", namespace, config.IstioConfigName, err)
			}
		})
	}
}

func replaceTabs(s string) string {
	return strings.ReplaceAll(s, "\t", "    ")
}
