/*
Copyright 2019 The Knative Authors

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
	"fmt"
	"hash/adler32"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"knative.dev/pkg/tracker"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1listers "k8s.io/client-go/listers/core/v1"

	istiov1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	"knative.dev/net-istio/pkg/reconciler/ingress/config"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	netconfig "knative.dev/networking/pkg/config"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakeserviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	"knative.dev/pkg/kmeta"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/system"
)

var secret = corev1.Secret{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "secret0",
		Namespace: system.Namespace(),
	},
	Data: map[string][]byte{
		"test": []byte("test"),
	},
}

var secretGVK = schema.GroupVersionKind{Version: "v1", Kind: "Secret"}

var wildcardSecret, _ = GenerateCertificate([]string{"*.example.com"}, "secret0", system.Namespace())

var wildcardSecrets = map[string]*corev1.Secret{
	system.Namespace() + "/secret0": wildcardSecret,
}

var originSecrets = map[string]*corev1.Secret{
	system.Namespace() + "/secret0": &secret,
}

var selector = map[string]string{
	"istio": "ingressgateway",
}

var gateway = v1beta1.Gateway{
	Spec: istiov1beta1.Gateway{
		Servers: servers,
	},
}

var servers = []*istiov1beta1.Server{{
	Hosts: []string{"host1.example.com"},
	Port: &istiov1beta1.Port{
		Name:     "test-ns/ingress:0",
		Number:   ExternalGatewayHTTPSPort,
		Protocol: "HTTPS",
	},
}, {
	Hosts: []string{"host2.example.com"},
	Port: &istiov1beta1.Port{
		Name:     "test-ns/non-ingress:0",
		Number:   ExternalGatewayHTTPSPort,
		Protocol: "HTTPS",
	},
}}

var httpServer = istiov1beta1.Server{
	Hosts: []string{"*"},
	Port: &istiov1beta1.Port{
		Name:     httpServerPortName,
		Number:   GatewayHTTPPort,
		Protocol: "HTTP",
	},
}

var gatewayWithPlaceholderServer = v1beta1.Gateway{
	Spec: istiov1beta1.Gateway{
		Servers: []*istiov1beta1.Server{&placeholderServer},
	},
}

var gatewayWithModifiedWildcardTLSServer = v1beta1.Gateway{
	Spec: istiov1beta1.Gateway{
		Servers: []*istiov1beta1.Server{&modifiedDefaultTLSServer},
	},
}

var modifiedDefaultTLSServer = istiov1beta1.Server{
	Hosts: []string{"added.by.user.example.com"},
	Port: &istiov1beta1.Port{
		Name:     "https",
		Number:   ExternalGatewayHTTPSPort,
		Protocol: "HTTPS",
	},
	Tls: &istiov1beta1.ServerTLSSettings{
		Mode: istiov1beta1.ServerTLSSettings_SIMPLE,
	},
}

var ingressSpec = v1alpha1.IngressSpec{
	Rules: []v1alpha1.IngressRule{{
		Hosts:      []string{"host1.example.com"},
		Visibility: v1alpha1.IngressVisibilityExternalIP,
	}},
	TLS: []v1alpha1.IngressTLS{{
		Hosts:           []string{"host1.example.com"},
		SecretName:      "secret0",
		SecretNamespace: system.Namespace(),
	}},
}

var ingressResource = v1alpha1.Ingress{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "ingress",
		Namespace: "test-ns",
	},
	Spec: ingressSpec,
}

var ingressResourceWithPublicGatewayLabel = v1alpha1.Ingress{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "ingress",
		Namespace: "test-ns",
		Labels: map[string]string{
			"exposition": "special",
		},
	},
	Spec: ingressSpec,
}

var ingressResourceWithDotName = v1alpha1.Ingress{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "ingress.com",
		Namespace: "test-ns",
	},
	Spec: ingressSpec,
}

var defaultGatewayService = corev1.Service{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "istio-ingressgateway",
		Namespace: "istio-system",
	},
	Spec: corev1.ServiceSpec{
		Selector: selector,
	},
}

var configDefaultGateway = &config.Config{
	Istio: &config.Istio{
		IngressGateways: []config.Gateway{{
			Name:       config.KnativeIngressGateway,
			ServiceURL: fmt.Sprintf("%s.%s.svc.cluster.local", defaultGatewayService.Name, defaultGatewayService.Namespace),
		}},
	},
	Network: &netconfig.Config{
		HTTPProtocol: netconfig.HTTPEnabled,
	},
}

var defaultGatewayCmpOpts = protocmp.Transform()

func TestGetServers(t *testing.T) {
	servers := GetServers(&gateway, &ingressResource)
	expected := []*istiov1beta1.Server{{
		Hosts: []string{"host1.example.com"},
		Port: &istiov1beta1.Port{
			Name:     "test-ns/ingress:0",
			Number:   ExternalGatewayHTTPSPort,
			Protocol: "HTTPS",
		},
	}}

	if diff := cmp.Diff(expected, servers, protocmp.Transform()); diff != "" {
		t.Error("Unexpected servers (-want +got):", diff)
	}
}

func TestGetHTTPServer(t *testing.T) {
	newGateway := gateway.DeepCopy()
	newGateway.Spec.Servers = append(newGateway.Spec.Servers, &httpServer)
	server := GetHTTPServer(newGateway)
	expected := &istiov1beta1.Server{
		Hosts: []string{"*"},
		Port: &istiov1beta1.Port{
			Name:     httpServerPortName,
			Number:   GatewayHTTPPort,
			Protocol: "HTTP",
		},
	}
	if diff := cmp.Diff(expected, server, defaultGatewayCmpOpts); diff != "" {
		t.Error("Unexpected server (-want +got):", diff)
	}
}

func TestMakeTLSServers(t *testing.T) {
	cases := []struct {
		name                    string
		ci                      *v1alpha1.Ingress
		gatewayServiceNamespace string
		originSecrets           map[string]*corev1.Secret
		expected                []*istiov1beta1.Server
		wantErr                 bool
	}{{
		name: "secret namespace is the different from the gateway service namespace",
		ci:   &ingressResource,
		// gateway service namespace is "istio-system", while the secret namespace is system.Namespace()("knative-testing").
		gatewayServiceNamespace: "istio-system",
		originSecrets:           originSecrets,
		expected: []*istiov1beta1.Server{{
			Hosts: []string{"host1.example.com"},
			Port: &istiov1beta1.Port{
				Name:     "test-ns/ingress:0",
				Number:   ExternalGatewayHTTPSPort,
				Protocol: "HTTPS",
			},
			Tls: &istiov1beta1.ServerTLSSettings{
				Mode:           istiov1beta1.ServerTLSSettings_SIMPLE,
				CredentialName: targetSecret(&secret, &ingressResource),
			},
		}},
	}, {
		name: "secret namespace is the same as the gateway service namespace",
		ci:   &ingressResource,
		// gateway service namespace and the secret namespace are both in system.Namespace().
		gatewayServiceNamespace: system.Namespace(),
		originSecrets:           originSecrets,
		expected: []*istiov1beta1.Server{{
			Hosts: []string{"host1.example.com"},
			Port: &istiov1beta1.Port{
				Name:     "test-ns/ingress:0",
				Number:   ExternalGatewayHTTPSPort,
				Protocol: "HTTPS",
			},
			Tls: &istiov1beta1.ServerTLSSettings{
				Mode:           istiov1beta1.ServerTLSSettings_SIMPLE,
				CredentialName: "secret0",
			},
		}},
	}, {
		name:                    "port name is created with ingress namespace-name",
		ci:                      &ingressResource,
		gatewayServiceNamespace: system.Namespace(),
		originSecrets:           originSecrets,
		expected: []*istiov1beta1.Server{{
			Hosts: []string{"host1.example.com"},
			Port: &istiov1beta1.Port{
				// port name is created with <namespace>/<name>
				Name:     "test-ns/ingress:0",
				Number:   ExternalGatewayHTTPSPort,
				Protocol: "HTTPS",
			},
			Tls: &istiov1beta1.ServerTLSSettings{
				Mode:           istiov1beta1.ServerTLSSettings_SIMPLE,
				CredentialName: "secret0",
			},
		}},
	}, {
		name:                    "error to make servers because of incorrect originSecrets",
		ci:                      &ingressResource,
		gatewayServiceNamespace: "istio-system",
		originSecrets:           map[string]*corev1.Secret{},
		wantErr:                 true,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			servers, err := MakeTLSServers(c.ci, v1alpha1.IngressVisibilityExternalIP, c.ci.GetIngressTLSForVisibility(v1alpha1.IngressVisibilityExternalIP), c.gatewayServiceNamespace, c.originSecrets)
			if (err != nil) != c.wantErr {
				t.Fatalf("Test: %s; MakeServers error = %v, WantErr %v", c.name, err, c.wantErr)
			}
			if diff := cmp.Diff(c.expected, servers, defaultGatewayCmpOpts); diff != "" {
				t.Error("Unexpected servers (-want, +got):", diff)
			}
		})
	}
}

func TestMakeHTTPServer(t *testing.T) {
	cases := []struct {
		name       string
		httpOption v1alpha1.HTTPOption
		expected   *istiov1beta1.Server
	}{{
		name:       "nil HTTP Server",
		httpOption: "",
		expected:   nil,
	}, {
		name:       "HTTP server",
		httpOption: v1alpha1.HTTPOptionEnabled,
		expected: &istiov1beta1.Server{
			Hosts: []string{"*"},
			Port: &istiov1beta1.Port{
				Name:     httpServerPortName,
				Number:   GatewayHTTPPort,
				Protocol: "HTTP",
			},
		},
	}, {
		name:       "Redirect HTTP server",
		httpOption: v1alpha1.HTTPOptionRedirected,
		expected: &istiov1beta1.Server{
			Hosts: []string{"*"},
			Port: &istiov1beta1.Port{
				Name:     httpServerPortName,
				Number:   GatewayHTTPPort,
				Protocol: "HTTP",
			},
			Tls: &istiov1beta1.ServerTLSSettings{
				HttpsRedirect: true,
			},
		},
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MakeHTTPServer(c.httpOption, []string{"*"})
			if diff := cmp.Diff(c.expected, got, defaultGatewayCmpOpts); diff != "" {
				t.Error("Unexpected HTTP Server (-want, +got):", diff)
			}
		})
	}
}

func TestUpdateGateway(t *testing.T) {
	cases := []struct {
		name            string
		existingServers []*istiov1beta1.Server
		newServers      []*istiov1beta1.Server
		original        *v1beta1.Gateway
		expected        *v1beta1.Gateway
	}{{
		name: "Update Gateway servers.",
		existingServers: []*istiov1beta1.Server{{
			Hosts: []string{"host1.example.com"},
			Port: &istiov1beta1.Port{
				Name:     "test-ns/ingress:0",
				Number:   ExternalGatewayHTTPSPort,
				Protocol: "HTTPS",
			},
		}},
		newServers: []*istiov1beta1.Server{{
			Hosts: []string{"host-new.example.com"},
			Port: &istiov1beta1.Port{
				Name:     "test-ns/ingress:0",
				Number:   ExternalGatewayHTTPSPort,
				Protocol: "HTTPS",
			},
		}},
		original: gateway.DeepCopy(),
		expected: &v1beta1.Gateway{
			Spec: istiov1beta1.Gateway{
				Servers: []*istiov1beta1.Server{{
					// The host name was updated to the one in "newServers".
					Hosts: []string{"host-new.example.com"},
					Port: &istiov1beta1.Port{
						Name:     "test-ns/ingress:0",
						Number:   ExternalGatewayHTTPSPort,
						Protocol: "HTTPS",
					},
				}, {
					Hosts: []string{"host2.example.com"},
					Port: &istiov1beta1.Port{
						Name:     "test-ns/non-ingress:0",
						Number:   ExternalGatewayHTTPSPort,
						Protocol: "HTTPS",
					},
				}},
			},
		},
	}, {
		name: "Delete servers from Gateway",
		existingServers: []*istiov1beta1.Server{{
			Hosts: []string{"host1.example.com"},
			Port: &istiov1beta1.Port{
				Name:     "test-ns/ingress:0",
				Number:   ExternalGatewayHTTPSPort,
				Protocol: "HTTPS",
			},
		}},
		newServers: []*istiov1beta1.Server{},
		original:   gateway.DeepCopy(),
		expected: &v1beta1.Gateway{
			Spec: istiov1beta1.Gateway{
				// Only one server is left. The other one is deleted.
				Servers: []*istiov1beta1.Server{{
					Hosts: []string{"host2.example.com"},
					Port: &istiov1beta1.Port{
						Name:     "test-ns/non-ingress:0",
						Number:   ExternalGatewayHTTPSPort,
						Protocol: "HTTPS",
					},
				}},
			},
		},
	}, {
		name: "Delete servers from Gateway and no real servers are left",

		// All the servers in the original gateway will be deleted.
		existingServers: []*istiov1beta1.Server{{
			Hosts: []string{"host1.example.com"},
			Port: &istiov1beta1.Port{
				Name:     "test-ns/ingress:0",
				Number:   ExternalGatewayHTTPSPort,
				Protocol: "HTTPS",
			},
		}, {
			Hosts: []string{"host2.example.com"},
			Port: &istiov1beta1.Port{
				Name:     "test-ns/non-ingress:0",
				Number:   ExternalGatewayHTTPSPort,
				Protocol: "HTTPS",
			},
		}},
		newServers: []*istiov1beta1.Server{},
		original:   gateway.DeepCopy(),
		expected:   gatewayWithPlaceholderServer.DeepCopy(),
	}, {
		name:            "Add servers to the gateway with only placeholder server",
		existingServers: []*istiov1beta1.Server{},
		newServers: []*istiov1beta1.Server{{
			Hosts: []string{"host1.example.com"},
			Port: &istiov1beta1.Port{
				Name:     "test-ns/ingress:0",
				Number:   ExternalGatewayHTTPSPort,
				Protocol: "HTTPS",
			},
		}},
		original: gatewayWithPlaceholderServer.DeepCopy(),
		// The placeholder server should be deleted.
		expected: &v1beta1.Gateway{
			Spec: istiov1beta1.Gateway{
				Servers: []*istiov1beta1.Server{{
					Hosts: []string{"host1.example.com"},
					Port: &istiov1beta1.Port{
						Name:     "test-ns/ingress:0",
						Number:   ExternalGatewayHTTPSPort,
						Protocol: "HTTPS",
					},
				}},
			},
		},
	}, {
		name:            "Do not delete modified wildcard servers from gateway",
		existingServers: []*istiov1beta1.Server{},
		newServers: []*istiov1beta1.Server{{
			Hosts: []string{"host1.example.com"},
			Port: &istiov1beta1.Port{
				Name:     "clusteringress:0",
				Number:   ExternalGatewayHTTPSPort,
				Protocol: "HTTPS",
			},
		}},
		original: gatewayWithModifiedWildcardTLSServer.DeepCopy(),
		expected: &v1beta1.Gateway{
			Spec: istiov1beta1.Gateway{
				Servers: []*istiov1beta1.Server{
					{
						Hosts: []string{"host1.example.com"},
						Port: &istiov1beta1.Port{
							Name:     "clusteringress:0",
							Number:   ExternalGatewayHTTPSPort,
							Protocol: "HTTPS",
						},
					},
					&modifiedDefaultTLSServer,
				},
			},
		},
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := UpdateGateway(c.original, c.newServers, c.existingServers)
			if diff := cmp.Diff(c.expected, g, defaultGatewayCmpOpts); diff != "" {
				t.Error("Unexpected gateway (-want, +got):", diff)
			}
		})
	}
}

func TestMakeWildcardGateways(t *testing.T) {
	testCases := []struct {
		name            string
		wildcardSecrets map[string]*corev1.Secret
		gatewayService  *corev1.Service
		want            []*v1beta1.Gateway
		wantErr         bool
	}{{
		name:            "happy path: secret namespace is the different from the gateway service namespace",
		wildcardSecrets: wildcardSecrets,
		gatewayService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istio-ingressgateway",
				Namespace: "istio-system",
			},
			Spec: corev1.ServiceSpec{
				Selector: selector,
			},
		},
		want: []*v1beta1.Gateway{{
			ObjectMeta: metav1.ObjectMeta{
				Name:            WildcardGatewayName(wildcardSecret.Name, "istio-system", "istio-ingressgateway"),
				Namespace:       system.Namespace(),
				OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(wildcardSecret, secretGVK)},
			},
			Spec: istiov1beta1.Gateway{
				Selector: selector,
				Servers: []*istiov1beta1.Server{{
					Hosts: []string{"*.example.com"},
					Port: &istiov1beta1.Port{
						Name:     "https",
						Number:   ExternalGatewayHTTPSPort,
						Protocol: "HTTPS",
					},
					Tls: &istiov1beta1.ServerTLSSettings{
						Mode:           istiov1beta1.ServerTLSSettings_SIMPLE,
						CredentialName: targetWildcardSecretName(wildcardSecret.Name, wildcardSecret.Namespace),
					},
				}},
			},
		}},
	}, {
		name:            "happy path: secret namespace is the same as the gateway service namespace",
		wildcardSecrets: wildcardSecrets,
		gatewayService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istio-ingressgateway",
				Namespace: system.Namespace(),
			},
			Spec: corev1.ServiceSpec{
				Selector: selector,
			},
		},
		want: []*v1beta1.Gateway{{
			ObjectMeta: metav1.ObjectMeta{
				Name:            WildcardGatewayName(wildcardSecret.Name, system.Namespace(), "istio-ingressgateway"),
				Namespace:       system.Namespace(),
				OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(wildcardSecret, secretGVK)},
			},
			Spec: istiov1beta1.Gateway{
				Selector: selector,
				Servers: []*istiov1beta1.Server{{
					Hosts: []string{"*.example.com"},
					Port: &istiov1beta1.Port{
						Name:     "https",
						Number:   ExternalGatewayHTTPSPort,
						Protocol: "HTTPS",
					},
					Tls: &istiov1beta1.ServerTLSSettings{
						Mode:           istiov1beta1.ServerTLSSettings_SIMPLE,
						CredentialName: wildcardSecret.Name,
					},
				}},
			},
		}},
	}, {
		name:            "error to make gateway because of incorrect originSecrets",
		wildcardSecrets: map[string]*corev1.Secret{"": &secret},
		gatewayService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istio-ingressgateway",
				Namespace: "istio-system",
			},
			Spec: corev1.ServiceSpec{
				Selector: selector,
			},
		},
		wantErr: true,
	}}

	for _, tc := range testCases {
		ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
		defer cancel()
		svcLister := serviceLister(ctx, tc.gatewayService)
		ctx = config.ToContext(context.Background(), &config.Config{
			Istio: &config.Istio{
				IngressGateways: []config.Gateway{{
					Name:       config.KnativeIngressGateway,
					ServiceURL: fmt.Sprintf("%s.%s.svc.cluster.local", tc.gatewayService.Name, tc.gatewayService.Namespace),
				}},
			},
			Network: &netconfig.Config{
				HTTPProtocol: netconfig.HTTPEnabled,
			},
		})
		t.Run(tc.name, func(t *testing.T) {
			got, err := MakeWildcardTLSGateways(ctx, &ingressResource, tc.wildcardSecrets, svcLister)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Test: %s; MakeWildcardGateways error = %v, WantErr %v", tc.name, err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, got, defaultGatewayCmpOpts); diff != "" {
				t.Error("Unexpected Gateways (-want, +got):", diff)
			}
		})
	}
}

func TestGatewayRef(t *testing.T) {
	gw := &v1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-ingress-gateway",
			Namespace: "knative-serving",
		},
	}
	want := tracker.Reference{
		APIVersion: "networking.istio.io/v1beta1",
		Kind:       "Gateway",
		Name:       "istio-ingress-gateway",
		Namespace:  "knative-serving",
	}
	got := GatewayRef(gw)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatal("GatewayRef failed. diff", diff)
	}
}

func TestGetQualifiedGatewayNames(t *testing.T) {
	gateways := []*v1beta1.Gateway{{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-ingress-gateway",
			Namespace: "knative-serving",
		},
	}}
	want := []string{"knative-serving/istio-ingress-gateway"}
	if got := GetQualifiedGatewayNames(gateways); cmp.Diff(want, got) != "" {
		t.Fatalf("GetQualifiedGatewayNames failed. Want: %v, got: %v", want, got)
	}
}

func TestMakeExternalIngressGateways(t *testing.T) {
	createGateway := func(qualifiedName string, sel map[string]string, serv *istiov1beta1.Server) *v1beta1.Gateway {
		return &v1beta1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:            fmt.Sprintf("ingress-%d", adler32.Checksum([]byte(qualifiedName))),
				Namespace:       "test-ns",
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(&ingressResource)},
				Labels: map[string]string{
					networking.IngressLabelKey: "ingress",
				},
			},
			Spec: istiov1beta1.Gateway{
				Selector: sel,
				Servers:  []*istiov1beta1.Server{serv},
			},
		}
	}

	gateway1Service := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway1",
			Namespace: "aNamespace",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"istio": "ingressgateway1",
			},
		},
	}

	gateway2Service := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway2",
			Namespace: "aNamespace",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"istio": "ingressgateway2",
			},
		},
	}

	configDoubleGateway := &config.Config{
		Istio: &config.Istio{
			IngressGateways: []config.Gateway{
				{
					Namespace:  "knative-serving",
					Name:       "gateway1",
					ServiceURL: "gateway1.aNamespace.svc.cluster.local",
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "exposition",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"special"},
							},
						},
					},
				},
				{
					Namespace:  "knative-serving",
					Name:       "gateway2",
					ServiceURL: "gateway2.aNamespace.svc.cluster.local",
				},
			},
		},
		Network: &netconfig.Config{
			HTTPProtocol: netconfig.HTTPEnabled,
		},
	}

	cases := []struct {
		name    string
		ia      *v1alpha1.Ingress
		conf    *config.Config
		servers []*istiov1beta1.Server
		want    []*v1beta1.Gateway
		wantErr bool
	}{{
		name:    "HTTP server",
		ia:      &ingressResource,
		conf:    configDefaultGateway,
		servers: []*istiov1beta1.Server{&httpServer},
		want:    []*v1beta1.Gateway{createGateway("istio-system/istio-ingressgateway", selector, &httpServer)},
	}, {
		name:    "HTTPS server",
		ia:      &ingressResource,
		conf:    configDefaultGateway,
		servers: []*istiov1beta1.Server{&modifiedDefaultTLSServer},
		want:    []*v1beta1.Gateway{createGateway("istio-system/istio-ingressgateway", selector, &modifiedDefaultTLSServer)},
	}, {
		name:    "HTTP Server Gateways filtered",
		ia:      &ingressResourceWithPublicGatewayLabel,
		conf:    configDoubleGateway,
		servers: []*istiov1beta1.Server{&httpServer},
		want: []*v1beta1.Gateway{createGateway("aNamespace/gateway1", map[string]string{
			"istio": "ingressgateway1",
		}, &httpServer)},
	}, {
		name:    "HTTPS Server Gateways filtered",
		ia:      &ingressResourceWithPublicGatewayLabel,
		conf:    configDoubleGateway,
		servers: []*istiov1beta1.Server{&modifiedDefaultTLSServer},
		want: []*v1beta1.Gateway{createGateway("aNamespace/gateway1", map[string]string{
			"istio": "ingressgateway1",
		}, &modifiedDefaultTLSServer)},
	}, {
		name:    "No gateway matched",
		ia:      &ingressResourceWithPublicGatewayLabel,
		conf:    configDefaultGateway, // default config have a default gateway
		servers: []*istiov1beta1.Server{&httpServer},
		want:    []*v1beta1.Gateway{createGateway("istio-system/istio-ingressgateway", selector, &httpServer)},
	}}
	for _, c := range cases {
		ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
		defer cancel()

		svcLister := serviceLister(ctx, &defaultGatewayService, &gateway1Service, &gateway2Service)
		ctx = config.ToContext(context.Background(), c.conf)

		t.Run(c.name, func(t *testing.T) {
			got, err := MakeExternalIngressGateways(ctx, c.ia, c.servers, svcLister)
			if (err != nil) != c.wantErr {
				t.Fatalf("Test: %s; MakeExternalIngressGateways error = %v, WantErr %v", c.name, err, c.wantErr)
			}
			if diff := cmp.Diff(c.want, got, defaultGatewayCmpOpts); diff != "" {
				t.Error("Unexpected Gateways (-want, +got):", diff)
			}
		})
	}
}

func TestMakeIngressTLSGateways(t *testing.T) {
	cases := []struct {
		name           string
		ia             *v1alpha1.Ingress
		visibility     v1alpha1.IngressVisibility
		originSecrets  map[string]*corev1.Secret
		gatewayService *corev1.Service
		want           []*v1beta1.Gateway
		wantErr        bool
	}{{
		name:          "happy path: secret namespace is the different from the gateway service namespace",
		ia:            &ingressResource,
		visibility:    v1alpha1.IngressVisibilityExternalIP,
		originSecrets: originSecrets,
		gatewayService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istio-ingressgateway",
				Namespace: "istio-system",
			},
			Spec: corev1.ServiceSpec{
				Selector: selector,
			},
		},
		want: []*v1beta1.Gateway{{
			ObjectMeta: metav1.ObjectMeta{
				Name:            fmt.Sprintf("ingress-%d", adler32.Checksum([]byte("istio-system/istio-ingressgateway"))),
				Namespace:       "test-ns",
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(&ingressResource)},
				Labels: map[string]string{
					networking.IngressLabelKey: "ingress",
				},
			},
			Spec: istiov1beta1.Gateway{
				Selector: selector,
				Servers: []*istiov1beta1.Server{{
					Hosts: []string{"host1.example.com"},
					Port: &istiov1beta1.Port{
						Name:     "test-ns/ingress:0",
						Number:   ExternalGatewayHTTPSPort,
						Protocol: "HTTPS",
					},
					Tls: &istiov1beta1.ServerTLSSettings{
						Mode:           istiov1beta1.ServerTLSSettings_SIMPLE,
						CredentialName: targetSecret(&secret, &ingressResource),
					},
				}},
			},
		}},
	}, {
		name:          "happy path: secret namespace is the same as the gateway service namespace",
		ia:            &ingressResource,
		originSecrets: originSecrets,
		visibility:    v1alpha1.IngressVisibilityExternalIP,
		// The namespace of gateway service is the same as the secrets.
		gatewayService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istio-ingressgateway",
				Namespace: system.Namespace(),
			},
			Spec: corev1.ServiceSpec{
				Selector: selector,
			},
		},
		want: []*v1beta1.Gateway{{
			ObjectMeta: metav1.ObjectMeta{
				Name:            fmt.Sprintf("ingress-%d", adler32.Checksum([]byte(system.Namespace()+"/istio-ingressgateway"))),
				Namespace:       "test-ns",
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(&ingressResource)},
				Labels: map[string]string{
					networking.IngressLabelKey: "ingress",
				},
			},
			Spec: istiov1beta1.Gateway{
				Selector: selector,
				Servers: []*istiov1beta1.Server{{
					Hosts: []string{"host1.example.com"},
					Port: &istiov1beta1.Port{
						Name:     "test-ns/ingress:0",
						Number:   ExternalGatewayHTTPSPort,
						Protocol: "HTTPS",
					},
					Tls: &istiov1beta1.ServerTLSSettings{
						Mode:           istiov1beta1.ServerTLSSettings_SIMPLE,
						CredentialName: secret.Name,
					},
				}},
			},
		}},
	}, {
		name: "happy path with cluster-local visibility",
		ia: func() *v1alpha1.Ingress {
			ing := ingressResource.DeepCopy()
			ing.Spec.Rules[0].Visibility = v1alpha1.IngressVisibilityClusterLocal
			return ing
		}(),
		visibility:    v1alpha1.IngressVisibilityClusterLocal,
		originSecrets: originSecrets,
		gatewayService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istio-ingressgateway",
				Namespace: "istio-system",
			},
			Spec: corev1.ServiceSpec{
				Selector: selector,
			},
		},
		want: []*v1beta1.Gateway{{
			ObjectMeta: metav1.ObjectMeta{
				Name:            fmt.Sprintf("ingress-%d", adler32.Checksum([]byte("istio-system/istio-ingressgateway-local"))),
				Namespace:       "test-ns",
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(&ingressResource)},
				Labels: map[string]string{
					networking.IngressLabelKey: "ingress",
				},
			},
			Spec: istiov1beta1.Gateway{
				Selector: selector,
				Servers: []*istiov1beta1.Server{{
					Hosts: []string{"host1.example.com"},
					Port: &istiov1beta1.Port{
						Name:     "test-ns/ingress:0",
						Number:   ClusterLocalGatewayHTTPSPort,
						Protocol: "HTTPS",
					},
					Tls: &istiov1beta1.ServerTLSSettings{
						Mode:           istiov1beta1.ServerTLSSettings_SIMPLE,
						CredentialName: targetSecret(&secret, &ingressResource),
					},
				}},
			},
		}},
	}, {
		name: "ingress name has dot",

		ia:            &ingressResourceWithDotName,
		visibility:    v1alpha1.IngressVisibilityExternalIP,
		originSecrets: originSecrets,
		gatewayService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istio-ingressgateway",
				Namespace: "istio-system",
			},
			Spec: corev1.ServiceSpec{
				Selector: selector,
			},
		},
		want: []*v1beta1.Gateway{{
			ObjectMeta: metav1.ObjectMeta{
				Name:            fmt.Sprintf("%d-%d", adler32.Checksum([]byte("ingress.com")), adler32.Checksum([]byte("istio-system/istio-ingressgateway"))),
				Namespace:       "test-ns",
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(&ingressResourceWithDotName)},
				Labels: map[string]string{
					networking.IngressLabelKey: "ingress.com",
				},
			},
			Spec: istiov1beta1.Gateway{
				Selector: selector,
				Servers: []*istiov1beta1.Server{{
					Hosts: []string{"host1.example.com"},
					Port: &istiov1beta1.Port{
						Name:     fmt.Sprintf("test-ns/%d:0", adler32.Checksum([]byte("ingress.com"))),
						Number:   ExternalGatewayHTTPSPort,
						Protocol: "HTTPS",
					},
					Tls: &istiov1beta1.ServerTLSSettings{
						Mode:           istiov1beta1.ServerTLSSettings_SIMPLE,
						CredentialName: targetSecret(&secret, &ingressResourceWithDotName),
					},
				}},
			},
		}},
	}, {
		name:          "error to make gateway because of incorrect originSecrets",
		ia:            &ingressResource,
		visibility:    v1alpha1.IngressVisibilityExternalIP,
		originSecrets: map[string]*corev1.Secret{},
		gatewayService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istio-ingressgateway",
				Namespace: "istio-system",
			},
			Spec: corev1.ServiceSpec{
				Selector: selector,
			},
		},
		wantErr: true,
	}}

	for _, c := range cases {
		ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
		defer cancel()
		svcLister := serviceLister(ctx, c.gatewayService)
		ctx = config.ToContext(context.Background(), &config.Config{
			Istio: &config.Istio{
				IngressGateways: []config.Gateway{{
					Name:       config.KnativeIngressGateway,
					ServiceURL: fmt.Sprintf("%s.%s.svc.cluster.local", c.gatewayService.Name, c.gatewayService.Namespace),
				}},
			},
			Network: &netconfig.Config{
				HTTPProtocol: netconfig.HTTPEnabled,
			},
		})
		t.Run(c.name, func(t *testing.T) {
			got, err := MakeIngressTLSGateways(ctx, c.ia, c.visibility, c.ia.GetIngressTLSForVisibility(c.visibility), c.originSecrets, svcLister)
			if (err != nil) != c.wantErr {
				t.Fatalf("Test: %s; MakeIngressTLSGateways error = %v, WantErr %v", c.name, err, c.wantErr)
			}
			if diff := cmp.Diff(c.want, got, defaultGatewayCmpOpts); diff != "" {
				t.Error("Unexpected Gateways (-want, +got):", diff)
			}
		})
	}
}

func serviceLister(ctx context.Context, svcs ...*corev1.Service) corev1listers.ServiceLister {
	fake := fakekubeclient.Get(ctx)
	informer := fakeserviceinformer.Get(ctx)

	for _, svc := range svcs {
		fake.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{})
		informer.Informer().GetIndexer().Add(svc)
	}

	return informer.Lister()
}

func TestGatewayName(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway",
			Namespace: "istio-system",
		},
	}
	ingress := &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingress",
			Namespace: "default",
		},
	}

	want := fmt.Sprintf("ingress-%d", adler32.Checksum([]byte("istio-system/gateway")))
	got := GatewayName(ingress, v1alpha1.IngressVisibilityExternalIP, svc)
	if got != want {
		t.Errorf("Unexpected external gateway name. want %q, got %q", want, got)
	}

	want = fmt.Sprintf("ingress-%d", adler32.Checksum([]byte("istio-system/gateway-local")))
	got = GatewayName(ingress, v1alpha1.IngressVisibilityClusterLocal, svc)
	if got != want {
		t.Errorf("Unexpected local gateway name. want %q, got %q", want, got)
	}
}

func TestGatewayNameLongIngressName(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway",
			Namespace: "istio-system",
		},
	}
	ingress := &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "areallyverylongdomainnamethatexcd8923dee789a086a0ac46d3046cbec7",
			Namespace: "default",
		},
	}

	want := fmt.Sprintf("areallyverylongdomainnamethatexcd8923dee789a086a0ac4-%d", adler32.Checksum([]byte("istio-system/gateway")))
	got := GatewayName(ingress, "", svc)
	if got != want {
		t.Errorf("Unexpected gateway name. want %q, got %q", want, got)
	}
}

func TestParseIngressGatewayConfig(t *testing.T) {
	type Expectation struct {
		Error bool
		Value metav1.ObjectMeta
	}

	cases := []struct {
		name    string
		gateway config.Gateway
		want    Expectation
	}{
		{
			name:    "Happy path svc.cluster.local",
			gateway: config.Gateway{ServiceURL: "service.namespace.svc.cluster.local"},
			want:    Expectation{Value: metav1.ObjectMeta{Name: "service", Namespace: "namespace"}},
		},
		{
			name:    "Happy path custom suffix",
			gateway: config.Gateway{ServiceURL: "service.namespace.customsuffix"},
			want:    Expectation{Value: metav1.ObjectMeta{Name: "service", Namespace: "namespace"}},
		},
		{
			name:    "Invalid service URL, no suffix",
			gateway: config.Gateway{ServiceURL: "service.namespace"},
			want:    Expectation{Error: true},
		},
		{
			name:    "Invalid service URL, no namespace, no suffix",
			gateway: config.Gateway{ServiceURL: "service"},
			want:    Expectation{Error: true},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotMeta, err := parseIngressGatewayConfig(c.gateway)
			if (err != nil) != c.want.Error {
				t.Errorf("Expecting error: %v, got error: %v", c.want.Error, err)
			}

			if !c.want.Error {
				if diff := cmp.Diff(c.want.Value, gotMeta); diff != "" {
					t.Error("Unexpected meta (-want, +got):", diff)
				}
			}
		})
	}
}

func TestQualifiedGatewayNamesFromContext(t *testing.T) {
	cases := []struct {
		name       string
		cfg        *config.Istio
		ingress    *v1alpha1.Ingress
		want       map[v1alpha1.IngressVisibility]sets.Set[string]
		shouldFail bool
	}{
		{
			name: "All match",
			cfg: &config.Istio{
				IngressGateways: []config.Gateway{
					{Namespace: "ns1", Name: "gtw1", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"expo": "my-value"}}},
				},
				LocalGateways: []config.Gateway{
					{Namespace: "ns1", Name: "gtw2"},
				},
			},
			ingress: &v1alpha1.Ingress{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
				"expo": "my-value",
			}}},
			want: map[v1alpha1.IngressVisibility]sets.Set[string]{
				v1alpha1.IngressVisibilityExternalIP:   sets.New[string]("ns1/gtw1"),
				v1alpha1.IngressVisibilityClusterLocal: sets.New[string]("ns1/gtw2"),
			},
		},
		{
			name: "Different expo",
			cfg: &config.Istio{
				IngressGateways: []config.Gateway{
					{Namespace: "ns1", Name: "gtw1", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"expo1": "my-value-1"}}},
					{Namespace: "ns1", Name: "gtw2", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"expo2": "my-value-2"}}},
				},
				LocalGateways: []config.Gateway{
					{Namespace: "ns2", Name: "gtw3"},
				},
			},
			ingress: &v1alpha1.Ingress{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
				"expo1": "my-value-1",
			}}},
			want: map[v1alpha1.IngressVisibility]sets.Set[string]{
				v1alpha1.IngressVisibilityExternalIP:   sets.New[string]("ns1/gtw1"),
				v1alpha1.IngressVisibilityClusterLocal: sets.New[string]("ns2/gtw3"),
			},
		},
		{
			name: "Partial match",
			cfg: &config.Istio{
				IngressGateways: []config.Gateway{
					{Namespace: "ns1", Name: "gtw1"},
					{Namespace: "wontmatch", Name: "wontmatch1", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"good-key": "bad-value"}}},
					{Namespace: "wontmatch", Name: "wontmatch2", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"bad-key": "good-value"}}},
					{Namespace: "matchingns", Name: "matching", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"good-key": "good-value"}}},
				},
				LocalGateways: []config.Gateway{
					{Namespace: "ns1", Name: "gtw2"},
				},
			},
			ingress: &v1alpha1.Ingress{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
				"good-key": "good-value",
			}}},
			want: map[v1alpha1.IngressVisibility]sets.Set[string]{
				v1alpha1.IngressVisibilityExternalIP:   sets.New[string]("matchingns/matching"),
				v1alpha1.IngressVisibilityClusterLocal: sets.New[string]("ns1/gtw2"),
			},
		},
		{
			name: "Matching default",
			cfg: &config.Istio{
				IngressGateways: []config.Gateway{
					{Namespace: "ns1", Name: "gtw1"},
					{Namespace: "wontmatch", Name: "wontmatch", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}}},
				},
				LocalGateways: []config.Gateway{
					{Namespace: "ns1", Name: "gtw2"},
				},
			},
			ingress: &v1alpha1.Ingress{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
				"not": "relevant",
			}}},
			want: map[v1alpha1.IngressVisibility]sets.Set[string]{
				v1alpha1.IngressVisibilityExternalIP:   sets.New[string]("ns1/gtw1"),
				v1alpha1.IngressVisibilityClusterLocal: sets.New[string]("ns1/gtw2"),
			},
		},
		{
			name: "No annotation",
			cfg: &config.Istio{
				IngressGateways: []config.Gateway{
					{Namespace: "ns1", Name: "gtw1"},
				},
				LocalGateways: []config.Gateway{
					{Namespace: "ns1", Name: "gtw2"},
				},
			},
			ingress: &v1alpha1.Ingress{},
			want: map[v1alpha1.IngressVisibility]sets.Set[string]{
				v1alpha1.IngressVisibilityExternalIP:   sets.New[string]("ns1/gtw1"),
				v1alpha1.IngressVisibilityClusterLocal: sets.New[string]("ns1/gtw2"),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := config.ToContext(context.Background(), &config.Config{Istio: c.cfg})

			got, err := QualifiedGatewayNamesFromContext(ctx, c.ingress)
			if c.shouldFail && (err == nil) {
				t.Fatal("test is supposed to fail and it's not")
			}

			if !c.shouldFail && (err != nil) {
				t.Fatal("test is supposed to succeed and it's not")
			}

			if c.shouldFail {
				return
			}

			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Error("Unexpected Gateways (-want, +got):", diff)
			}
		})
	}
}
