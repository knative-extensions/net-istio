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

package ingress

import (
	"context"
	"errors"
	"log"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	istiov1beta1 "istio.io/api/networking/v1beta1"
	v1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	"knative.dev/net-istio/pkg/reconciler/ingress/config"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/pkg/status"

	"go.uber.org/zap/zaptest"
	istiolisters "istio.io/client-go/pkg/listers/networking/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

func TestListProbeTargets(t *testing.T) {
	tests := []struct {
		name            string
		ingress         *v1alpha1.Ingress
		ingressGateways []config.Gateway
		localGateways   []config.Gateway
		gatewayLister   istiolisters.GatewayLister
		endpointsLister corev1listers.EndpointsLister
		serviceLister   corev1listers.ServiceLister
		errMessage      string
		results         []status.ProbeTarget
	}{{
		name: "unqualified gateway",
		ingressGateways: []config.Gateway{{
			Name: "gateway",
		}},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
		errMessage: "unexpected unqualified Gateway name",
	}, {
		name: "gateway error",
		ingressGateways: []config.Gateway{{
			Name:      "gateway",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{fails: true},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
		errMessage: "failed to get Gateway",
	}, {
		name: "service error",
		ingressGateways: []config.Gateway{{
			Name:      "gateway",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Number:   80,
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		serviceLister: &fakeServiceLister{fails: true},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
		errMessage: "failed to get Services",
	}, {
		name: "endpoints error",
		ingressGateways: []config.Gateway{{
			Name:      "gateway",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Number:   80,
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
					Labels: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{fails: true},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
		errMessage: "failed to get Endpoints",
	}, {
		name: "service port not found",
		ingressGateways: []config.Gateway{{
			Name:      "gateway",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Number:   80,
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "bogus",
						Port: 8080,
					}},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{
			endpointses: []*v1.Endpoints{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
			}},
		},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
	}, {
		name: "no endpoints",
		ingressGateways: []config.Gateway{{
			Name:      "gateway",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Number:   80,
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "http",
						Port: 80,
					}},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{
			endpointses: []*v1.Endpoints{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Subsets: []v1.EndpointSubset{},
			}},
		},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
	}, {
		name: "no services",
		ingressGateways: []config.Gateway{{
			Name:      "gateway",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Number:   80,
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		serviceLister:   &fakeServiceLister{},
		endpointsLister: &fakeEndpointsLister{},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
	}, {
		name: "unsupported protocol",
		ingressGateways: []config.Gateway{{
			Name:      "gateway",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Number:   80,
							Protocol: "Mongo",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "http",
						Port: 80,
					}},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
	}, {
		name: "one gateway",
		ingressGateways: []config.Gateway{{
			Name:      "gateway",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Name:     "http",
							Number:   8080,
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{
			endpointses: []*v1.Endpoints{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Subsets: []v1.EndpointSubset{{
					Ports: []v1.EndpointPort{{
						Name: "bogus",
						Port: 8081,
					}, {
						Name: "real",
						Port: 8080,
					}},
					Addresses: []v1.EndpointAddress{{
						IP: "1.1.1.1",
					}},
				}},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
					Labels: map[string]string{
						"gwt": "istio",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "bogus",
						Port: 8081,
					}, {
						Name: "real",
						Port: 8080,
					}},
				},
			}},
		},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
		results: []status.ProbeTarget{{
			PodIPs:  sets.New("1.1.1.1"),
			PodPort: "8080",
			Port:    "8080",
			URLs:    []*url.URL{{Scheme: "http", Host: "foo.bar.com:8080"}},
		}},
	}, {
		name: "one gateway HTTPS",
		ingressGateways: []config.Gateway{{
			Name:      "gateway",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Name:     "https",
							Number:   8443,
							Protocol: "HTTPS",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{
			endpointses: []*v1.Endpoints{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Subsets: []v1.EndpointSubset{{
					Ports: []v1.EndpointPort{{
						Name: "bogus",
						Port: 8081,
					}, {
						Name: "real",
						Port: 8443,
					}},
					Addresses: []v1.EndpointAddress{{
						IP: "1.1.1.1",
					}},
				}},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
					Labels: map[string]string{
						"gwt": "istio",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "bogus",
						Port: 8081,
					}, {
						Name: "real",
						Port: 8443,
					}},
				},
			}},
		},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
		results: []status.ProbeTarget{{
			PodIPs:  sets.New("1.1.1.1"),
			PodPort: "8443",
			Port:    "8443",
			URLs:    []*url.URL{{Scheme: "https", Host: "foo.bar.com:8443"}},
		}},
	}, {
		name: "one gateway HTTPS with MUTUAL TLS",
		ingressGateways: []config.Gateway{{
			Name:      "gateway",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Name:     "https",
							Number:   8443,
							Protocol: "HTTPS",
						},
						Tls: &istiov1beta1.ServerTLSSettings{
							Mode: istiov1beta1.ServerTLSSettings_MUTUAL,
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{
			endpointses: []*v1.Endpoints{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Subsets: []v1.EndpointSubset{{
					Ports: []v1.EndpointPort{{
						Name: "bogus",
						Port: 8081,
					}, {
						Name: "real",
						Port: 8443,
					}},
					Addresses: []v1.EndpointAddress{{
						IP: "1.1.1.1",
					}},
				}},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
					Labels: map[string]string{
						"gwt": "istio",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "bogus",
						Port: 8081,
					}, {
						Name: "real",
						Port: 8443,
					}},
				},
			}},
		},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
		results: []status.ProbeTarget{
			// No results, no client cert available.
		},
	}, {
		name: "Different port between endpoint and gateway service",
		ingressGateways: []config.Gateway{{
			Name:      "gateway",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Name:     "http",
							Number:   80,
							Protocol: "HTTP",
						},
					}, {
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Name:     "https",
							Number:   443,
							Protocol: "HTTPS",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{
			endpointses: []*v1.Endpoints{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Subsets: []v1.EndpointSubset{{
					Ports: []v1.EndpointPort{{
						Name: "bogus",
						Port: 8081,
					}, {
						Name: "real",
						Port: 8080, // Different port number between Endpoint and Gateway Service.
					}},
					Addresses: []v1.EndpointAddress{{
						IP: "1.1.1.1",
					}},
				}},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
					Labels: map[string]string{
						"gwt": "istio",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "bogus",
						Port: 8081,
					}, {
						Name: "real",
						Port: 80,
					}},
				},
			}},
		},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
		results: []status.ProbeTarget{{
			PodIPs:  sets.New("1.1.1.1"),
			PodPort: "8080",
			Port:    "80",
			URLs:    []*url.URL{{Scheme: "http", Host: "foo.bar.com:80"}},
		}},
	}, {
		name: "one gateway, https redirect",
		ingressGateways: []config.Gateway{{
			Name:      "gateway",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Name:     "http",
							Number:   80,
							Protocol: "HTTP",
						},
						Tls: &istiov1beta1.ServerTLSSettings{
							HttpsRedirect: true,
						},
					}, {
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Name:     "https",
							Number:   443,
							Protocol: "HTTPS",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{
			endpointses: []*v1.Endpoints{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Subsets: []v1.EndpointSubset{{
					Ports: []v1.EndpointPort{{
						Name: "bogus",
						Port: 8080,
					}, {
						Name: "real",
						Port: 80,
					}},
					Addresses: []v1.EndpointAddress{{
						IP: "1.1.1.1",
					}},
				}},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
					Labels: map[string]string{
						"gwt": "istio",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "bogus",
						Port: 8080,
					}, {
						Name: "real",
						Port: 80,
					}},
				},
			}},
		},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
		results: []status.ProbeTarget{},
	}, {
		name: "unsupported protocols",
		ingressGateways: []config.Gateway{{
			Name:      "gateway",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Name:     "http",
							Number:   80,
							Protocol: "GRPC",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{
			endpointses: []*v1.Endpoints{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Subsets: []v1.EndpointSubset{{
					Ports: []v1.EndpointPort{{
						Name: "bogus",
						Port: 8080,
					}, {
						Name: "real",
						Port: 80,
					}},
					Addresses: []v1.EndpointAddress{{
						IP: "1.1.1.1",
					}},
				}},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
					Labels: map[string]string{
						"gwt": "istio",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "bogus",
						Port: 8080,
					}, {
						Name: "real",
						Port: 80,
					}},
				},
			}},
		},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
		results: []status.ProbeTarget{},
	}, {
		name: "subsets with no ports",
		ingressGateways: []config.Gateway{{
			Name:      "gateway",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Name:     "http",
							Number:   80,
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{
			endpointses: []*v1.Endpoints{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Subsets: []v1.EndpointSubset{{
					Ports: []v1.EndpointPort{{
						Name: "unknown",
						Port: 9999,
					}},
					Addresses: []v1.EndpointAddress{{
						IP: "1.1.1.1",
					}},
				}},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
					Labels: map[string]string{
						"gwt": "istio",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "bogus",
						Port: 8080,
					}, {
						Name: "real",
						Port: 80,
					}},
				},
			}},
		},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
		results: []status.ProbeTarget{},
	}, {
		name: "two gateways",
		ingressGateways: []config.Gateway{{
			Name:      "gateway",
			Namespace: "default",
		}, {
			Name:      "gateway-two",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Name:     "http",
							Number:   80,
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}, {
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway-two",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Name:     "http",
							Number:   90,
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "gateway-two",
					},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{
			endpointses: []*v1.Endpoints{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Subsets: []v1.EndpointSubset{{
					Ports: []v1.EndpointPort{{
						Name: "bogus",
						Port: 8080,
					}, {
						Name: "real",
						Port: 80,
					}},
					Addresses: []v1.EndpointAddress{{
						IP: "1.1.1.1",
					}},
				}},
			}, {
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway-two",
				},
				Subsets: []v1.EndpointSubset{{
					Ports: []v1.EndpointPort{{
						Name: "bogus",
						Port: 8080,
					}, {
						Name: "real",
						Port: 90,
					}},
					Addresses: []v1.EndpointAddress{{
						IP: "2.2.2.2",
					}, {
						IP: "2.2.2.3",
					}},
				}},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
					Labels: map[string]string{
						"gwt": "istio",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "bogus",
						Port: 8080,
					}, {
						Name: "real",
						Port: 80,
					}},
				},
			}, {
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway-two",
					Labels: map[string]string{
						"gwt": "gateway-two",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "bogus",
						Port: 8080,
					}, {
						Name: "real",
						Port: 90,
					}},
				},
			}},
		},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
		results: []status.ProbeTarget{{
			PodIPs:  sets.New("1.1.1.1"),
			PodPort: "80",
			Port:    "80",
			URLs:    []*url.URL{{Scheme: "http", Host: "foo.bar.com:80"}},
		}, {
			PodIPs:  sets.New("2.2.2.2", "2.2.2.3"),
			PodPort: "90",
			Port:    "90",
			URLs:    []*url.URL{{Scheme: "http", Host: "foo.bar.com:90"}},
		}},
	}, {
		name: "pick first host",
		ingressGateways: []config.Gateway{{
			Name:      "ingress-gateway",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "ingress-gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Name:     "http",
							Number:   80,
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{
			endpointses: []*v1.Endpoints{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "ingress-gateway",
				},
				Subsets: []v1.EndpointSubset{{
					Ports: []v1.EndpointPort{{
						Name: "real",
						Port: 80,
					}},
					Addresses: []v1.EndpointAddress{{
						IP: "1.1.1.1",
					}},
				}},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "ingress-gateway",
					Labels: map[string]string{
						"gwt": "istio",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "real",
						Port: 80,
					}},
				},
			}},
		},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
						"route.knative.dev",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
		results: []status.ProbeTarget{{
			PodIPs:  sets.New("1.1.1.1"),
			PodPort: "80",
			Port:    "80",
			URLs: []*url.URL{
				{Scheme: "http", Host: "foo.bar.com:80"},
			},
		}},
	}, {
		name: "local gateways",
		localGateways: []config.Gateway{{
			Name:      "local-gateway",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Name:     "http",
							Number:   80,
							Protocol: "HTTP",
						},
					}, {
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Name:     "https",
							Number:   443,
							Protocol: "HTTPS",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}, {
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "local-gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Name:     "http",
							Number:   80,
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "local-gateway",
					},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{
			endpointses: []*v1.Endpoints{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Subsets: []v1.EndpointSubset{{
					Ports: []v1.EndpointPort{{
						Name: "bogus",
						Port: 8080,
					}, {
						Name: "real",
						Port: 80,
					}},
					Addresses: []v1.EndpointAddress{{
						IP: "1.1.1.1",
					}},
				}},
			}, {
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "local-gateway",
				},
				Subsets: []v1.EndpointSubset{{
					Ports: []v1.EndpointPort{{
						Name: "bogus",
						Port: 8080,
					}, {
						Name: "real",
						Port: 80,
					}},
					Addresses: []v1.EndpointAddress{{
						IP: "2.2.2.2",
					}, {
						IP: "2.2.2.3",
					}},
				}},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
					Labels: map[string]string{
						"gwt": "istio",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "bogus",
						Port: 8080,
					}, {
						Name: "real",
						Port: 80,
					}},
				},
			}, {
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "local-gateway",
					Labels: map[string]string{
						"gwt": "local-gateway",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "bogus",
						Port: 8080,
					}, {
						Name: "real",
						Port: 80,
					}},
				},
			}},
		},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.svc.cluster.local",
					},
					Visibility: v1alpha1.IngressVisibilityClusterLocal,
				}},
			},
		},
		results: []status.ProbeTarget{{
			PodIPs:  sets.New("2.2.2.2", "2.2.2.3"),
			PodPort: "80",
			Port:    "80",
			URLs: []*url.URL{
				{Scheme: "http", Host: "foo.bar:80"},
			},
		}},
	}, {
		name: "two servers, same port, same protocol",
		ingressGateways: []config.Gateway{{
			Name:      "gateway",
			Namespace: "default",
		}},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1beta1.Gateway{
					Servers: []*istiov1beta1.Server{{
						Hosts: []string{"*"},
						Port: &istiov1beta1.Port{
							Name:     "http",
							Number:   80,
							Protocol: "HTTP",
						},
					}, {
						Hosts: []string{"foo.bar.com"},
						Port: &istiov1beta1.Port{
							Name:     "http",
							Number:   80,
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{
			endpointses: []*v1.Endpoints{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Subsets: []v1.EndpointSubset{{
					Ports: []v1.EndpointPort{{
						Name: "bogus",
						Port: 8080,
					}, {
						Name: "real",
						Port: 80,
					}},
					Addresses: []v1.EndpointAddress{{
						IP: "1.1.1.1",
					}},
				}},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
					Labels: map[string]string{
						"gwt": "istio",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "real",
						Port: 80,
					}},
				},
			}},
		},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
		results: []status.ProbeTarget{{
			PodIPs:  sets.New("1.1.1.1"),
			PodPort: "80",
			Port:    "80",
			URLs:    []*url.URL{{Scheme: "http", Host: "foo.bar.com:80"}},
		}},
	}, {
		name: "one gateway matched",
		ingressGateways: []config.Gateway{
			{
				Name:      "gateway-not-matching",
				Namespace: "default",
			},
			{
				Name:      "gateway-matching",
				Namespace: "default",
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
		},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1beta1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "gateway-not-matching",
					},
					Spec: istiov1beta1.Gateway{
						Servers: []*istiov1beta1.Server{{
							Hosts: []string{"*"},
							Port: &istiov1beta1.Port{
								Name:     "http",
								Number:   8888,
								Protocol: "HTTP",
							},
						}},
						Selector: map[string]string{
							"gwt": "istio-not-matching",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "gateway-matching",
					},
					Spec: istiov1beta1.Gateway{
						Servers: []*istiov1beta1.Server{{
							Hosts: []string{"*"},
							Port: &istiov1beta1.Port{
								Name:     "http",
								Number:   8080,
								Protocol: "HTTP",
							},
						}},
						Selector: map[string]string{
							"gwt": "istio",
						},
					},
				},
			},
		},
		endpointsLister: &fakeEndpointsLister{
			endpointses: []*v1.Endpoints{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway-matching",
				},
				Subsets: []v1.EndpointSubset{{
					Ports: []v1.EndpointPort{{
						Name: "bogus",
						Port: 8081,
					}, {
						Name: "real",
						Port: 8080,
					}},
					Addresses: []v1.EndpointAddress{{
						IP: "1.1.1.1",
					}},
				}},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway-matching",
					Labels: map[string]string{
						"gwt": "istio",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "bogus",
						Port: 8081,
					}, {
						Name: "real",
						Port: 8080,
					}},
				},
			}},
		},
		ingress: &v1alpha1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "whatever",
				Labels: map[string]string{
					"exposition": "special",
				},
			},
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.com",
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
		results: []status.ProbeTarget{{
			PodIPs:  sets.New("1.1.1.1"),
			PodPort: "8080",
			Port:    "8080",
			URLs:    []*url.URL{{Scheme: "http", Host: "foo.bar.com:8080"}},
		}},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lister := gatewayPodTargetLister{
				logger:          zaptest.NewLogger(t).Sugar(),
				gatewayLister:   test.gatewayLister,
				endpointsLister: test.endpointsLister,
				serviceLister:   test.serviceLister,
			}
			ctx := config.ToContext(context.Background(), &config.Config{
				Istio: &config.Istio{
					IngressGateways: test.ingressGateways,
					LocalGateways:   test.localGateways,
				},
			})
			results, err := lister.ListProbeTargets(ctx, test.ingress)
			if err == nil {
				if test.errMessage != "" {
					t.Fatalf("expected error message %q, saw no error", test.errMessage)
				}
			} else if !strings.Contains(err.Error(), test.errMessage) {
				t.Errorf("expected error message %q but saw %v", test.errMessage, err)
			}
			if len(test.results)+len(results) > 0 { // consider nil map == empty map
				// Sort by port number
				sort.Slice(results, func(i, j int) bool {
					return results[i].Port < results[j].Port
				})
				if diff := cmp.Diff(test.results, results); diff != "" {
					t.Error("Unexpected probe targets (-want +got):", diff)
				}
			}
		})
	}
}

