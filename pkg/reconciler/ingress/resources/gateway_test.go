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
	fmt.Sprintf("%s/secret0", system.Namespace()): wildcardSecret,
}

var originSecrets = map[string]*corev1.Secret{
	fmt.Sprintf("%s/secret0", system.Namespace()): &secret,
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
		Number:   443,
		Protocol: "HTTPS",
	},
	Tls: &istiov1beta1.ServerTLSSettings{
		Mode:              istiov1beta1.ServerTLSSettings_SIMPLE,
		ServerCertificate: corev1.TLSCertKey,
		PrivateKey:        corev1.TLSPrivateKeyKey,
	},
}, {
	Hosts: []string{"host2.example.com"},
	Port: &istiov1beta1.Port{
		Name:     "test-ns/non-ingress:0",
		Number:   443,
		Protocol: "HTTPS",
	},
	Tls: &istiov1beta1.ServerTLSSettings{
		Mode:              istiov1beta1.ServerTLSSettings_SIMPLE,
		ServerCertificate: corev1.TLSCertKey,
		PrivateKey:        corev1.TLSPrivateKeyKey,
	},
}}

var httpServer = istiov1beta1.Server{
	Hosts: []string{"*"},
	Port: &istiov1beta1.Port{
		Name:     httpServerPortName,
		Number:   80,
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
		Number:   443,
		Protocol: "HTTPS",
	},
	Tls: &istiov1beta1.ServerTLSSettings{
		Mode:              istiov1beta1.ServerTLSSettings_SIMPLE,
		ServerCertificate: corev1.TLSCertKey,
		PrivateKey:        corev1.TLSPrivateKeyKey,
	},
}

