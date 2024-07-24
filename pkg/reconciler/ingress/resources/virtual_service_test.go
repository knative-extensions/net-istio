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
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	istiov1beta1 "istio.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
)

var (
	defaultGateways         = makeGatewayMap([]string{"gateway"}, []string{"private-gateway"})
	defaultIngressRuleValue = &v1alpha1.HTTPIngressRuleValue{
		Paths: []v1alpha1.HTTPIngressPath{{
			Splits: []v1alpha1.IngressBackendSplit{{
				Percent: 100,
				IngressBackend: v1alpha1.IngressBackend{
					ServiceNamespace: "test",
					ServiceName:      "test.svc.cluster.local",
					ServicePort:      intstr.FromInt(8080),
				},
			}},
		}},
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
	defaultVSCmpOpts = protocmp.Transform()
)

func TestMakeVirtualServices_CorrectMetadata(t *testing.T) {
	for _, tc := range []struct {
		name     string
		gateways map[v1alpha1.IngressVisibility]sets.Set[string]
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
					networking.IngressLabelKey: "test-ingress",
					RouteLabelKey:              "test-route",
					RouteNamespaceLabelKey:     "test-ns",
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
				networking.IngressLabelKey: "test",
				RouteLabelKey:              "test-route",
				RouteNamespaceLabelKey:     "test-ns",
			},
		}, {
			Name:      "test-ingress",
			Namespace: system.Namespace(),
			Labels: map[string]string{
				networking.IngressLabelKey: "test",
				RouteLabelKey:              "test-route",
				RouteNamespaceLabelKey:     "test-ns",
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
					RouteLabelKey:          "test-route",
					RouteNamespaceLabelKey: "test-ns",
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
				networking.IngressLabelKey: "test",
				RouteLabelKey:              "test-route",
				RouteNamespaceLabelKey:     "test-ns",
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
					RouteLabelKey:          "test-route",
					RouteNamespaceLabelKey: "test-ns",
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
				networking.IngressLabelKey: "test-ingress",
				RouteLabelKey:              "test-route",
				RouteNamespaceLabelKey:     "test-ns",
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
					RouteLabelKey:          "test-route",
					RouteNamespaceLabelKey: "test-ns",
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
				networking.IngressLabelKey: "test-ingress",
				RouteLabelKey:              "test-route",
				RouteNamespaceLabelKey:     "test-ns",
			},
		}},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			vss, err := MakeVirtualServices(tc.ci, tc.gateways)
			if err != nil {
				t.Fatal("MakeVirtualServices failed:", err)
			}
			if len(vss) != len(tc.expected) {
				t.Fatalf("Expected %d VirtualService, saw %d", len(tc.expected), len(vss))
			}
			for i := range tc.expected {
				tc.expected[i].OwnerReferences = []metav1.OwnerReference{*kmeta.NewControllerRef(tc.ci)}
				if diff := cmp.Diff(tc.expected[i], vss[i].ObjectMeta); diff != "" {
					t.Error("Unexpected metadata (-want +got):", diff)
				}
			}
		})
	}
}

