/*
Copyright 2018 The Knative Authors.

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

package config

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/system"

	. "knative.dev/pkg/configmap/testing"
	_ "knative.dev/pkg/system/testing"
)

func TestIstio(t *testing.T) {
	cm, example := ConfigMapsFromTestFile(t, IstioConfigName)

	if _, err := NewIstioFromConfigMap(cm); err != nil {
		t.Error("NewIstioFromConfigMap(actual) =", err)
	}

	if _, err := NewIstioFromConfigMap(example); err != nil {
		t.Error("NewIstioFromConfigMap(example) =", err)
	}
}

func TestQualifiedName(t *testing.T) {
	g := Gateway{
		Namespace: "foo",
		Name:      "bar",
	}
	expected := "foo/bar"
	saw := g.QualifiedName()
	if saw != expected {
		t.Errorf("Expected %q, saw %q", expected, saw)
	}
}

func TestGatewayConfiguration(t *testing.T) {
	gatewayConfigTests := []struct {
		name      string
		wantErr   bool
		wantIstio *Istio
		config    *corev1.ConfigMap
	}{{
		name: "gateway configuration with no network input",
		wantIstio: &Istio{
			IngressGateways: defaultIngressGateways(),
			LocalGateways:   defaultLocalGateways(),
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
		},
	}, {
		name:    "gateway configuration with invalid url",
		wantErr: true,
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"gateway.invalid": "_invalid",
			},
		},
	}, {
		name: "gateway configuration with valid url",
		wantIstio: &Istio{
			IngressGateways: []Gateway{{
				Namespace:  "knative-testing",
				Name:       "knative-ingress-freeway",
				ServiceURL: "istio-ingressfreeway.istio-system.svc.cluster.local",
			}},
			LocalGateways: defaultLocalGateways(),
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"gateway.knative-ingress-freeway": "istio-ingressfreeway.istio-system.svc.cluster.local",
			},
		},
	}, {
		name: "gateway configuration with valid url having a dot at the end",
		wantIstio: &Istio{
			IngressGateways: []Gateway{{
				Namespace:  "knative-testing",
				Name:       "knative-ingress-freeway",
				ServiceURL: "istio-ingressfreeway.istio-system.svc.cluster.local.",
			}},
			LocalGateways: defaultLocalGateways(),
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"gateway.knative-ingress-freeway": "istio-ingressfreeway.istio-system.svc.cluster.local.",
			},
		},
	}, {
		name: "gateway configuration in custom namespace with valid url",
		wantIstio: &Istio{
			IngressGateways: []Gateway{{
				Namespace:  "custom-namespace",
				Name:       "custom-gateway",
				ServiceURL: "istio-ingressfreeway.istio-system.svc.cluster.local",
			}},
			LocalGateways: defaultLocalGateways(),
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"gateway.custom-namespace.custom-gateway": "istio-ingressfreeway.istio-system.svc.cluster.local",
			},
		},
	}, {
		name:    "gateway configuration in custom namespace with invalid url",
		wantErr: true,
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"gateway.custom-namespace.invalid": "_invalid",
			},
		},
	}, {
		name: "local gateway configuration with valid url",
		wantIstio: &Istio{
			IngressGateways: defaultIngressGateways(),
			LocalGateways: []Gateway{{
				Namespace:  "knative-testing",
				Name:       "knative-ingress-backroad",
				ServiceURL: "istio-ingressbackroad.istio-system.svc.cluster.local",
			}},
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"local-gateway.knative-ingress-backroad": "istio-ingressbackroad.istio-system.svc.cluster.local",
			},
		},
	}, {
		name:    "local gateway configuration with invalid url",
		wantErr: true,
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"local-gateway.invalid": "_invalid",
			},
		},
	}, {
		name: "local gateway configuration in custom namespace with valid url",
		wantIstio: &Istio{
			IngressGateways: defaultIngressGateways(),
			LocalGateways: []Gateway{{
				Namespace:  "custom-namespace",
				Name:       "custom-local-gateway",
				ServiceURL: "istio-ingressbackroad.istio-system.svc.cluster.local",
			}},
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"local-gateway.custom-namespace.custom-local-gateway": "istio-ingressbackroad.istio-system.svc.cluster.local",
			},
		},
	}, {
		name:    "local gateway configuration in custom namespace with invalid url",
		wantErr: true,
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"local-gateway.custom-namespace.invalid": "_invalid",
			},
		},
	}, {
		name: "new format alone",
		wantIstio: &Istio{
			IngressGateways: []Gateway{{
				Namespace:  "namespace1",
				Name:       "gateway1",
				ServiceURL: "istio-ingressbackroad.istio-system.svc.cluster.local",
			}},
			LocalGateways: []Gateway{{
				Namespace:  "namespace2",
				Name:       "gateway2",
				ServiceURL: "istio-local-gateway.istio-system.svc.cluster.local",
			}},
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"external-gateways": replaceTabs(`
				- namespace: "namespace1"
				  name: "gateway1"
				  service:  "istio-ingressbackroad.istio-system.svc.cluster.local"`),
				"local-gateways": replaceTabs(`
				- namespace: "namespace2"
				  name: "gateway2"
				  service:  "istio-local-gateway.istio-system.svc.cluster.local"`),
			},
		},
	}, {
		name:    "new & old format",
		wantErr: true,
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"external-gateways": replaceTabs(`
				- namespace: "namespace1"
				  name: "gateway1"
				  service:  "istio-ingressbackroad.istio-system.svc.cluster.local"`),
				"local-gateways": replaceTabs(`
				- namespace: "namespace2"
				  name: "gateway2"
				  service:  "istio-local-gateway.istio-system.svc.cluster.local"`),
				"local-gateway.custom-namespace.custom-local-gateway": "istio-ingressbackroad.istio-system.svc.cluster.local",
				"gateway.custom-namespace.custom-gateway":             "istio-ingressfreeway.istio-system.svc.cluster.local",
			},
		},
	}, {
		name:    "new format - invalid URL",
		wantErr: true,
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"external-gateways": replaceTabs(`
				- namespace: "namespace1"
				  name: "gateway1"
				  service:  "istio-ingressbackroad.istio-system.svc.cluster.local"`),
				"local-gateways": replaceTabs(`
				- namespace: "namespace2"
				  name: "gateway2"
				  service:  "_invalid"`),
			},
		},
	}, {
		name:    "new format - missing service",
		wantErr: true,
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"external-gateways": replaceTabs(`
				- namespace: "namespace1"
				  name: "gateway1"
				  service:  ""`),
				"local-gateways": replaceTabs(`
				- namespace: "namespace2"
				  name: "gateway2"
				  service:  "istio-local-gateway.istio-system.svc.cluster.local"`),
			},
		},
	}, {
		name:    "new format - missing name",
		wantErr: true,
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"external-gateways": replaceTabs(`
				- namespace: "namespace1"
				  name: ""
				  service:  "istio-ingressbackroad.istio-system.svc.cluster.local"`),
				"local-gateways": replaceTabs(`
				- namespace: "namespace2"
				  name: "gateway2"
				  service:  "istio-local-gateway.istio-system.svc.cluster.local"`),
			},
		},
	}, {
		name:    "new format - missing namespace",
		wantErr: true,
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"external-gateways": replaceTabs(`
				- namespace: "namespace1"
				  name: "gateway1"
				  service:  "istio-ingressbackroad.istio-system.svc.cluster.local"`),
				"local-gateways": replaceTabs(`
				- namespace: ""
				  name: "gateway2"
				  service:  "istio-local-gateway.istio-system.svc.cluster.local"`),
			},
		},
	}, {
		name:    "new format - invalid local yaml",
		wantErr: true,
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"external-gateways": replaceTabs(`
				- namespace: "namespace1"
				  name: "gateway1"
				  service:  "istio-ingressbackroad.istio-system.svc.cluster.local"`),
				"local-gateways": "notYAML",
			},
		},
	}, {
		name:    "new format - invalid external yaml",
		wantErr: true,
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"external-gateways": "notYAML",
				"local-gateways": replaceTabs(`
				- namespace: "namespace2"
				  name: "gateway2"
				  service:  "istio-local-gateway.istio-system.svc.cluster.local"`),
			},
		},
	}, {
		name: "new format - missing external gateway configuration",
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"local-gateways": replaceTabs(`
				- namespace: "namespace2"
				  name: "gateway2"
				  service:  "istio-local-gateway.istio-system.svc.cluster.local"`),
			},
		},
		wantIstio: &Istio{
			LocalGateways: []Gateway{{
				Namespace:  "namespace2",
				Name:       "gateway2",
				ServiceURL: "istio-local-gateway.istio-system.svc.cluster.local",
			}},
			IngressGateways: defaultIngressGateways(),
		},
	}, {
		name: "new format - missing local gateway configuration",
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"external-gateways": replaceTabs(`
				- namespace: "namespace1"
				  name: "gateway1"
				  service:  "istio-ingressbackroad.istio-system.svc.cluster.local"`),
			},
		},
		wantIstio: &Istio{
			IngressGateways: []Gateway{{
				Namespace:  "namespace1",
				Name:       "gateway1",
				ServiceURL: "istio-ingressbackroad.istio-system.svc.cluster.local",
			}},
			LocalGateways: defaultLocalGateways(),
		},
	}, {
		name: "new format - missing default gateway",
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"external-gateways": replaceTabs(`
				- namespace: "namespace"
				  name: "gateway"
				  service: "istio-gateway.istio-system.svc.cluster.local"
				  labelSelector:
					matchExpressions:
					- key: "key"
					  operator: "In"
					  values: ["value"]`),
			},
		},
		wantErr: true,
	}, {
		name: "new format - missing default local gateway",
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"local-gateways": replaceTabs(`
				- namespace: "namespace"
				  name: "gateway"
				  service: "istio-gateway.istio-system.svc.cluster.local"
				  labelSelector:
					matchExpressions:
					- key: "key"
					  operator: "In"
					  values: ["value"]`),
			},
		},
		wantErr: true,
	}, {
		name: "new format - label selector",
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"external-gateways": replaceTabs(`
				- namespace: "unused"
				  name: "unused"
				  service: "default.default.svc.cluster.local"
				- namespace: "namespace"
				  name: "gateway"
				  service: "istio-gateway.istio-system.svc.cluster.local"
				  labelSelector:
					matchExpressions:
					- key: "key"
					  operator: "In"
					  values: ["value"]`),

				"local-gateways": replaceTabs(`
				- namespace: "namespace2"
				  name: "gateway2"
				  service: "istio-local-gateway.istio-system.svc.cluster.local"`),
			},
		},
		wantIstio: &Istio{
			IngressGateways: []Gateway{
				{
					Namespace:  "unused",
					Name:       "unused",
					ServiceURL: "default.default.svc.cluster.local",
				},
				{
					Namespace:  "namespace",
					Name:       "gateway",
					ServiceURL: "istio-gateway.istio-system.svc.cluster.local",
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "key",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"value"},
							},
						},
					},
				},
			},
			LocalGateways: []Gateway{{
				Namespace:  "namespace2",
				Name:       "gateway2",
				ServiceURL: "istio-local-gateway.istio-system.svc.cluster.local",
			}},
		},
	}, {
		name: "local gateway with selector",
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"local-gateways": replaceTabs(`
				- namespace: "namespace"
				  name: "gateway"
				  service: "istio-local.istio-system.svc.cluster.local"
				  labelSelector:
					matchExpressions:
					- key: "key"
					  operator: "In"
					  values: ["value"]
				- namespace: "namespace1"
				  name: "gateway1"
				  service: "istio-local-gateway.istio-system.svc.cluster.local"`),

				"external-gateways": replaceTabs(`
				- namespace: "namespace2"
				  name: "gateway2"
				  service: "istio-gateway.istio-system.svc.cluster.local"`),
			},
		},
		wantIstio: &Istio{
			IngressGateways: []Gateway{{
				Namespace:  "namespace2",
				Name:       "gateway2",
				ServiceURL: "istio-gateway.istio-system.svc.cluster.local",
			}},
			LocalGateways: []Gateway{
				{
					Namespace:  "namespace",
					Name:       "gateway",
					ServiceURL: "istio-local.istio-system.svc.cluster.local",
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "key",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"value"},
							},
						},
					},
				},
				{
					Namespace:  "namespace1",
					Name:       "gateway1",
					ServiceURL: "istio-local-gateway.istio-system.svc.cluster.local",
				},
			},
		},
	}, {
		name: "new format - invalid label selector",
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"external-gateways": replaceTabs(`
				- namespace: "default"
				  name: "default"
				  service: "default.default.svc.cluster.local"
				- namespace: "namespace"
				  name: "gateway"
				  service: "istio-gateway.istio-system.svc.cluster.local"
				  labelSelector:
					matchExpressions:
					- key: "key"
					  operator: "In"`),
			},
		},
		wantErr: true,
	}}

	for _, tt := range gatewayConfigTests {
		t.Run(tt.name, func(t *testing.T) {
			actualIstio, err := NewIstioFromConfigMap(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Test: %q; NewIstioFromConfigMap() error = %v, WantErr %v", tt.name, err, tt.wantErr)
			}

			if diff := cmp.Diff(actualIstio, tt.wantIstio); diff != "" {
				t.Fatalf("Want %+v, but got %+v", tt.wantIstio, actualIstio)
			}
		})
	}
}

func replaceTabs(s string) string {
	return strings.ReplaceAll(s, "\t", "    ")
}