var ingressSpec = v1alpha1.IngressSpec{
	Rules: []v1alpha1.IngressRule{{
		Hosts: []string{"host1.example.com"},
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

var defaultGatewayCmpOpts = protocmp.Transform()

func TestGetServers(t *testing.T) {
	servers := GetServers(&gateway, &ingressResource)
	expected := []*istiov1beta1.Server{{
		Hosts: []string{"host1.example.com"},
		Port: &istiov1beta1.Port{
			Name:     "test-ns/ingress:0",
			Number:   443,
			Protocol: "HTTPS",
		},
		Tls: &istiov1beta1.ServerTLSSettings{
			Mode:              istiov1beta1.ServerTLSSettings_SIMPLE,
			ServerCertificate: corev1.TLSCertKey,
			PrivateKey:        corev1.TLSPrivateKeyKey,
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
			Number:   80,
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
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1beta1.ServerTLSSettings{
				Mode:               istiov1beta1.ServerTLSSettings_SIMPLE,
				ServerCertificate:  corev1.TLSCertKey,
				PrivateKey:         corev1.TLSPrivateKeyKey,
				CredentialName:     targetSecret(&secret, &ingressResource),
				MinProtocolVersion: istiov1beta1.ServerTLSSettings_TLSV1_2,
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
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1beta1.ServerTLSSettings{
				Mode:               istiov1beta1.ServerTLSSettings_SIMPLE,
				ServerCertificate:  corev1.TLSCertKey,
				PrivateKey:         corev1.TLSPrivateKeyKey,
				CredentialName:     "secret0",
				MinProtocolVersion: istiov1beta1.ServerTLSSettings_TLSV1_2,
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
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1beta1.ServerTLSSettings{
				Mode:               istiov1beta1.ServerTLSSettings_SIMPLE,
				ServerCertificate:  corev1.TLSCertKey,
				PrivateKey:         corev1.TLSPrivateKeyKey,
				CredentialName:     "secret0",
				MinProtocolVersion: istiov1beta1.ServerTLSSettings_TLSV1_2,
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
			servers, err := MakeTLSServers(c.ci, c.ci.Spec.TLS, c.gatewayServiceNamespace, c.originSecrets)
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
				Number:   80,
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
				Number:   80,
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
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1beta1.ServerTLSSettings{
				Mode:              istiov1beta1.ServerTLSSettings_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
			},
		}},
		newServers: []*istiov1beta1.Server{{
			Hosts: []string{"host-new.example.com"},
			Port: &istiov1beta1.Port{
				Name:     "test-ns/ingress:0",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1beta1.ServerTLSSettings{
				Mode:              istiov1beta1.ServerTLSSettings_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
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
						Number:   443,
						Protocol: "HTTPS",
					},
					Tls: &istiov1beta1.ServerTLSSettings{
						Mode:              istiov1beta1.ServerTLSSettings_SIMPLE,
						ServerCertificate: corev1.TLSCertKey,
						PrivateKey:        corev1.TLSPrivateKeyKey,
					},
				}, {
					Hosts: []string{"host2.example.com"},
					Port: &istiov1beta1.Port{
						Name:     "test-ns/non-ingress:0",
						Number:   443,
						Protocol: "HTTPS",
					},
					Tls: &istiov1beta1.ServerTLSSettings{
						Mode:              istiov1beta1.ServerTLSSettings_SIMPLE,
						ServerCertificate: corev1.TLSCertKey,
						PrivateKey:        corev1.TLSPrivateKeyKey,
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
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1beta1.ServerTLSSettings{
				Mode:              istiov1beta1.ServerTLSSettings_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
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
						Number:   443,
						Protocol: "HTTPS",
					},
					Tls: &istiov1beta1.ServerTLSSettings{
						Mode:              istiov1beta1.ServerTLSSettings_SIMPLE,
						ServerCertificate: corev1.TLSCertKey,
						PrivateKey:        corev1.TLSPrivateKeyKey,
					},
				}},
			},
		},
	}, {
		name: "Delete servers from Gateway and no real servers are left",

		// All of the servers in the original gateway will be deleted.
		existingServers: []*istiov1beta1.Server{{
			Hosts: []string{"host1.example.com"},
			Port: &istiov1beta1.Port{
				Name:     "test-ns/ingress:0",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1beta1.ServerTLSSettings{
				Mode:              istiov1beta1.ServerTLSSettings_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
			},
		}, {
			Hosts: []string{"host2.example.com"},
			Port: &istiov1beta1.Port{
				Name:     "test-ns/non-ingress:0",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1beta1.ServerTLSSettings{
				Mode:              istiov1beta1.ServerTLSSettings_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
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
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1beta1.ServerTLSSettings{
				Mode:               istiov1beta1.ServerTLSSettings_SIMPLE,
				ServerCertificate:  corev1.TLSCertKey,
				PrivateKey:         corev1.TLSPrivateKeyKey,
				MinProtocolVersion: istiov1beta1.ServerTLSSettings_TLSV1_2,
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
						Number:   443,
						Protocol: "HTTPS",
					},
					Tls: &istiov1beta1.ServerTLSSettings{
						Mode:               istiov1beta1.ServerTLSSettings_SIMPLE,
						ServerCertificate:  corev1.TLSCertKey,
						PrivateKey:         corev1.TLSPrivateKeyKey,
						MinProtocolVersion: istiov1beta1.ServerTLSSettings_TLSV1_2,
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
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1beta1.ServerTLSSettings{
				Mode:              istiov1beta1.ServerTLSSettings_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
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
							Number:   443,
							Protocol: "HTTPS",
						},
						Tls: &istiov1beta1.ServerTLSSettings{
							Mode:              istiov1beta1.ServerTLSSettings_SIMPLE,
							ServerCertificate: corev1.TLSCertKey,
							PrivateKey:        corev1.TLSPrivateKeyKey,
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
						Number:   443,
						Protocol: "HTTPS",
					},
					Tls: &istiov1beta1.ServerTLSSettings{
						Mode:               istiov1beta1.ServerTLSSettings_SIMPLE,
						ServerCertificate:  corev1.TLSCertKey,
						PrivateKey:         corev1.TLSPrivateKeyKey,
						CredentialName:     targetWildcardSecretName(wildcardSecret.Name, wildcardSecret.Namespace),
						MinProtocolVersion: istiov1beta1.ServerTLSSettings_TLSV1_2,
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
						Number:   443,
						Protocol: "HTTPS",
					},
					Tls: &istiov1beta1.ServerTLSSettings{
						Mode:               istiov1beta1.ServerTLSSettings_SIMPLE,
						ServerCertificate:  corev1.TLSCertKey,
						PrivateKey:         corev1.TLSPrivateKeyKey,
						CredentialName:     wildcardSecret.Name,
						MinProtocolVersion: istiov1beta1.ServerTLSSettings_TLSV1_2,
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

func TestMakeIngressGateways(t *testing.T) {
	cases := []struct {
		name    string
		ia      *v1alpha1.Ingress
		servers []*istiov1beta1.Server
		want    []*v1beta1.Gateway
		wantErr bool
	}{{
		name:    "HTTP server",
		ia:      &ingressResource,
		servers: []*istiov1beta1.Server{&httpServer},
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
				Servers:  []*istiov1beta1.Server{&httpServer},
			},
		}},
	}, {
		name:    "HTTPS server",
		ia:      &ingressResource,
		servers: []*istiov1beta1.Server{&modifiedDefaultTLSServer},
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
				Servers:  []*istiov1beta1.Server{&modifiedDefaultTLSServer},
			},
		}},
	}}
	for _, c := range cases {
		ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
		defer cancel()
		svcLister := serviceLister(ctx, &defaultGatewayService)
		ctx = config.ToContext(context.Background(), &config.Config{
			Istio: &config.Istio{
				IngressGateways: []config.Gateway{{
					Name:       config.KnativeIngressGateway,
					ServiceURL: fmt.Sprintf("%s.%s.svc.cluster.local", defaultGatewayService.Name, defaultGatewayService.Namespace),
				}},
			},
			Network: &netconfig.Config{
				HTTPProtocol: netconfig.HTTPEnabled,
			},
		})
		t.Run(c.name, func(t *testing.T) {
			got, err := MakeIngressGateways(ctx, c.ia, c.servers, svcLister)
			if (err != nil) != c.wantErr {
				t.Fatalf("Test: %s; MakeIngressTLSGateways error = %v, WantErr %v", c.name, err, c.wantErr)
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
		originSecrets  map[string]*corev1.Secret
		gatewayService *corev1.Service
		want           []*v1beta1.Gateway
		wantErr        bool
	}{{
		name:          "happy path: secret namespace is the different from the gateway service namespace",
		ia:            &ingressResource,
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
						Number:   443,
						Protocol: "HTTPS",
					},
					Tls: &istiov1beta1.ServerTLSSettings{
						Mode:               istiov1beta1.ServerTLSSettings_SIMPLE,
						ServerCertificate:  corev1.TLSCertKey,
						PrivateKey:         corev1.TLSPrivateKeyKey,
						CredentialName:     targetSecret(&secret, &ingressResource),
						MinProtocolVersion: istiov1beta1.ServerTLSSettings_TLSV1_2,
					},
				}},
			},
		}},
	}, {
		name:          "happy path: secret namespace is the same as the gateway service namespace",
		ia:            &ingressResource,
		originSecrets: originSecrets,
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
						Number:   443,
						Protocol: "HTTPS",
					},
					Tls: &istiov1beta1.ServerTLSSettings{
						Mode:               istiov1beta1.ServerTLSSettings_SIMPLE,
						ServerCertificate:  corev1.TLSCertKey,
						PrivateKey:         corev1.TLSPrivateKeyKey,
						CredentialName:     secret.Name,
						MinProtocolVersion: istiov1beta1.ServerTLSSettings_TLSV1_2,
					},
				}},
			},
		}},
	}, {
		name: "ingress name has dot",

		ia:            &ingressResourceWithDotName,
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
						Number:   443,
						Protocol: "HTTPS",
					},
					Tls: &istiov1beta1.ServerTLSSettings{
						Mode:               istiov1beta1.ServerTLSSettings_SIMPLE,
						ServerCertificate:  corev1.TLSCertKey,
						PrivateKey:         corev1.TLSPrivateKeyKey,
						CredentialName:     targetSecret(&secret, &ingressResourceWithDotName),
						MinProtocolVersion: istiov1beta1.ServerTLSSettings_TLSV1_2,
					},
				}},
			},
		}},
	}, {
		name:          "error to make gateway because of incorrect originSecrets",
		ia:            &ingressResource,
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
			got, err := MakeIngressTLSGateways(ctx, c.ia, c.ia.Spec.TLS, c.originSecrets, svcLister)
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
	got := GatewayName(ingress, svc)
	if got != want {
		t.Errorf("Unexpected gateway name. want %q, got %q", want, got)
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
	got := GatewayName(ingress, svc)
	if got != want {
		t.Errorf("Unexpected gateway name. want %q, got %q", want, got)
	}
}

func TestGetGatewaysFromAnnotations(t *testing.T) {
	cases := []struct {
		name       string
		ingress    *v1alpha1.Ingress
		wantPublic sets.String
		wantLocal  sets.String
	}{
		{
			name: "Happy path",
			ingress: &v1alpha1.Ingress{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				PublicGatewayAnnotation: "ns1/gtw1",
				LocalGatewaysAnnotation: "ns1/gtw2,ns2/gtw3",
			}}},
			wantPublic: sets.NewString("ns1/gtw1"),
			wantLocal:  sets.NewString("ns1/gtw2", "ns2/gtw3"),
		},
		{
			name: "Empty/No annotation",
			ingress: &v1alpha1.Ingress{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				PublicGatewayAnnotation: "",
			}}},
			wantPublic: sets.NewString(),
			wantLocal:  sets.NewString(),
		},
		{
			name: "Invalid annotation",
			ingress: &v1alpha1.Ingress{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				PublicGatewayAnnotation: "ns1.gtw1",
			}}},
			wantPublic: sets.NewString(),
			wantLocal:  sets.NewString(),
		},
		{
			name: "Annotation with spaces",
			ingress: &v1alpha1.Ingress{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				PublicGatewayAnnotation: "  ns1/gtw1 ",
				LocalGatewaysAnnotation: "ns1/gtw2  ,  ns2/gtw3 ",
			}}},
			wantPublic: sets.NewString("ns1/gtw1"),
			wantLocal:  sets.NewString("ns1/gtw2", "ns2/gtw3"),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotPublic := GetGatewaysFromAnnotations(c.ingress, v1alpha1.IngressVisibilityExternalIP)
			if diff := cmp.Diff(c.wantPublic, gotPublic); diff != "" {
				t.Error("Unexpected public Gateways (-want, +got):", diff)
			}

			gotLocal := GetGatewaysFromAnnotations(c.ingress, v1alpha1.IngressVisibilityClusterLocal)
			if diff := cmp.Diff(c.wantLocal, gotLocal); diff != "" {
				t.Error("Unexpected local Gateways (-want, +got):", diff)
			}
		})
	}
}
