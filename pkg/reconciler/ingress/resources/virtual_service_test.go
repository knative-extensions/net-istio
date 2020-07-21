/*
Copyright 2019 The Knative Authors.

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
	"context"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/google/go-cmp/cmp"
	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/net-istio/pkg/reconciler/ingress/config"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
	apiconfig "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/network/ingress"
)

var (
	defaultMaxRevisionTimeout = time.Duration(apiconfig.DefaultMaxRevisionTimeoutSeconds) * time.Second
	defaultGateways           = makeGatewayMap([]string{"gateway"}, []string{"private-gateway"})
	defaultIngressRuleValue   = &v1alpha1.HTTPIngressRuleValue{
		Paths: []v1alpha1.HTTPIngressPath{
			{
				Timeout: &metav1.Duration{time.Minute},
				Splits: []v1alpha1.IngressBackendSplit{
					{
						Percent: 100,
						IngressBackend: v1alpha1.IngressBackend{
							ServiceNamespace: "test",
							ServiceName:      "test.svc.cluster.local",
							ServicePort:      intstr.FromInt(8080),
						},
					},
				},
				Retries: &v1alpha1.HTTPRetry{
					Attempts:      3,
					PerTryTimeout: &metav1.Duration{time.Minute},
				},
			},
		},
	}
	defaultIngress = v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingress",
			Namespace: system.Namespace(),
		},
		Spec: v1alpha1.IngressSpec{Rules: []v1alpha1.IngressRule{{
			Hosts: []string{
				"test-route.test-ns.svc.cluster.local",
			},
			HTTP: defaultIngressRuleValue,
		}}},
	}
)

func TestMakeVirtualServices_CorrectMetadata(t *testing.T) {
	for _, tc := range []struct {
		name     string
		gateways map[v1alpha1.IngressVisibility]sets.String
		ci       *v1alpha1.Ingress
		expected []metav1.ObjectMeta
	}{{
		name:     "mesh and ingress",
		gateways: makeGatewayMap([]string{"gateway"}, []string{"private-gateway"}),
		ci: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: system.Namespace(),
				Labels: map[string]string{
					networking.IngressLabelKey:     "test-ingress",
					serving.RouteLabelKey:          "test-route",
					serving.RouteNamespaceLabelKey: "test-ns",
				},
			},
			Spec: v1alpha1.IngressSpec{Rules: []v1alpha1.IngressRule{{
				Hosts: []string{
					"test-route.test-ns.svc.cluster.local",
				},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
				HTTP:       &v1alpha1.HTTPIngressRuleValue{},
			}}},
		},
		expected: []metav1.ObjectMeta{{
			Name:      "test-mesh",
			Namespace: system.Namespace(),
			Labels: map[string]string{
				networking.IngressLabelKey:     "test",
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
		}, {
			Name:      "test-ingress",
			Namespace: system.Namespace(),
			Labels: map[string]string{
				networking.IngressLabelKey:     "test",
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
		}},
	}, {
		name:     "ingress only",
		gateways: makeGatewayMap([]string{"gateway"}, []string{"private-gateway"}),
		ci: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: system.Namespace(),
				Labels: map[string]string{
					serving.RouteLabelKey:          "test-route",
					serving.RouteNamespaceLabelKey: "test-ns",
				},
			},
			Spec: v1alpha1.IngressSpec{Rules: []v1alpha1.IngressRule{{
				Hosts: []string{
					"test-route.test-ns.example.com",
				},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
				HTTP:       &v1alpha1.HTTPIngressRuleValue{},
			}}},
		},
		expected: []metav1.ObjectMeta{{
			Name:      "test-ingress",
			Namespace: system.Namespace(),
			Labels: map[string]string{
				networking.IngressLabelKey:     "test",
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
		}},
	}, {
		name:     "mesh only",
		gateways: nil,
		ci: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingress",
				Namespace: system.Namespace(),
				Labels: map[string]string{
					serving.RouteLabelKey:          "test-route",
					serving.RouteNamespaceLabelKey: "test-ns",
				},
			},
			Spec: v1alpha1.IngressSpec{Rules: []v1alpha1.IngressRule{{
				Hosts: []string{
					"test-route.test-ns.svc.cluster.local",
				},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
				HTTP:       &v1alpha1.HTTPIngressRuleValue{},
			}}},
		},
		expected: []metav1.ObjectMeta{{
			Name:      "test-ingress-mesh",
			Namespace: system.Namespace(),
			Labels: map[string]string{
				networking.IngressLabelKey:     "test-ingress",
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
		}},
	}, {
		name:     "mesh only with namespace",
		gateways: nil,
		ci: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingress",
				Namespace: "test-ns",
				Labels: map[string]string{
					serving.RouteLabelKey:          "test-route",
					serving.RouteNamespaceLabelKey: "test-ns",
				},
			},
			Spec: v1alpha1.IngressSpec{Rules: []v1alpha1.IngressRule{{
				Hosts: []string{
					"test-route.test-ns.svc.cluster.local",
				},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
				HTTP:       &v1alpha1.HTTPIngressRuleValue{},
			}}},
		},
		expected: []metav1.ObjectMeta{{
			Name:      "test-ingress-mesh",
			Namespace: "test-ns",
			Labels: map[string]string{
				networking.IngressLabelKey:     "test-ingress",
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
		}},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			vss, err := MakeVirtualServices(context.Background(), tc.ci, tc.gateways)
			if err != nil {
				t.Fatalf("MakeVirtualServices failed: %v", err)
			}
			if len(vss) != len(tc.expected) {
				t.Fatalf("Expected %d VirtualService, saw %d", len(tc.expected), len(vss))
			}
			for i := range tc.expected {
				tc.expected[i].OwnerReferences = []metav1.OwnerReference{*kmeta.NewControllerRef(tc.ci)}
				if diff := cmp.Diff(tc.expected[i], vss[i].ObjectMeta); diff != "" {
					t.Errorf("Unexpected metadata (-want +got): %v", diff)
				}
			}
		})
	}
}

func TestMakeVirtualServicesSpec_CorrectGateways(t *testing.T) {

	tests := []struct {
		name             string
		ingress          *v1alpha1.Ingress
		gateways         map[v1alpha1.IngressVisibility]sets.String
		expectedGateways sets.String
	}{
		{
			name: "local visibility",
			ingress: &v1alpha1.Ingress{
				Spec: v1alpha1.IngressSpec{
					Rules: []v1alpha1.IngressRule{
						{
							Hosts:      []string{"test.svc.cluster.local"},
							Visibility: v1alpha1.IngressVisibilityClusterLocal,
							HTTP:       defaultIngressRuleValue,
						},
					},
					Visibility: v1alpha1.IngressVisibilityClusterLocal,
				},
			},
			gateways: map[v1alpha1.IngressVisibility]sets.String{
				v1alpha1.IngressVisibilityClusterLocal: sets.NewString("cluster-local-gateway/knative-serving"),
				v1alpha1.IngressVisibilityExternalIP:   sets.NewString("knative-ingress-gateway/knative-serving"),
			},
			expectedGateways: sets.NewString("cluster-local-gateway/knative-serving"),
		},
		{
			name: "public visibility",
			ingress: &v1alpha1.Ingress{
				Spec: v1alpha1.IngressSpec{
					Rules: []v1alpha1.IngressRule{
						{
							Hosts:      []string{"test.example.com"},
							Visibility: v1alpha1.IngressVisibilityExternalIP,
							HTTP:       defaultIngressRuleValue,
						},
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				},
			},
			gateways: map[v1alpha1.IngressVisibility]sets.String{
				v1alpha1.IngressVisibilityClusterLocal: sets.NewString("cluster-local-gateway/knative-serving"),
				v1alpha1.IngressVisibilityExternalIP:   sets.NewString("knative-ingress-gateway/knative-serving"),
			},
			expectedGateways: sets.NewString("knative-ingress-gateway/knative-serving"),
		},
		{
			name: "local and public visibility",
			ingress: &v1alpha1.Ingress{
				Spec: v1alpha1.IngressSpec{
					Rules: []v1alpha1.IngressRule{
						{
							Hosts:      []string{"test.example.com"},
							Visibility: v1alpha1.IngressVisibilityExternalIP,
							HTTP:       defaultIngressRuleValue,
						},
						{
							Hosts:      []string{"test.svc.cluster.local"},
							Visibility: v1alpha1.IngressVisibilityClusterLocal,
							HTTP:       defaultIngressRuleValue,
						},
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				},
			},
			gateways: map[v1alpha1.IngressVisibility]sets.String{
				v1alpha1.IngressVisibilityClusterLocal: sets.NewString("cluster-local-gateway/knative-serving"),
				v1alpha1.IngressVisibilityExternalIP:   sets.NewString("knative-ingress-gateway/knative-serving"),
			},
			expectedGateways: sets.NewString("knative-ingress-gateway/knative-serving",
				"cluster-local-gateway/knative-serving"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vs := makeVirtualServiceSpec(context.Background(), tc.ingress, tc.gateways, ingress.ExpandedHosts(getHosts(tc.ingress)))
			actualGateways := sets.NewString(vs.Gateways...)
			if !actualGateways.Equal(tc.expectedGateways) {
				t.Fatalf("Got gateways %v, expected %v", actualGateways.List(), tc.expectedGateways.List())
			}
		})
	}
}

func TestMakeMeshVirtualServiceSpec_CorrectGateways(t *testing.T) {
	ci := &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingress",
			Namespace: system.Namespace(),
			Labels: map[string]string{
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
		},
		Spec: v1alpha1.IngressSpec{
			Rules: []v1alpha1.IngressRule{{
				Hosts: []string{
					"test-route.test-ns.svc.cluster.local",
				},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
				HTTP:       defaultIngressRuleValue,
			}}},
	}
	expected := []string{"mesh"}
	gateways := MakeMeshVirtualService(context.Background(), ci, defaultGateways).Spec.Gateways
	if diff := cmp.Diff(expected, gateways); diff != "" {
		t.Errorf("Unexpected gateways (-want +got): %v", diff)
	}
}

func TestMakeMeshVirtualServiceSpecCorrectHosts(t *testing.T) {
	for _, tc := range []struct {
		name          string
		gateways      map[v1alpha1.IngressVisibility]sets.String
		expectedHosts sets.String
	}{{
		name: "with cluster local gateway: expanding hosts",
		gateways: map[v1alpha1.IngressVisibility]sets.String{
			v1alpha1.IngressVisibilityClusterLocal: sets.NewString("cluster-local"),
		},
		expectedHosts: sets.NewString(
			"test-route.test-ns.svc.cluster.local",
			"test-route.test-ns.svc",
			"test-route.test-ns",
		),
	}, {
		name:          "with mesh: no exapnding hosts",
		gateways:      map[v1alpha1.IngressVisibility]sets.String{},
		expectedHosts: sets.NewString("test-route.test-ns.svc.cluster.local"),
	}} {
		t.Run(tc.name, func(t *testing.T) {
			vs := MakeMeshVirtualService(context.Background(), &defaultIngress, tc.gateways)
			vsHosts := sets.NewString(vs.Spec.Hosts...)
			if !vsHosts.Equal(tc.expectedHosts) {
				t.Errorf("Unexpected hosts want %v; got %v", tc.expectedHosts, vsHosts)
			}
		})
	}

}

func TestMakeMeshVirtualServiceSpec_CorrectRetries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ci       *v1alpha1.Ingress
		expected *istiov1alpha3.HTTPRetry
	}{{
		name: "default retries",
		ci: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingress",
				Namespace: system.Namespace(),
				Labels: map[string]string{
					serving.RouteLabelKey:          "test-route",
					serving.RouteNamespaceLabelKey: "test-ns",
				},
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"test-route.test-ns.svc.cluster.local",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
					HTTP: &v1alpha1.HTTPIngressRuleValue{
						Paths: []v1alpha1.HTTPIngressPath{{
							Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
							Retries: &v1alpha1.HTTPRetry{
								PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
								Attempts:      networking.DefaultRetryCount,
							},
						}},
					},
				}}},
		},
		expected: &istiov1alpha3.HTTPRetry{
			RetryOn:       retriableConditions,
			Attempts:      int32(networking.DefaultRetryCount),
			PerTryTimeout: types.DurationProto(defaultMaxRevisionTimeout),
		},
	}, {
		name: "no per-try timeout",
		ci: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingress",
				Namespace: system.Namespace(),
				Labels: map[string]string{
					serving.RouteLabelKey:          "test-route",
					serving.RouteNamespaceLabelKey: "test-ns",
				},
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"test-route.test-ns.svc.cluster.local",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
					HTTP: &v1alpha1.HTTPIngressRuleValue{
						Paths: []v1alpha1.HTTPIngressPath{{
							Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
							Retries: &v1alpha1.HTTPRetry{
								Attempts: networking.DefaultRetryCount,
							},
						}},
					},
				}}},
		},
		expected: &istiov1alpha3.HTTPRetry{
			RetryOn:  retriableConditions,
			Attempts: int32(networking.DefaultRetryCount),
		},
	}, {
		name: "retry attempt=0",
		ci: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingress",
				Namespace: system.Namespace(),
				Labels: map[string]string{
					serving.RouteLabelKey:          "test-route",
					serving.RouteNamespaceLabelKey: "test-ns",
				},
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"test-route.test-ns.svc.cluster.local",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
					HTTP: &v1alpha1.HTTPIngressRuleValue{
						Paths: []v1alpha1.HTTPIngressPath{{
							Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
							Retries: &v1alpha1.HTTPRetry{
								PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
								Attempts:      0,
							},
						}},
					},
				}}},
		},
		expected: &istiov1alpha3.HTTPRetry{},
	}, {
		name: "disabling retries",
		ci: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ingress",
				Namespace: system.Namespace(),
				Labels: map[string]string{
					serving.RouteLabelKey:          "test-route",
					serving.RouteNamespaceLabelKey: "test-ns",
				},
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"test-route.test-ns.svc.cluster.local",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
					HTTP: &v1alpha1.HTTPIngressRuleValue{
						Paths: []v1alpha1.HTTPIngressPath{{
							Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
						}},
					},
				}}},
		},
		expected: &istiov1alpha3.HTTPRetry{},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			for _, h := range MakeMeshVirtualService(context.Background(), tc.ci, defaultGateways).Spec.Http {
				if diff := cmp.Diff(tc.expected, h.Retries); diff != "" {
					t.Errorf("Unexpected retries (-want +got): %v", diff)
				}
			}
		})
	}
}

func TestMakeMeshVirtualServiceSpec_CorrectRoutes(t *testing.T) {
	ci := &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingress",
			Namespace: system.Namespace(),
		},
		Spec: v1alpha1.IngressSpec{
			Rules: []v1alpha1.IngressRule{{
				Hosts: []string{
					"test-route.test-ns.svc.cluster.local",
				},
				HTTP: &v1alpha1.HTTPIngressRuleValue{
					Paths: []v1alpha1.HTTPIngressPath{{
						Path: "^/pets/(.*?)?",
						Splits: []v1alpha1.IngressBackendSplit{{
							IngressBackend: v1alpha1.IngressBackend{
								ServiceNamespace: "test-ns",
								ServiceName:      "v2-service",
								ServicePort:      intstr.FromInt(80),
							},
							Percent: 100,
							AppendHeaders: map[string]string{
								"ugh": "blah",
							},
						}},
						AppendHeaders: map[string]string{
							"foo": "bar",
						},
						Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
						Retries: &v1alpha1.HTTPRetry{
							PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
							Attempts:      networking.DefaultRetryCount,
						},
					}},
				},
			}, {
				Hosts: []string{
					"v1.domain.com",
				},
				HTTP: &v1alpha1.HTTPIngressRuleValue{
					Paths: []v1alpha1.HTTPIngressPath{{
						Path: "^/pets/(.*?)?",
						Splits: []v1alpha1.IngressBackendSplit{{
							IngressBackend: v1alpha1.IngressBackend{
								ServiceNamespace: "test-ns",
								ServiceName:      "v1-service",
								ServicePort:      intstr.FromInt(80),
							},
							Percent: 100,
						}},
						AppendHeaders: map[string]string{
							"foo": "baz",
						},
						Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
						Retries: &v1alpha1.HTTPRetry{
							PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
							Attempts:      networking.DefaultRetryCount,
						},
					}},
				},
			}},
		},
	}
	expected := []*istiov1alpha3.HTTPRoute{{
		Match: []*istiov1alpha3.HTTPMatchRequest{{
			Uri: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Regex{Regex: "^/pets/(.*?)?"},
			},
			Authority: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Prefix{Prefix: `test-route.test-ns`},
			},
			Gateways: []string{"mesh"},
		}},
		Route: []*istiov1alpha3.HTTPRouteDestination{{
			Destination: &istiov1alpha3.Destination{
				Host: "v2-service.test-ns.svc.cluster.local",
				Port: &istiov1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
			Headers: &istiov1alpha3.Headers{
				Request: &istiov1alpha3.Headers_HeaderOperations{
					Set: map[string]string{
						"ugh": "blah",
					},
				},
			},
		}},
		Headers: &istiov1alpha3.Headers{
			Request: &istiov1alpha3.Headers_HeaderOperations{
				Set: map[string]string{
					"foo": "bar",
				},
			},
		},
		Timeout: types.DurationProto(defaultMaxRevisionTimeout),
		Retries: &istiov1alpha3.HTTPRetry{
			RetryOn:       retriableConditions,
			Attempts:      int32(networking.DefaultRetryCount),
			PerTryTimeout: types.DurationProto(defaultMaxRevisionTimeout),
		},
	}}

	routes := MakeMeshVirtualService(context.Background(), ci, defaultGateways).Spec.Http
	if diff := cmp.Diff(expected, routes); diff != "" {
		t.Errorf("Unexpected routes (-want +got): %v", diff)
	}
}

func TestMakeIngressVirtualServiceSpec_CorrectGateways(t *testing.T) {
	ci := defaultIngress.DeepCopy()
	// We add public gateways, so make sure that the rules have ExternalIP visibility.
	for idx := range ci.Spec.Rules {
		ci.Spec.Rules[idx].Visibility = v1alpha1.IngressVisibilityExternalIP
	}
	expected := []string{"knative-testing/gateway-one", "knative-testing/gateway-two"}
	gateways := MakeIngressVirtualService(context.Background(), ci, makeGatewayMap([]string{"knative-testing/gateway-one", "knative-testing/gateway-two"}, nil)).Spec.Gateways
	if diff := cmp.Diff(expected, gateways); diff != "" {
		t.Errorf("Unexpected gateways (-want +got): %v", diff)
	}
}

func TestMakeIngressVirtualServiceSpec_CorrectRoutes(t *testing.T) {
	ci := &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingress",
			Namespace: system.Namespace(),
		},
		Spec: v1alpha1.IngressSpec{
			Rules: []v1alpha1.IngressRule{{
				Hosts: []string{
					"domain.com",
					"test-route.test-ns.svc.cluster.local",
				},
				HTTP: &v1alpha1.HTTPIngressRuleValue{
					Paths: []v1alpha1.HTTPIngressPath{{
						Path: "^/pets/(.*?)?",
						Splits: []v1alpha1.IngressBackendSplit{{
							IngressBackend: v1alpha1.IngressBackend{
								ServiceNamespace: "test-ns",
								ServiceName:      "v2-service",
								ServicePort:      intstr.FromInt(80),
							},
							Percent: 100,
							AppendHeaders: map[string]string{
								"ugh": "blah",
							},
						}},
						AppendHeaders: map[string]string{
							"foo": "bar",
						},
						Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
						Retries: &v1alpha1.HTTPRetry{
							PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
							Attempts:      networking.DefaultRetryCount,
						},
					}},
				},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
			}, {
				Hosts: []string{
					"v1.domain.com",
				},
				HTTP: &v1alpha1.HTTPIngressRuleValue{
					Paths: []v1alpha1.HTTPIngressPath{{
						Path: "^/pets/(.*?)?",
						Splits: []v1alpha1.IngressBackendSplit{{
							IngressBackend: v1alpha1.IngressBackend{
								ServiceNamespace: "test-ns",
								ServiceName:      "v1-service",
								ServicePort:      intstr.FromInt(80),
							},
							Percent: 100,
						}},
						AppendHeaders: map[string]string{
							"foo": "baz",
						},
						Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
						Retries: &v1alpha1.HTTPRetry{
							PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
							Attempts:      networking.DefaultRetryCount,
						},
					}},
				},
			}},
		},
	}

	expected := []*istiov1alpha3.HTTPRoute{{
		Match: []*istiov1alpha3.HTTPMatchRequest{{
			Uri: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Regex{Regex: "^/pets/(.*?)?"},
			},
			Authority: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Prefix{Prefix: `domain.com`},
			},
			Gateways: []string{"gateway.public"},
		}, {
			Uri: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Regex{Regex: "^/pets/(.*?)?"},
			},
			Authority: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Prefix{Prefix: `test-route.test-ns`},
			},
			Gateways: []string{"gateway.private"},
		}},
		Route: []*istiov1alpha3.HTTPRouteDestination{{
			Destination: &istiov1alpha3.Destination{
				Host: "v2-service.test-ns.svc.cluster.local",
				Port: &istiov1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
			Headers: &istiov1alpha3.Headers{
				Request: &istiov1alpha3.Headers_HeaderOperations{
					Set: map[string]string{
						"ugh": "blah",
					},
				},
			},
		}},
		Headers: &istiov1alpha3.Headers{
			Request: &istiov1alpha3.Headers_HeaderOperations{
				Set: map[string]string{
					"foo": "bar",
				},
			},
		},
		Timeout: types.DurationProto(defaultMaxRevisionTimeout),
		Retries: &istiov1alpha3.HTTPRetry{
			RetryOn:       retriableConditions,
			Attempts:      int32(networking.DefaultRetryCount),
			PerTryTimeout: types.DurationProto(defaultMaxRevisionTimeout),
		},
	}, {
		Match: []*istiov1alpha3.HTTPMatchRequest{{
			Uri: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Regex{Regex: "^/pets/(.*?)?"},
			},
			Authority: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Prefix{Prefix: `v1.domain.com`},
			},
			Gateways: []string{},
		}},
		Route: []*istiov1alpha3.HTTPRouteDestination{{
			Destination: &istiov1alpha3.Destination{
				Host: "v1-service.test-ns.svc.cluster.local",
				Port: &istiov1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
		Headers: &istiov1alpha3.Headers{
			Request: &istiov1alpha3.Headers_HeaderOperations{
				Set: map[string]string{
					"foo": "baz",
				},
			},
		},
		Timeout: types.DurationProto(defaultMaxRevisionTimeout),
		Retries: &istiov1alpha3.HTTPRetry{
			RetryOn:       retriableConditions,
			Attempts:      int32(networking.DefaultRetryCount),
			PerTryTimeout: types.DurationProto(defaultMaxRevisionTimeout),
		},
	}}

	routes := MakeIngressVirtualService(context.Background(), ci, makeGatewayMap([]string{"gateway.public"}, []string{"gateway.private"})).Spec.Http
	if diff := cmp.Diff(expected, routes); diff != "" {
		t.Errorf("Unexpected routes (-want +got): %v", diff)
	}
}

func TestMakeVirtualServiceRoute_RewriteHost(t *testing.T) {
	ingressPath := &v1alpha1.HTTPIngressPath{
		RewriteHost: "the.target.host",
		Timeout:     &metav1.Duration{Duration: defaultMaxRevisionTimeout},
		Retries: &v1alpha1.HTTPRetry{
			PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
			Attempts:      networking.DefaultRetryCount,
		},
	}
	ctx := config.ToContext(context.Background(), &config.Config{
		Istio: &config.Istio{
			LocalGateways: []config.Gateway{{
				ServiceURL: "the-local-gateway.svc.url",
			}},
		},
	})
	route := makeVirtualServiceRoute(ctx, sets.NewString("a.vanity.url", "another.vanity.url"), ingressPath, makeGatewayMap([]string{"gateway-1"}, nil), v1alpha1.IngressVisibilityExternalIP)
	expected := &istiov1alpha3.HTTPRoute{
		Match: []*istiov1alpha3.HTTPMatchRequest{{
			Gateways: []string{"gateway-1"},
			Authority: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Prefix{Prefix: `a.vanity.url`},
			},
		}, {
			Gateways: []string{"gateway-1"},
			Authority: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Prefix{Prefix: `another.vanity.url`},
			},
		}},
		Rewrite: &istiov1alpha3.HTTPRewrite{
			Authority: "the.target.host",
		},
		Route: []*istiov1alpha3.HTTPRouteDestination{{
			Destination: &istiov1alpha3.Destination{
				Host: "the-local-gateway.svc.url",
			},
			Weight: 100,
		}},
		Timeout: types.DurationProto(defaultMaxRevisionTimeout),
		Retries: &istiov1alpha3.HTTPRetry{
			RetryOn:       retriableConditions,
			Attempts:      int32(networking.DefaultRetryCount),
			PerTryTimeout: types.DurationProto(defaultMaxRevisionTimeout),
		},
	}
	if diff := cmp.Diff(expected, route); diff != "" {
		t.Errorf("Unexpected route  (-want +got): %v", diff)
	}
}

// One active target.
func TestMakeVirtualServiceRoute_Vanilla(t *testing.T) {
	ingressPath := &v1alpha1.HTTPIngressPath{
		Headers: map[string]v1alpha1.HeaderMatch{
			"my-header": {
				Exact: "my-header-value",
			},
		},
		Splits: []v1alpha1.IngressBackendSplit{{
			IngressBackend: v1alpha1.IngressBackend{

				ServiceNamespace: "test-ns",
				ServiceName:      "revision-service",
				ServicePort:      intstr.FromInt(80),
			},
			Percent: 100,
		}},
		Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
		Retries: &v1alpha1.HTTPRetry{
			PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
			Attempts:      networking.DefaultRetryCount,
		},
	}
	route := makeVirtualServiceRoute(context.Background(), sets.NewString("a.com", "b.org"), ingressPath, makeGatewayMap([]string{"gateway-1"}, nil), v1alpha1.IngressVisibilityExternalIP)
	expected := &istiov1alpha3.HTTPRoute{
		Match: []*istiov1alpha3.HTTPMatchRequest{{
			Gateways: []string{"gateway-1"},
			Authority: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Prefix{Prefix: `a.com`},
			},
			Headers: map[string]*istiov1alpha3.StringMatch{
				"my-header": {
					MatchType: &istiov1alpha3.StringMatch_Exact{
						Exact: "my-header-value",
					},
				},
			},
		}, {
			Gateways: []string{"gateway-1"},
			Authority: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Prefix{Prefix: `b.org`},
			},
			Headers: map[string]*istiov1alpha3.StringMatch{
				"my-header": {
					MatchType: &istiov1alpha3.StringMatch_Exact{
						Exact: "my-header-value",
					},
				},
			},
		}},
		Route: []*istiov1alpha3.HTTPRouteDestination{{
			Destination: &istiov1alpha3.Destination{
				Host: "revision-service.test-ns.svc.cluster.local",
				Port: &istiov1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
		Timeout: types.DurationProto(defaultMaxRevisionTimeout),
		Retries: &istiov1alpha3.HTTPRetry{
			RetryOn:       retriableConditions,
			Attempts:      int32(networking.DefaultRetryCount),
			PerTryTimeout: types.DurationProto(defaultMaxRevisionTimeout),
		},
	}
	if diff := cmp.Diff(expected, route); diff != "" {
		t.Errorf("Unexpected route  (-want +got): %v", diff)
	}
}

// Two active targets.
func TestMakeVirtualServiceRoute_TwoTargets(t *testing.T) {
	ingressPath := &v1alpha1.HTTPIngressPath{
		Splits: []v1alpha1.IngressBackendSplit{{
			IngressBackend: v1alpha1.IngressBackend{
				ServiceNamespace: "test-ns",
				ServiceName:      "revision-service",
				ServicePort:      intstr.FromInt(80),
			},
			Percent: 90,
		}, {
			IngressBackend: v1alpha1.IngressBackend{
				ServiceNamespace: "test-ns",
				ServiceName:      "new-revision-service",
				ServicePort:      intstr.FromInt(81),
			},
			Percent: 10,
		}},
		Timeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
		Retries: &v1alpha1.HTTPRetry{
			PerTryTimeout: &metav1.Duration{Duration: defaultMaxRevisionTimeout},
			Attempts:      networking.DefaultRetryCount,
		},
	}
	route := makeVirtualServiceRoute(context.Background(), sets.NewString("test.org"), ingressPath, makeGatewayMap([]string{"knative-testing/gateway-1"}, nil), v1alpha1.IngressVisibilityExternalIP)
	expected := &istiov1alpha3.HTTPRoute{
		Match: []*istiov1alpha3.HTTPMatchRequest{{
			Gateways: []string{"knative-testing/gateway-1"},
			Authority: &istiov1alpha3.StringMatch{
				MatchType: &istiov1alpha3.StringMatch_Prefix{Prefix: `test.org`},
			},
		}},
		Route: []*istiov1alpha3.HTTPRouteDestination{{
			Destination: &istiov1alpha3.Destination{
				Host: "revision-service.test-ns.svc.cluster.local",
				Port: &istiov1alpha3.PortSelector{Number: 80},
			},
			Weight: 90,
		}, {
			Destination: &istiov1alpha3.Destination{
				Host: "new-revision-service.test-ns.svc.cluster.local",
				Port: &istiov1alpha3.PortSelector{Number: 81},
			},
			Weight: 10,
		}},
		Timeout: types.DurationProto(defaultMaxRevisionTimeout),
		Retries: &istiov1alpha3.HTTPRetry{
			RetryOn:       retriableConditions,
			Attempts:      int32(networking.DefaultRetryCount),
			PerTryTimeout: types.DurationProto(defaultMaxRevisionTimeout),
		},
	}
	if diff := cmp.Diff(expected, route); diff != "" {
		t.Errorf("Unexpected route  (-want +got): %v", diff)
	}
}

func TestGetHosts_Duplicate(t *testing.T) {
	ci := &v1alpha1.Ingress{
		Spec: v1alpha1.IngressSpec{
			Rules: []v1alpha1.IngressRule{{
				Hosts: []string{"test-route1", "test-route2"},
			}, {
				Hosts: []string{"test-route1", "test-route3"},
			}},
		},
	}
	hosts := getHosts(ci)
	expected := sets.NewString("test-route1", "test-route2", "test-route3")
	if diff := cmp.Diff(expected, hosts); diff != "" {
		t.Errorf("Unexpected hosts  (-want +got): %v", diff)
	}
}

func makeGatewayMap(publicGateways []string, privateGateways []string) map[v1alpha1.IngressVisibility]sets.String {
	return map[v1alpha1.IngressVisibility]sets.String{
		v1alpha1.IngressVisibilityExternalIP:   sets.NewString(publicGateways...),
		v1alpha1.IngressVisibilityClusterLocal: sets.NewString(privateGateways...),
	}
}