func TestMakeVirtualServicesSpec_CorrectGateways(t *testing.T) {
	tests := []struct {
		name             string
		ingress          *v1alpha1.Ingress
		gateways         map[v1alpha1.IngressVisibility]sets.Set[string]
		expectedGateways sets.Set[string]
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
				},
			},
			gateways: map[v1alpha1.IngressVisibility]sets.Set[string]{
				v1alpha1.IngressVisibilityClusterLocal: sets.New("knative-local-gateway/knative-serving"),
				v1alpha1.IngressVisibilityExternalIP:   sets.New("knative-ingress-gateway/knative-serving"),
			},
			expectedGateways: sets.New("knative-local-gateway/knative-serving"),
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
				},
			},
			gateways: map[v1alpha1.IngressVisibility]sets.Set[string]{
				v1alpha1.IngressVisibilityClusterLocal: sets.New("knative-local-gateway/knative-serving"),
				v1alpha1.IngressVisibilityExternalIP:   sets.New("knative-ingress-gateway/knative-serving"),
			},
			expectedGateways: sets.New("knative-ingress-gateway/knative-serving"),
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
				},
			},
			gateways: map[v1alpha1.IngressVisibility]sets.Set[string]{
				v1alpha1.IngressVisibilityClusterLocal: sets.New("knative-local-gateway/knative-serving"),
				v1alpha1.IngressVisibilityExternalIP:   sets.New("knative-ingress-gateway/knative-serving"),
			},
			expectedGateways: sets.New("knative-ingress-gateway/knative-serving",
				"knative-local-gateway/knative-serving"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vs := makeVirtualServiceSpec(tc.ingress, tc.gateways, getHosts(tc.ingress))
			actualGateways := sets.New(vs.Gateways...)
			if !actualGateways.Equal(tc.expectedGateways) {
				t.Fatalf("Got gateways %v, expected %v", sets.List(actualGateways), sets.List(tc.expectedGateways))
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
				RouteLabelKey:          "test-route",
				RouteNamespaceLabelKey: "test-ns",
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
	gateways := MakeMeshVirtualService(ci, defaultGateways).Spec.Gateways
	if diff := cmp.Diff(expected, gateways); diff != "" {
		t.Error("Unexpected gateways (-want +got):", diff)
	}
}

func TestMakeMeshVirtualServiceSpecCorrectHosts(t *testing.T) {
	for _, tc := range []struct {
		name          string
		gateways      map[v1alpha1.IngressVisibility]sets.Set[string]
		expectedHosts sets.Set[string]
	}{{
		name: "with cluster local gateway: expanding hosts",
		gateways: map[v1alpha1.IngressVisibility]sets.Set[string]{
			v1alpha1.IngressVisibilityClusterLocal: sets.New("cluster-local"),
		},
		expectedHosts: sets.New(
			"test-route.test-ns.svc.cluster.local",
			"test-route.test-ns.svc",
			"test-route.test-ns",
		),
	}, {
		name:          "with mesh: no exapnding hosts",
		gateways:      map[v1alpha1.IngressVisibility]sets.Set[string]{},
		expectedHosts: sets.New("test-route.test-ns.svc.cluster.local"),
	}} {
		t.Run(tc.name, func(t *testing.T) {
			vs := MakeMeshVirtualService(&defaultIngress, tc.gateways)
			vsHosts := sets.New(vs.Spec.Hosts...)
			if !vsHosts.Equal(tc.expectedHosts) {
				t.Errorf("Unexpected hosts want %v; got %v", tc.expectedHosts, vsHosts)
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
				Visibility: v1alpha1.IngressVisibilityClusterLocal,
				HTTP: &v1alpha1.HTTPIngressRuleValue{
					Paths: []v1alpha1.HTTPIngressPath{{
						Path: "/pets/",
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
					}},
				},
			}, {
				Hosts: []string{
					"v1.domain.com",
				},
				Visibility: v1alpha1.IngressVisibilityClusterLocal,
				HTTP: &v1alpha1.HTTPIngressRuleValue{
					Paths: []v1alpha1.HTTPIngressPath{{
						Path: "/pets/",
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
					}},
				},
			}},
		},
	}
	expected := []*istiov1beta1.HTTPRoute{{
		Retries: &istiov1beta1.HTTPRetry{},
		Match: []*istiov1beta1.HTTPMatchRequest{{
			Uri: &istiov1beta1.StringMatch{
				MatchType: &istiov1beta1.StringMatch_Prefix{Prefix: "/pets/"},
			},
			Authority: &istiov1beta1.StringMatch{
				MatchType: &istiov1beta1.StringMatch_Prefix{Prefix: `test-route.test-ns`},
			},
			Gateways: []string{"mesh"},
		}},
		Route: []*istiov1beta1.HTTPRouteDestination{{
			Destination: &istiov1beta1.Destination{
				Host: "v2-service.test-ns.svc.cluster.local",
				Port: &istiov1beta1.PortSelector{Number: 80},
			},
			Weight: 100,
			Headers: &istiov1beta1.Headers{
				Request: &istiov1beta1.Headers_HeaderOperations{
					Set: map[string]string{
						"ugh": "blah",
					},
				},
			},
		}},
		Headers: &istiov1beta1.Headers{
			Request: &istiov1beta1.Headers_HeaderOperations{
				Set: map[string]string{
					"foo": "bar",
				},
			},
		},
	}}

	routes := MakeMeshVirtualService(ci, defaultGateways).Spec.Http
	if diff := cmp.Diff(expected, routes, defaultVSCmpOpts); diff != "" {
		t.Error("Unexpected routes (-want +got):", diff)
	}
}

func TestMakeIngressVirtualServiceSpec_CorrectGateways(t *testing.T) {
	ci := defaultIngress.DeepCopy()
	// We add public gateways, so make sure that the rules have ExternalIP visibility.
	for idx := range ci.Spec.Rules {
		ci.Spec.Rules[idx].Visibility = v1alpha1.IngressVisibilityExternalIP
	}
	expected := []string{"knative-testing/gateway-one", "knative-testing/gateway-two"}
	gateways := MakeIngressVirtualService(ci, makeGatewayMap([]string{"knative-testing/gateway-one", "knative-testing/gateway-two"}, nil)).Spec.Gateways
	if diff := cmp.Diff(expected, gateways); diff != "" {
		t.Error("Unexpected gateways (-want +got):", diff)
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
						Path: "/pets/",
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
					}},
				},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
			}, {
				Hosts: []string{
					"v1.domain.com",
				},
				HTTP: &v1alpha1.HTTPIngressRuleValue{
					Paths: []v1alpha1.HTTPIngressPath{{
						Path: "/pets/",
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
					}},
				},
			}},
		},
	}

	expected := []*istiov1beta1.HTTPRoute{{
		Retries: &istiov1beta1.HTTPRetry{},
		Match: []*istiov1beta1.HTTPMatchRequest{{
			Uri: &istiov1beta1.StringMatch{
				MatchType: &istiov1beta1.StringMatch_Prefix{Prefix: "/pets/"},
			},
			Authority: &istiov1beta1.StringMatch{
				MatchType: &istiov1beta1.StringMatch_Prefix{Prefix: `domain.com`},
			},
			Gateways: []string{"gateway.public"},
		}, {
			Uri: &istiov1beta1.StringMatch{
				MatchType: &istiov1beta1.StringMatch_Prefix{Prefix: "/pets/"},
			},
			Authority: &istiov1beta1.StringMatch{
				MatchType: &istiov1beta1.StringMatch_Prefix{Prefix: `test-route.test-ns`},
			},
			Gateways: []string{"gateway.public"},
		}},
		Route: []*istiov1beta1.HTTPRouteDestination{{
			Destination: &istiov1beta1.Destination{
				Host: "v2-service.test-ns.svc.cluster.local",
				Port: &istiov1beta1.PortSelector{Number: 80},
			},
			Weight: 100,
			Headers: &istiov1beta1.Headers{
				Request: &istiov1beta1.Headers_HeaderOperations{
					Set: map[string]string{
						"ugh": "blah",
					},
				},
			},
		}},
		Headers: &istiov1beta1.Headers{
			Request: &istiov1beta1.Headers_HeaderOperations{
				Set: map[string]string{
					"foo": "bar",
				},
			},
		},
	}, {
		Retries: &istiov1beta1.HTTPRetry{},
		Match: []*istiov1beta1.HTTPMatchRequest{{
			Uri: &istiov1beta1.StringMatch{
				MatchType: &istiov1beta1.StringMatch_Prefix{Prefix: "/pets/"},
			},
			Authority: &istiov1beta1.StringMatch{
				MatchType: &istiov1beta1.StringMatch_Prefix{Prefix: `v1.domain.com`},
			},
			Gateways: []string{},
		}},
		Route: []*istiov1beta1.HTTPRouteDestination{{
			Destination: &istiov1beta1.Destination{
				Host: "v1-service.test-ns.svc.cluster.local",
				Port: &istiov1beta1.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
		Headers: &istiov1beta1.Headers{
			Request: &istiov1beta1.Headers_HeaderOperations{
				Set: map[string]string{
					"foo": "baz",
				},
			},
		},
	}}

	routes := MakeIngressVirtualService(ci, makeGatewayMap([]string{"gateway.public"}, []string{"gateway.private"})).Spec.Http
	if diff := cmp.Diff(expected, routes, defaultVSCmpOpts); diff != "" {
		t.Error("Unexpected routes (-want +got):", diff)
	}
}

func TestMakeVirtualServiceRoute_RewriteHost(t *testing.T) {
	ingressPath := &v1alpha1.HTTPIngressPath{
		RewriteHost: "the.target.host",
		Splits: []v1alpha1.IngressBackendSplit{{
			Percent: 100,
			IngressBackend: v1alpha1.IngressBackend{
				ServiceName:      "the-svc",
				ServiceNamespace: "the-ns",
				ServicePort:      intstr.FromInt(8080),
			},
		}},
	}
	route := makeVirtualServiceRoute(sets.New("a.vanity.url", "another.vanity.url"), ingressPath, makeGatewayMap([]string{"gateway-1"}, nil), v1alpha1.IngressVisibilityExternalIP)
	expected := &istiov1beta1.HTTPRoute{
		Retries: &istiov1beta1.HTTPRetry{},
		Match: []*istiov1beta1.HTTPMatchRequest{{
			Gateways: []string{"gateway-1"},
			Authority: &istiov1beta1.StringMatch{
				MatchType: &istiov1beta1.StringMatch_Prefix{Prefix: `a.vanity.url`},
			},
		}, {
			Gateways: []string{"gateway-1"},
			Authority: &istiov1beta1.StringMatch{
				MatchType: &istiov1beta1.StringMatch_Prefix{Prefix: `another.vanity.url`},
			},
		}},
		Rewrite: &istiov1beta1.HTTPRewrite{
			Authority: "the.target.host",
		},
		Route: []*istiov1beta1.HTTPRouteDestination{{
			Destination: &istiov1beta1.Destination{
				Host: "the-svc.the-ns.svc.cluster.local",
				Port: &istiov1beta1.PortSelector{
					Number: 8080,
				},
			},
			Weight: 100,
		}},
	}
	if diff := cmp.Diff(expected, route, defaultVSCmpOpts); diff != "" {
		t.Error("Unexpected route  (-want +got):", diff)
	}
}

// One active target.
func TestMakeVirtualServiceRoute_Vanilla(t *testing.T) {
	ingressPath := &v1alpha1.HTTPIngressPath{
		Headers: map[string]v1alpha1.HeaderMatch{
			"my-header": {
				Exact: "my-header-value",
			},
			"my-second-header": {
				Exact: "my-second-header-value",
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
	}
	route := makeVirtualServiceRoute(sets.New("a.com", "b.org"), ingressPath, makeGatewayMap([]string{"gateway-1"}, nil), v1alpha1.IngressVisibilityExternalIP)
	expected := &istiov1beta1.HTTPRoute{
		Retries: &istiov1beta1.HTTPRetry{},
		Match: []*istiov1beta1.HTTPMatchRequest{{
			Gateways: []string{"gateway-1"},
			Authority: &istiov1beta1.StringMatch{
				MatchType: &istiov1beta1.StringMatch_Prefix{Prefix: `a.com`},
			},
			Headers: map[string]*istiov1beta1.StringMatch{
				"my-header": {
					MatchType: &istiov1beta1.StringMatch_Exact{
						Exact: "my-header-value",
					},
				},
				"my-second-header": {
					MatchType: &istiov1beta1.StringMatch_Exact{
						Exact: "my-second-header-value",
					},
				},
			},
		}, {
			Gateways: []string{"gateway-1"},
			Authority: &istiov1beta1.StringMatch{
				MatchType: &istiov1beta1.StringMatch_Prefix{Prefix: `b.org`},
			},
			Headers: map[string]*istiov1beta1.StringMatch{
				"my-header": {
					MatchType: &istiov1beta1.StringMatch_Exact{
						Exact: "my-header-value",
					},
				},
				"my-second-header": {
					MatchType: &istiov1beta1.StringMatch_Exact{
						Exact: "my-second-header-value",
					},
				},
			},
		}},
		Route: []*istiov1beta1.HTTPRouteDestination{{
			Destination: &istiov1beta1.Destination{
				Host: "revision-service.test-ns.svc.cluster.local",
				Port: &istiov1beta1.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
	}
	if diff := cmp.Diff(expected, route, defaultVSCmpOpts); diff != "" {
		t.Error("Unexpected route  (-want +got):", diff)
	}
}

// One active target.
func TestMakeVirtualServiceRoute_Internal(t *testing.T) {
	ingressPath := &v1alpha1.HTTPIngressPath{
		Splits: []v1alpha1.IngressBackendSplit{{
			IngressBackend: v1alpha1.IngressBackend{
				ServiceNamespace: "test-ns",
				ServiceName:      "revision-service",
				ServicePort:      intstr.FromInt(80),
			},
			Percent: 100,
		}},
	}
	route := makeVirtualServiceRoute(sets.New("a.default", "a.default.svc", "a.default.svc.cluster.local"),
		ingressPath, makeGatewayMap([]string{"gateway-1"}, nil), v1alpha1.IngressVisibilityExternalIP)
	expected := &istiov1beta1.HTTPRoute{
		Retries: &istiov1beta1.HTTPRetry{},
		Match: []*istiov1beta1.HTTPMatchRequest{{
			Gateways: []string{"gateway-1"},
			Authority: &istiov1beta1.StringMatch{
				MatchType: &istiov1beta1.StringMatch_Prefix{Prefix: `a.default`},
			},
		}},
		Route: []*istiov1beta1.HTTPRouteDestination{{
			Destination: &istiov1beta1.Destination{
				Host: "revision-service.test-ns.svc.cluster.local",
				Port: &istiov1beta1.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
	}
	if diff := cmp.Diff(expected, route, defaultVSCmpOpts); diff != "" {
		t.Error("Unexpected route  (-want +got):", diff)
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
	}
	route := makeVirtualServiceRoute(sets.New("test.org"), ingressPath, makeGatewayMap([]string{"knative-testing/gateway-1"}, nil), v1alpha1.IngressVisibilityExternalIP)
	expected := &istiov1beta1.HTTPRoute{
		Retries: &istiov1beta1.HTTPRetry{},
		Match: []*istiov1beta1.HTTPMatchRequest{{
			Gateways: []string{"knative-testing/gateway-1"},
			Authority: &istiov1beta1.StringMatch{
				MatchType: &istiov1beta1.StringMatch_Prefix{Prefix: `test.org`},
			},
		}},
		Route: []*istiov1beta1.HTTPRouteDestination{{
			Destination: &istiov1beta1.Destination{
				Host: "revision-service.test-ns.svc.cluster.local",
				Port: &istiov1beta1.PortSelector{Number: 80},
			},
			Weight: 90,
		}, {
			Destination: &istiov1beta1.Destination{
				Host: "new-revision-service.test-ns.svc.cluster.local",
				Port: &istiov1beta1.PortSelector{Number: 81},
			},
			Weight: 10,
		}},
	}
	if diff := cmp.Diff(expected, route, defaultVSCmpOpts); diff != "" {
		t.Error("Unexpected route  (-want +got):", diff)
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
	expected := sets.New("test-route1", "test-route2", "test-route3")
	if diff := cmp.Diff(expected, hosts); diff != "" {
		t.Error("Unexpected hosts  (-want +got):", diff)
	}
}

func makeGatewayMap(publicGateways []string, privateGateways []string) map[v1alpha1.IngressVisibility]sets.Set[string] {
	return map[v1alpha1.IngressVisibility]sets.Set[string]{
		v1alpha1.IngressVisibilityExternalIP:   sets.New(publicGateways...),
		v1alpha1.IngressVisibilityClusterLocal: sets.New(privateGateways...),
	}
}

func TestGetDistinctHostPrefixes(t *testing.T) {
	cases := []struct {
		name string
		in   sets.Set[string]
		out  sets.Set[string]
	}{
		{"empty", sets.New[string](), sets.New[string]()},
		{"single element", sets.New("a"), sets.New("a")},
		{"no overlap", sets.New("a", "b"), sets.New("a", "b")},
		{"overlap", sets.New("a", "ab", "abc"), sets.New("a")},
		{"multiple overlaps", sets.New("a", "ab", "abc", "xyz", "xy", "m"), sets.New("a", "xy", "m")},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := getDistinctHostPrefixes(tt.in)
			if !tt.out.Equal(got) {
				t.Fatalf("Expected %v, got %v", tt.out, got)
			}
		})
	}
}