type fakeGatewayLister struct {
	gateways []*v1beta1.Gateway
	fails    bool
}

func (l *fakeGatewayLister) Gateways(namespace string) istiolisters.GatewayNamespaceLister {
	if l.fails {
		return &fakeGatewayNamespaceLister{fails: true}
	}

	var matches []*v1beta1.Gateway
	for _, gateway := range l.gateways {
		if gateway.Namespace == namespace {
			matches = append(matches, gateway)
		}
	}
	return &fakeGatewayNamespaceLister{
		gateways: matches,
	}
}

func (l *fakeGatewayLister) List(_ labels.Selector) ([]*v1beta1.Gateway, error) {
	log.Panic("not implemented")
	return nil, nil
}

type fakeGatewayNamespaceLister struct {
	gateways []*v1beta1.Gateway
	fails    bool
}

func (l *fakeGatewayNamespaceLister) List(_ labels.Selector) ([]*v1beta1.Gateway, error) {
	log.Panic("not implemented")
	return nil, nil
}

func (l *fakeGatewayNamespaceLister) Get(name string) (*v1beta1.Gateway, error) {
	if l.fails {
		return nil, errors.New("failed to get Gateway")
	}

	for _, gateway := range l.gateways {
		if gateway.Name == name {
			return gateway, nil
		}
	}
	return nil, errors.New("not found")
}

