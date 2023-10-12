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
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
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
		wantIstio interface{}
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
		name:      "gateway configuration with invalid url",
		wantErr:   true,
		wantIstio: (*Istio)(nil),
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
		name:    "gateway configuration with valid url",
		wantErr: false,
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
		name:    "gateway configuration with valid url having a dot at the end",
		wantErr: false,
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
		name:    "gateway configuration in custom namespace with valid url",
		wantErr: false,
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
		name:      "gateway configuration in custom namespace with invalid url",
		wantErr:   true,
		wantIstio: (*Istio)(nil),
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
		name:    "local gateway configuration with valid url",
		wantErr: false,
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
		name:      "local gateway configuration with invalid url",
		wantErr:   true,
		wantIstio: (*Istio)(nil),
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
		name:    "local gateway configuration in custom namespace with valid url",
		wantErr: false,
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
		name:      "local gateway configuration in custom namespace with invalid url",
		wantErr:   true,
		wantIstio: (*Istio)(nil),
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
		name:    "local gateway configuration in custom namespace with valid url with exposition",
		wantErr: false,
		wantIstio: &Istio{
			IngressGateways: defaultIngressGateways(),
			LocalGateways: []Gateway{
				{
					Namespace:  "custom-namespace-0",
					Name:       "custom-local-gateway-0",
					ServiceURL: "istio-ingressbackroad.istio-system.svc.cluster.local",
				},
				{
					Namespace:   "custom-namespace-1",
					Name:        "custom-local-gateway-1",
					ServiceURL:  "istio-ingressbackroad.istio-system.svc.cluster.local",
					Expositions: sets.New[string]("some-value-1"),
				},
				{
					Namespace:   "custom-namespace-2",
					Name:        "custom-local-gateway-2",
					ServiceURL:  "istio-ingressbackroad.istio-system.svc.cluster.local",
					Expositions: sets.New[string]("some-value-2a", "some-value-2b"),
				}},
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"local-gateway.custom-namespace-0.custom-local-gateway-0": "istio-ingressbackroad.istio-system.svc.cluster.local",
				"local-gateway.custom-namespace-1.custom-local-gateway-1": "istio-ingressbackroad.istio-system.svc.cluster.local",
				"local-gateway.custom-namespace-2.custom-local-gateway-2": "istio-ingressbackroad.istio-system.svc.cluster.local",
				"exposition.custom-namespace-1.custom-local-gateway-1":    "some-value-1",
				"exposition.custom-namespace-2.custom-local-gateway-2":    "some-value-2a,some-value-2b",
			},
		},
	}, {
		name:    "gateway configuration defined with/without namespace with exposition defined with/without namespace",
		wantErr: false,
		wantIstio: &Istio{
			IngressGateways: []Gateway{
				{
					Namespace:   "knative-testing",
					Name:        "custom-gateway-wo-wi",
					ServiceURL:  "istio-ingressfreeway.istio-system.svc.cluster.local",
					Expositions: sets.New[string]("some-value-wo-wi"),
				},
				{
					Namespace:   "knative-testing",
					Name:        "custom-gateway-wo-wo",
					ServiceURL:  "istio-ingressfreeway.istio-system.svc.cluster.local",
					Expositions: sets.New[string]("some-value-wo-wo"),
				},
				{
					Namespace:   "knative-testing",
					Name:        "custom-gateway-wi-wi",
					ServiceURL:  "istio-ingressfreeway.istio-system.svc.cluster.local",
					Expositions: sets.New[string]("some-value-wi-wi"),
				},
				{
					Namespace:   "knative-testing",
					Name:        "custom-gateway-wi-wo",
					ServiceURL:  "istio-ingressfreeway.istio-system.svc.cluster.local",
					Expositions: sets.New[string]("some-value-wi-wo"),
				}},
			LocalGateways: defaultLocalGateways(),
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"gateway.custom-gateway-wo-wo":                    "istio-ingressfreeway.istio-system.svc.cluster.local",
				"gateway.custom-gateway-wo-wi":                    "istio-ingressfreeway.istio-system.svc.cluster.local",
				"gateway.knative-testing.custom-gateway-wi-wo":    "istio-ingressfreeway.istio-system.svc.cluster.local",
				"gateway.knative-testing.custom-gateway-wi-wi":    "istio-ingressfreeway.istio-system.svc.cluster.local",
				"exposition.custom-gateway-wo-wo":                 "some-value-wo-wo",
				"exposition.knative-testing.custom-gateway-wo-wi": "some-value-wo-wi",
				"exposition.custom-gateway-wi-wo":                 "some-value-wi-wo",
				"exposition.knative-testing.custom-gateway-wi-wi": "some-value-wi-wi",
			},
		},
	}, {
		name:    "Same exposition defined for local and non-local gateways",
		wantErr: false,
		wantIstio: &Istio{
			IngressGateways: []Gateway{
				{
					Namespace:  "knative-testing",
					Name:       "custom-gateway-no-exposition",
					ServiceURL: "istio-ingress1.istio-system.svc.cluster.local",
				},
				{
					Namespace:   "knative-testing",
					Name:        "custom-gateway-with-exposition",
					ServiceURL:  "istio-ingress2.istio-system.svc.cluster.local",
					Expositions: sets.New[string]("some-value"),
				},
			},
			LocalGateways: []Gateway{
				{
					Namespace:  "knative-testing",
					Name:       "custom-gateway-no-exposition",
					ServiceURL: "istio-ingress1.istio-system.svc.cluster.local",
				},
				{
					Namespace:   "knative-testing",
					Name:        "custom-gateway-with-exposition",
					ServiceURL:  "istio-ingress2.istio-system.svc.cluster.local",
					Expositions: sets.New[string]("some-value"),
				},
			},
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"gateway.custom-gateway-no-exposition":         "istio-ingress1.istio-system.svc.cluster.local",
				"gateway.custom-gateway-with-exposition":       "istio-ingress2.istio-system.svc.cluster.local",
				"local-gateway.custom-gateway-no-exposition":   "istio-ingress1.istio-system.svc.cluster.local",
				"local-gateway.custom-gateway-with-exposition": "istio-ingress2.istio-system.svc.cluster.local",
				"exposition.custom-gateway-with-exposition":    "some-value",
			},
		},
	}, {
		name:    "Exposition defined but referencing no known gateways",
		wantErr: false,
		wantIstio: &Istio{
			IngressGateways: []Gateway{
				{
					Namespace:  "knative-testing",
					Name:       "custom-gateway",
					ServiceURL: "istio-ingress.istio-system.svc.cluster.local",
				},
			},
			LocalGateways: []Gateway{
				{
					Namespace:  "knative-testing",
					Name:       "custom-local-gateway",
					ServiceURL: "istio-ingress.istio-system.svc.cluster.local",
				},
			},
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      IstioConfigName,
			},
			Data: map[string]string{
				"gateway.custom-gateway":             "istio-ingress.istio-system.svc.cluster.local",
				"local-gateway.custom-local-gateway": "istio-ingress.istio-system.svc.cluster.local",
				"exposition.unknown-value":           "some-value",
			},
		},
	}}

	for _, tt := range gatewayConfigTests {
		t.Run(tt.name, func(t *testing.T) {
			actualIstio, err := NewIstioFromConfigMap(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Test: %q; NewIstioFromConfigMap() error = %v, WantErr %v", tt.name, err, tt.wantErr)
			}

			if diff := cmp.Diff(actualIstio, tt.wantIstio); diff != "" {
				t.Fatalf("Want %v, but got %v", tt.wantIstio, actualIstio)
			}
		})
	}
}