type fakeEndpointsLister struct {
	// Golum, golum.
	endpointses []*v1.Endpoints
	fails       bool
}

func (l *fakeEndpointsLister) List(_ labels.Selector) ([]*v1.Endpoints, error) {
	log.Panic("not implemented")
	return nil, nil
}

func (l *fakeEndpointsLister) Endpoints(_ string) corev1listers.EndpointsNamespaceLister {
	return l
}

func (l *fakeEndpointsLister) Get(name string) (*v1.Endpoints, error) {
	if l.fails {
		return nil, errors.New("failed to get Endpoints")
	}
	for _, ep := range l.endpointses {
		if ep.Name == name {
			return ep, nil
		}
	}
	return nil, errors.New("not found")
}

type fakeServiceLister struct {
	services []*v1.Service
	fails    bool
}

func (l *fakeServiceLister) List(selector labels.Selector) ([]*v1.Service, error) {
	if l.fails {
		return nil, errors.New("failed to get Services")
	}
	results := []*v1.Service{}
	for _, svc := range l.services {
		if selector.Matches(labels.Set(svc.Labels)) {
			results = append(results, svc)
		}
	}
	return results, nil
}

func (l *fakeServiceLister) Services(_ string) corev1listers.ServiceNamespaceLister {
	log.Panic("not implemented")
	return nil
}

func (l *fakeServiceLister) GetPodServices(_ *v1.Pod) ([]*v1.Service, error) {
	log.Panic("not implemented")
	return nil, nil
}
