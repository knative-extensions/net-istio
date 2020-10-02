/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
istributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ingress

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	// Inject our fakes
	istioclient "knative.dev/net-istio/pkg/client/istio/injection/client"
	fakeistioclient "knative.dev/net-istio/pkg/client/istio/injection/client/fake"
	_ "knative.dev/net-istio/pkg/client/istio/injection/informers/networking/v1alpha3/gateway/fake"
	_ "knative.dev/net-istio/pkg/client/istio/injection/informers/networking/v1alpha3/virtualservice/fake"
	fakenetworkingclient "knative.dev/networking/pkg/client/injection/client/fake"
	fakeingressclient "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/ingress/fake"
	"knative.dev/networking/pkg/ingress"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/pod/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/secret/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	"knative.dev/pkg/logging"

	proto "github.com/gogo/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/sync/errgroup"
	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgotesting "k8s.io/client-go/testing"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	pkgreconciler "knative.dev/pkg/reconciler"

	"knative.dev/net-istio/pkg/reconciler/ingress/config"
	"knative.dev/net-istio/pkg/reconciler/ingress/resources"
	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	ingressreconciler "knative.dev/networking/pkg/client/injection/reconciler/networking/v1alpha1/ingress"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"

	. "knative.dev/net-istio/pkg/reconciler/testing"
	. "knative.dev/pkg/reconciler/testing"
)

const (
	originDomainInternal = "istio-ingressgateway.istio-system.svc.cluster.local"
	newDomainInternal    = "custom.istio-system.svc.cluster.local"
	targetSecretName     = "reconciling-ingress-uid"
	testNS               = "test-ns"
)

// ingressfinalizer is the name that we put into the resource finalizer list, e.g.
//  metadata:
//    finalizers:
//    - ingresses.networking.internal.knative.dev
var (
	ingressResource  = v1alpha1.Resource("ingresses")
	ingressFinalizer = ingressResource.String()
)

var (
	nonWildcardCert, _ = resources.GenerateCertificate("host-1.example.com", "secret0", "istio-system")
	wildcardCert, _    = resources.GenerateCertificate("*.example.com", "secret0", "istio-system")
	selector           = map[string]string{
		"istio": "ingress",
	}
	gwLabels = map[string]string{
		networking.IngressLabelKey: "reconciling-ingress",
	}
	ingressService = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-ingressgateway",
			Namespace: "istio-system",
		},
		Spec: corev1.ServiceSpec{
			Selector: selector,
		},
	}
	wildcardTLSServer = &istiov1alpha3.Server{
		Hosts: []string{"*.example.com"},
		Port: &istiov1alpha3.Port{
			Name:     "https",
			Number:   443,
			Protocol: "HTTPS",
		},
		Tls: &istiov1alpha3.ServerTLSSettings{
			Mode:              istiov1alpha3.ServerTLSSettings_SIMPLE,
			ServerCertificate: corev1.TLSCertKey,
			PrivateKey:        corev1.TLSPrivateKeyKey,
			CredentialName:    "secret0",
		},
	}
	originGateways = map[string]string{
		"gateway.knative-test-gateway": originDomainInternal,
	}
	newGateways = map[string]string{
		"gateway." + config.KnativeIngressGateway: newDomainInternal,
		"gateway.knative-test-gateway":            originDomainInternal,
	}
	ingressGateway = map[v1alpha1.IngressVisibility]sets.String{
		v1alpha1.IngressVisibilityExternalIP: sets.NewString(config.KnativeIngressGateway),
	}
	gateways = map[v1alpha1.IngressVisibility]sets.String{
		v1alpha1.IngressVisibilityExternalIP: sets.NewString("knative-test-gateway", config.KnativeIngressGateway),
	}
	perIngressGatewayName = resources.GatewayName(ingressWithTLS("reconciling-ingress", 1234, ingressTLS), ingressService)
)

var (
	ingressRules = []v1alpha1.IngressRule{{
		Hosts: []string{
			"host-tls.example.com",
			"host-tls.test-ns.svc.cluster.local",
		},
		HTTP: &v1alpha1.HTTPIngressRuleValue{
			Paths: []v1alpha1.HTTPIngressPath{{
				Splits: []v1alpha1.IngressBackendSplit{{
					IngressBackend: v1alpha1.IngressBackend{
						ServiceNamespace: testNS,
						ServiceName:      "test-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
				Timeout: &metav1.Duration{Duration: time.Duration(90) * time.Second}},
			},
		},
		Visibility: v1alpha1.IngressVisibilityExternalIP,
	}}

	ingressTLS = []v1alpha1.IngressTLS{{
		Hosts:           []string{"host-tls.example.com"},
		SecretName:      "secret0",
		SecretNamespace: "istio-system",
	}}

	// The gateway server according to ingressTLS.
	ingressTLSServer = &istiov1alpha3.Server{
		Hosts: []string{"host-tls.example.com"},
		Port: &istiov1alpha3.Port{
			Name:     "test-ns/reconciling-ingress:0",
			Number:   443,
			Protocol: "HTTPS",
		},
		Tls: &istiov1alpha3.ServerTLSSettings{
			Mode:              istiov1alpha3.ServerTLSSettings_SIMPLE,
			ServerCertificate: "tls.crt",
			PrivateKey:        "tls.key",
			CredentialName:    "secret0",
		},
	}

	ingressHTTPRedirectServer = &istiov1alpha3.Server{
		Hosts: []string{"*"},
		Port: &istiov1alpha3.Port{
			Name:     "http-server",
			Number:   80,
			Protocol: "HTTP",
		},
		Tls: &istiov1alpha3.ServerTLSSettings{
			HttpsRedirect: true,
		},
	}

	// The gateway server irrelevant to ingressTLS.
	irrelevantServer = &istiov1alpha3.Server{
		Hosts: []string{"host-tls.example.com", "host-tls.test-ns.svc.cluster.local"},
		Port: &istiov1alpha3.Port{
			Name:     "test:0",
			Number:   443,
			Protocol: "HTTPS",
		},
		Tls: &istiov1alpha3.ServerTLSSettings{
			Mode:              istiov1alpha3.ServerTLSSettings_SIMPLE,
			ServerCertificate: "tls.crt",
			PrivateKey:        "tls.key",
			CredentialName:    "other-secret",
		},
	}
	irrelevantServer1 = &istiov1alpha3.Server{
		Hosts: []string{"*"},
		Port: &istiov1alpha3.Port{
			Name:     "http-server",
			Number:   80,
			Protocol: "HTTP",
		},
	}

	deletionTime = metav1.NewTime(time.Unix(1e9, 0))
)

func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name: "bad workqueue key",
		Key:  "too/many/parts",
	}, {
		Name: "key not found",
		Key:  "foo/not-found",
	}, {
		Name: "skip ingress not matching class key",
		Objects: []runtime.Object{
			addAnnotations(ing("no-virtualservice-yet", 1234),
				map[string]string{networking.IngressClassAnnotationKey: "fake-controller"}),
		},
	}, {
		Name:    "observed generation is updated when error is encountered in reconciling, and ingress ready status is unknown",
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "virtualservices"),
		},
		Objects: []runtime.Object{
			ingressWithStatus("reconcile-failed", 1234,
				v1alpha1.IngressStatus{
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
							Type:   v1alpha1.IngressConditionLoadBalancerReady,
							Status: corev1.ConditionTrue,
						}, {
							Type:   v1alpha1.IngressConditionNetworkConfigured,
							Status: corev1.ConditionTrue,
						}, {
							Type:   v1alpha1.IngressConditionReady,
							Status: corev1.ConditionTrue,
						}},
					},
				},
			),
			&v1alpha3.VirtualService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "reconcile-failed-ingress",
					Namespace: testNS,
					Labels: map[string]string{
						networking.IngressLabelKey: "reconcile-failed",
					},
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing("reconcile-failed", 1234))},
				},
				Spec: istiov1alpha3.VirtualService{},
			},
		},
		WantCreates: []runtime.Object{
			resources.MakeMeshVirtualService(context.Background(), insertProbe(ing("reconcile-failed", 1234)), gateways),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: resources.MakeIngressVirtualService(context.Background(), insertProbe(ing("reconcile-failed", 1234)),
				makeGatewayMap([]string{"knative-testing/knative-test-gateway", "knative-testing/" + config.KnativeIngressGateway}, nil)),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithStatus("reconcile-failed", 1234,
				v1alpha1.IngressStatus{
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Reason:   virtualServiceNotReconciled,
							Severity: apis.ConditionSeverityError,
							Message:  "failed to update VirtualService: inducing failure for update virtualservices",
							Status:   corev1.ConditionFalse,
						}, {
							Type:   v1alpha1.IngressConditionNetworkConfigured,
							Status: corev1.ConditionTrue,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionFalse,
							Severity: apis.ConditionSeverityError,
							Reason:   notReconciledReason,
							Message:  notReconciledMessage,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "reconcile-failed"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconcile-failed-mesh"),
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to update VirtualService: inducing failure for update virtualservices"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconcile-failed", "ingresses.networking.internal.knative.dev"),
		},
		Key: "test-ns/reconcile-failed",
	}, {
		Name: "reconcile VirtualService to match desired one",
		Objects: []runtime.Object{
			ing("reconcile-virtualservice", 1234),
			gateway("knative-ingress-gateway", system.Namespace(), []*istiov1alpha3.Server{irrelevantServer1}),
			gateway("knative-test-gateway", system.Namespace(), []*istiov1alpha3.Server{irrelevantServer1}),
			&v1alpha3.VirtualService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "reconcile-virtualservice-ingress",
					Namespace: testNS,
					Labels: map[string]string{
						networking.IngressLabelKey: "reconcile-virtualservice",
					},
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing("reconcile-virtualservice", 1234))},
				},
				Spec: istiov1alpha3.VirtualService{},
			},
			&v1alpha3.VirtualService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "reconcile-virtualservice-extra",
					Namespace: testNS,
					Labels: map[string]string{
						networking.IngressLabelKey: "reconcile-virtualservice",
					},
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing("reconcile-virtualservice", 1234))},
				},
				Spec: istiov1alpha3.VirtualService{},
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: resources.MakeIngressVirtualService(context.Background(), insertProbe(ing("reconcile-virtualservice", 1234)),
				makeGatewayMap([]string{"knative-testing/knative-test-gateway", "knative-testing/" + config.KnativeIngressGateway}, nil)),
		}},
		WantCreates: []runtime.Object{
			resources.MakeMeshVirtualService(context.Background(), insertProbe(ing("reconcile-virtualservice", 1234)), gateways),
		},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: testNS,
				Verb:      "delete",
				Resource:  v1alpha3.SchemeGroupVersion.WithResource("virtualservices"),
			},
			Name: "reconcile-virtualservice-extra",
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithStatus("reconcile-virtualservice", 1234,
				v1alpha1.IngressStatus{
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("test-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "reconcile-virtualservice"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconcile-virtualservice-mesh"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated VirtualService %s/%s",
				"test-ns", "reconcile-virtualservice-ingress"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconcile-virtualservice", "ingresses.networking.internal.knative.dev"),
		},
		Key: "test-ns/reconcile-virtualservice",
	}, {}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		r := &Reconciler{
			kubeclient:           kubeclient.Get(ctx),
			istioClientSet:       istioclient.Get(ctx),
			virtualServiceLister: listers.GetVirtualServiceLister(),
			gatewayLister:        listers.GetGatewayLister(),
			statusManager: &fakeStatusManager{
				FakeIsReady: func(ctx context.Context, ing *v1alpha1.Ingress) (bool, error) {
					return true, nil
				},
			},
		}

		return ingressreconciler.NewReconciler(ctx, logging.FromContext(ctx), fakenetworkingclient.Get(ctx),
			listers.GetIngressLister(), controller.GetEventRecorder(ctx), r, network.IstioIngressClassName, controller.Options{
				ConfigStore: &testConfigStore{
					config: ReconcilerTestConfig(),
				}})
	}))
}

func TestReconcile_EnableAutoTLS(t *testing.T) {
	table := TableTest{{
		Name:                    "create Ingress Gateway to match newly created Ingress",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-ingress", 1234, ingressTLS),
			// No Gateway servers match the given TLS of Ingress.
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),
			originSecret("istio-system", "secret0"),
			ingressService,
		},
		WantCreates: []runtime.Object{
			// The creation of default global Gateway is triggered when setting up the test.
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),

			// The newly created per-Ingress Gateway.
			gateway(perIngressGatewayName, testNS, []*istiov1alpha3.Server{ingressTLSServer},
				withOwnerRef(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)),
				withLabels(gwLabels), withSelector(selector)),
			resources.MakeMeshVirtualService(context.Background(), insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)), ingressGateway),
			resources.MakeIngressVirtualService(context.Background(), insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)),
				makeGatewayMap([]string{"knative-testing/" + config.KnativeIngressGateway, "test-ns/" + perIngressGatewayName}, nil)),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ingressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-ingress", 1234,
				ingressTLS,
				v1alpha1.IngressStatus{
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "reconciling-ingress"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress-mesh"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress-ingress"),
		},
		Key: "test-ns/reconciling-ingress",
	}, {
		Name:                    "Update Ingress Gateway to match Ingress",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-ingress", 1234, ingressTLS),
			// The default global Gateway.
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),
			// The existing Ingress gateway does not have HTTPS server.
			gateway(perIngressGatewayName, testNS,
				[]*istiov1alpha3.Server{}, withOwnerRef(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)),
				withLabels(gwLabels), withSelector(selector)),
			originSecret("istio-system", "secret0"),
			ingressService,
		},
		WantCreates: []runtime.Object{
			// The creation of default global Gateway is triggered when setting up the test.
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),
			gateway(perIngressGatewayName, testNS,
				[]*istiov1alpha3.Server{}, withOwnerRef(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)),
				withLabels(gwLabels), withSelector(selector)),

			resources.MakeMeshVirtualService(context.Background(), insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)), ingressGateway),
			resources.MakeIngressVirtualService(context.Background(), insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)),
				makeGatewayMap([]string{"knative-testing/" + config.KnativeIngressGateway, "test-ns/" + perIngressGatewayName}, nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: gateway(perIngressGatewayName, testNS,
				[]*istiov1alpha3.Server{ingressTLSServer}, withOwnerRef(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)),
				withLabels(gwLabels), withSelector(selector)),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ingressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-ingress", 1234,
				ingressTLS,
				v1alpha1.IngressStatus{
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "reconciling-ingress"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress-mesh"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress-ingress"),
		},
		Key: "test-ns/reconciling-ingress",
	}, {
		Name:                    "new Ingress using wildcard certificate",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-ingress", 1234, ingressTLS),
			// No Gateway servers match the given TLS of Ingress.
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),
			wildcardCert,
			ingressService,
		},
		WantCreates: []runtime.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),
			wildcardGateway(resources.WildcardGatewayName(wildcardCert.Name, ingressService.Namespace, ingressService.Name), "istio-system",
				[]*istiov1alpha3.Server{wildcardTLSServer}, selector),

			resources.MakeMeshVirtualService(context.Background(), insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)), ingressGateway),
			resources.MakeIngressVirtualService(context.Background(), insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)),
				makeGatewayMap([]string{"knative-testing/" + config.KnativeIngressGateway,
					"istio-system/" + resources.WildcardGatewayName(wildcardCert.Name, ingressService.Namespace, ingressService.Name)}, nil)),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ingressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-ingress", 1234,
				ingressTLS,
				v1alpha1.IngressStatus{
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "reconciling-ingress"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress-mesh"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress-ingress"),
		},
		Key: "test-ns/reconciling-ingress",
	}, {
		Name: "No preinstalled Ingress service",
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-ingress", 1234, ingressTLS),
			originSecret("istio-system", "secret0"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ingressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-ingress", 1234,
				ingressTLS,
				v1alpha1.IngressStatus{
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Status:   corev1.ConditionUnknown,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionNetworkConfigured,
							Status:   corev1.ConditionUnknown,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionUnknown,
							Severity: apis.ConditionSeverityError,
							Reason:   notReconciledReason,
							Message:  notReconciledMessage,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "reconciling-ingress"),
			Eventf(corev1.EventTypeWarning, "InternalError", "service \"istio-ingressgateway\" not found"),
		},
		// Error should be returned when there is no preinstalled gateways.
		WantErr: true,
		Key:     "test-ns/reconciling-ingress",
	}, {
		Name:                    "delete Ingress",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithFinalizers("reconciling-ingress", 1234, ingressTLS, []string{ingressFinalizer}, &deletionTime),
			// ingressHTTPRedirectServer should not be deleted when deleting ingress related TLS server..
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer, ingressTLSServer, ingressHTTPRedirectServer}),
		},
		WantCreates: []runtime.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer, ingressTLSServer, ingressHTTPRedirectServer}),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{ingressHTTPRedirectServer, irrelevantServer}),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ""),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Gateway %s/%s", system.Namespace(), config.KnativeIngressGateway),
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "reconciling-ingress"),
		},
		Key: "test-ns/reconciling-ingress",
	}, {
		Name:                    "delete ingress with leftover secrets",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithFinalizers("reconciling-ingress", 1234, ingressTLS, []string{ingressFinalizer}, &deletionTime),
			// ingressHTTPRedirectServer should not be deleted when deleting ingress related TLS server..
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer, ingressTLSServer, ingressHTTPRedirectServer}),
			targetSecret("istio-system", "targetSecret", resources.MakeTargetSecretLabels("secret0", "istio-system")),
		},
		WantCreates: []runtime.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer, ingressTLSServer, ingressHTTPRedirectServer}),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{ingressHTTPRedirectServer, irrelevantServer}),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ""),
		},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: "istio-system",
				Verb:      "delete",
				Resource:  corev1.SchemeGroupVersion.WithResource("secrets"),
			},
			Name: "targetSecret",
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Gateway %s/%s", system.Namespace(), config.KnativeIngressGateway),
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "reconciling-ingress"),
		},
		Key: "test-ns/reconciling-ingress",
	}, {
		Name:                    "TLS Secret is not in the namespace of Istio gateway service",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-ingress", 1234, ingressTLSWithSecretNamespace("knative-serving")),
			// No Gateway servers match the given TLS of Ingress.
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),
			// The namespace (`knative-serving`) of the origin secret is different
			// from the namespace (`istio-system`) of Istio gateway service.
			originSecret("knative-serving", "secret0"),
			ingressService,
		},
		WantCreates: []runtime.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),

			// The newly created per-Ingress Gateway.
			gateway(perIngressGatewayName, testNS,
				[]*istiov1alpha3.Server{withCredentialName(deepCopy(ingressTLSServer), targetSecretName)},
				withOwnerRef(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)),
				withLabels(gwLabels), withSelector(selector)),

			resources.MakeMeshVirtualService(context.Background(), insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLSWithSecretNamespace("knative-serving"))), ingressGateway),
			resources.MakeIngressVirtualService(context.Background(), insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLSWithSecretNamespace("knative-serving"))),
				makeGatewayMap([]string{"knative-testing/" + config.KnativeIngressGateway, "test-ns/" + perIngressGatewayName}, nil)),

			// The secret copy under istio-system.
			targetSecret("istio-system", targetSecretName, map[string]string{
				networking.OriginSecretNameLabelKey:      "secret0",
				networking.OriginSecretNamespaceLabelKey: "knative-serving",
			}),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ingressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-ingress", 1234,
				ingressTLSWithSecretNamespace("knative-serving"),
				v1alpha1.IngressStatus{
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "reconciling-ingress"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Secret %s/%s", "istio-system", targetSecretName),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress-mesh"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress-ingress"),
		},
		Key: "test-ns/reconciling-ingress",
	}, {
		Name:                    "Reconcile Target secret",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-ingress", 1234, ingressTLSWithSecretNamespace("knative-serving")),

			// The default global Gateway.
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),

			// The newly created per-Ingress Gateway.
			gateway(perIngressGatewayName, testNS,
				[]*istiov1alpha3.Server{withCredentialName(deepCopy(ingressTLSServer), targetSecretName)},
				withOwnerRef(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)),
				withLabels(gwLabels), withSelector(selector)),
			ingressService,

			// The origin secret.
			originSecret("knative-serving", "secret0"),

			// The target secret that has the Data different from the origin secret. The Data should be reconciled.
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      targetSecretName,
					Namespace: "istio-system",
					Labels: map[string]string{
						networking.OriginSecretNameLabelKey:      "secret0",
						networking.OriginSecretNamespaceLabelKey: "knative-serving",
					},
				},
				Data: map[string][]byte{
					"wrong_data": []byte("wrongdata"),
				},
			},
		},
		WantCreates: []runtime.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),
			gateway(perIngressGatewayName, testNS,
				[]*istiov1alpha3.Server{withCredentialName(deepCopy(ingressTLSServer), targetSecretName)},
				withOwnerRef(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)),
				withLabels(gwLabels), withSelector(selector)),

			resources.MakeMeshVirtualService(context.Background(), insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLSWithSecretNamespace("knative-serving"))), ingressGateway),
			resources.MakeIngressVirtualService(context.Background(), insertProbe(ingressWithTLS("reconciling-ingress", 1234, ingressTLSWithSecretNamespace("knative-serving"))),
				makeGatewayMap([]string{"knative-testing/" + config.KnativeIngressGateway, "test-ns/" + perIngressGatewayName}, nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      targetSecretName,
					Namespace: "istio-system",
					Labels: map[string]string{
						networking.OriginSecretNameLabelKey:      "secret0",
						networking.OriginSecretNamespaceLabelKey: "knative-serving",
					},
				},
				// The data is expected to be updated to the right one.
				Data: nonWildcardCert.Data,
			},
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ingressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-ingress", 1234,
				ingressTLSWithSecretNamespace("knative-serving"),
				v1alpha1.IngressStatus{
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "reconciling-ingress"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Secret %s/%s", "istio-system", targetSecretName),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress-mesh"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress-ingress"),
		},
		Key: "test-ns/reconciling-ingress",
	}, {
		Name:                    "Reconcile with autoTLS but cluster local visibilty, mesh only",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLSClusterLocal("reconciling-ingress", 1234, ingressTLS),
			// No Gateway servers match the given TLS of Ingress.
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),
			originSecret("istio-system", "secret0"),
		},
		WantCreates: []runtime.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1alpha3.Server{irrelevantServer}),
			resources.MakeMeshVirtualService(context.Background(), insertProbe(ingressWithTLSClusterLocal("reconciling-ingress", 1234, ingressTLS)), ingressGateway),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatusClusterLocal("reconciling-ingress", 1234,
				ingressTLS,
				v1alpha1.IngressStatus{
					PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system")},
						},
					},
					PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{MeshOnly: true},
						},
					},
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
							Type:     v1alpha1.IngressConditionLoadBalancerReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionNetworkConfigured,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}, {
							Type:     v1alpha1.IngressConditionReady,
							Status:   corev1.ConditionTrue,
							Severity: apis.ConditionSeverityError,
						}},
					},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "reconciling-ingress"),
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "reconciling-ingress-mesh"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ingressFinalizer),
		},
		Key: "test-ns/reconciling-ingress",
	}}
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {

		// As we use a customized resource name for Gateway CRD (i.e. `gateways`), not the one
		// originally generated by kubernetes code generator (i.e. `gatewaies`), we have to
		// explicitly create gateways when setting up the test per suggestion
		// https://github.com/knative/serving/blob/a6852fc3b6cdce72b99c5d578dd64f2e03dabb8b/vendor/k8s.io/client-go/testing/fixture.go#L292
		gateways := getGatewaysFromObjects(listers.GetIstioObjects())
		for _, gateway := range gateways {
			fakeistioclient.Get(ctx).NetworkingV1alpha3().Gateways(gateway.Namespace).Create(ctx, gateway, metav1.CreateOptions{})
		}

		r := &Reconciler{
			kubeclient:           kubeclient.Get(ctx),
			istioClientSet:       istioclient.Get(ctx),
			virtualServiceLister: listers.GetVirtualServiceLister(),
			gatewayLister:        listers.GetGatewayLister(),
			secretLister:         listers.GetSecretLister(),
			svcLister:            listers.GetK8sServiceLister(),
			tracker:              &NullTracker{},
			statusManager: &fakeStatusManager{
				FakeIsReady: func(ctx context.Context, ing *v1alpha1.Ingress) (bool, error) {
					return true, nil
				},
			},
		}

		return ingressreconciler.NewReconciler(ctx, logging.FromContext(ctx), fakenetworkingclient.Get(ctx),
			listers.GetIngressLister(), controller.GetEventRecorder(ctx), r, network.IstioIngressClassName, controller.Options{
				ConfigStore: &testConfigStore{
					// Enable reconciling gateway.
					config: &config.Config{
						Istio: &config.Istio{
							IngressGateways: []config.Gateway{{
								Namespace:  system.Namespace(),
								Name:       config.KnativeIngressGateway,
								ServiceURL: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system"),
							}},
						},
						Network: &network.Config{
							HTTPProtocol: network.HTTPDisabled,
							AutoTLS:      true,
						},
					},
				},
			})
	}))
}

func getGatewaysFromObjects(objects []runtime.Object) []*v1alpha3.Gateway {
	gateways := []*v1alpha3.Gateway{}
	for _, object := range objects {
		if gateway, ok := object.(*v1alpha3.Gateway); ok {
			gateways = append(gateways, gateway)
		}
	}
	sort.Slice(gateways, func(i, j int) bool {
		return strings.Compare(gateways[i].Name, gateways[j].Name) == -1
	})
	return gateways
}

type GatewayOpt func(*v1alpha3.Gateway)

func gateway(name, namespace string, servers []*istiov1alpha3.Server, opts ...GatewayOpt) *v1alpha3.Gateway {
	gw := &v1alpha3.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: istiov1alpha3.Gateway{
			Servers: servers,
		},
	}
	for _, opt := range opts {
		opt(gw)
	}
	return gw
}

func withOwnerRef(ing *v1alpha1.Ingress) GatewayOpt {
	return func(gw *v1alpha3.Gateway) {
		gw.OwnerReferences = []metav1.OwnerReference{*kmeta.NewControllerRef(ing)}
	}
}

func withLabels(labels map[string]string) GatewayOpt {
	return func(gw *v1alpha3.Gateway) {
		gw.Labels = labels
	}
}

func withSelector(selector map[string]string) GatewayOpt {
	return func(gw *v1alpha3.Gateway) {
		gw.Spec.Selector = selector
	}
}

func wildcardGateway(name, namespace string, servers []*istiov1alpha3.Server, selector map[string]string) *v1alpha3.Gateway {
	gw := gateway(name, namespace, servers)
	gvk := schema.GroupVersionKind{Version: "v1", Kind: "Secret"}
	gw.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(wildcardCert, gvk)}
	gw.Spec.Selector = selector
	return gw
}

func originSecret(namespace, name string) *corev1.Secret {
	tmp := secret(namespace, name, map[string]string{})
	tmp.UID = "uid"
	return tmp
}

func targetSecret(namespace, name string, labels map[string]string) *corev1.Secret {
	tmp := secret(namespace, name, labels)
	tmp.OwnerReferences = []metav1.OwnerReference{}
	return tmp
}

func secret(namespace, name string, labels map[string]string) *corev1.Secret {
	secret := *nonWildcardCert
	secret.Namespace = namespace
	secret.Name = name
	secret.OwnerReferences = []metav1.OwnerReference{*kmeta.NewControllerRef(ing("reconciling-ingress", 1234))}
	secret.Labels = labels
	return &secret
}

func withCredentialName(tlsServer *istiov1alpha3.Server, credentialName string) *istiov1alpha3.Server {
	tlsServer.Tls.CredentialName = credentialName
	return tlsServer
}

// Open-coded deepCopy since istio.io/api's Server doesn't provide one currently
func deepCopy(server *istiov1alpha3.Server) *istiov1alpha3.Server {
	return proto.Clone(server).(*istiov1alpha3.Server)
}

func ingressTLSWithSecretNamespace(namespace string) []v1alpha1.IngressTLS {
	result := []v1alpha1.IngressTLS{}
	for _, tls := range ingressTLS {
		tls.SecretNamespace = namespace
		result = append(result, tls)
	}
	return result
}

func getPatchFinalizerAction(finalizer string) string {
	var patch string
	if finalizer != "" {
		patch = fmt.Sprintf(`{"metadata":{"finalizers":[%q],"resourceVersion":"v1"}}`, finalizer)
	} else {
		patch = `{"metadata":{"finalizers":[],"resourceVersion":"v1"}}`
	}
	return patch
}

func patchAddFinalizerAction(ingressName, finalizer string) clientgotesting.PatchActionImpl {
	action := clientgotesting.PatchActionImpl{
		Name: ingressName,
	}
	action.Patch = []byte(getPatchFinalizerAction(finalizer))
	return action
}

func addAnnotations(ing *v1alpha1.Ingress, annos map[string]string) *v1alpha1.Ingress {
	// UnionMaps(a, b) where value from b wins. Use annos for second arg.
	ing.ObjectMeta.Annotations = kmeta.UnionMaps(ing.ObjectMeta.Annotations, annos)
	return ing
}

type testConfigStore struct {
	config *config.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, t.config)
}

var _ pkgreconciler.ConfigStore = (*testConfigStore)(nil)

func ReconcilerTestConfig() *config.Config {
	return &config.Config{
		Istio: &config.Istio{
			IngressGateways: []config.Gateway{{
				Namespace:  system.Namespace(),
				Name:       "knative-test-gateway",
				ServiceURL: pkgnet.GetServiceHostname("test-ingressgateway", "istio-system"),
			}, {
				Namespace:  system.Namespace(),
				Name:       config.KnativeIngressGateway,
				ServiceURL: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system"),
			}},
		},
		Network: &network.Config{
			AutoTLS: false,
		},
	}
}

func ingressWithStatus(name string, generation int64, status v1alpha1.IngressStatus) *v1alpha1.Ingress {
	return &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS,
			Labels: map[string]string{
				resources.RouteLabelKey:          "test-route",
				resources.RouteNamespaceLabelKey: testNS,
			},
			Annotations:     map[string]string{networking.IngressClassAnnotationKey: network.IstioIngressClassName},
			ResourceVersion: "v1",
		},
		Spec: v1alpha1.IngressSpec{
			Rules: ingressRules,
		},
		Status: status,
	}
}

func ing(name string, generation int64) *v1alpha1.Ingress {
	return ingressWithStatus(name, generation, v1alpha1.IngressStatus{})
}

func ingressWithFinalizers(name string, generation int64, tls []v1alpha1.IngressTLS, finalizers []string, deletionTime *metav1.Time) *v1alpha1.Ingress {
	ingress := ingressWithTLS(name, generation, tls)
	ingress.ObjectMeta.Finalizers = finalizers
	if deletionTime != nil {
		ingress.ObjectMeta.DeletionTimestamp = deletionTime
	}
	return ingress
}

func ingressWithTLS(name string, generation int64, tls []v1alpha1.IngressTLS) *v1alpha1.Ingress {
	return ingressWithTLSAndStatus(name, generation, tls, v1alpha1.IngressStatus{})
}

func ingressWithTLSClusterLocal(name string, generation int64, tls []v1alpha1.IngressTLS) *v1alpha1.Ingress {
	ci := ingressWithTLSAndStatus(name, generation, tls, v1alpha1.IngressStatus{}).DeepCopy()
	rules := ci.Spec.Rules
	for i, rule := range rules {
		rCopy := rule.DeepCopy()
		rCopy.Visibility = v1alpha1.IngressVisibilityClusterLocal
		rules[i] = *rCopy
	}

	ci.Spec.Rules = rules

	return ci
}

func ingressWithTLSAndStatus(name string, generation int64, tls []v1alpha1.IngressTLS, status v1alpha1.IngressStatus) *v1alpha1.Ingress {
	ci := ingressWithStatus(name, generation, status)
	ci.Spec.TLS = tls
	return ci
}

func ingressWithTLSAndStatusClusterLocal(name string, generation int64, tls []v1alpha1.IngressTLS, status v1alpha1.IngressStatus) *v1alpha1.Ingress {
	ci := ingressWithTLSClusterLocal(name, generation, tls)
	ci.Status = status
	return ci
}

func newTestSetup(t *testing.T, configs ...*corev1.ConfigMap) (
	context.Context,
	context.CancelFunc,
	[]controller.Informer,
	*controller.Impl,
	*configmap.ManualWatcher) {

	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	configMapWatcher := &configmap.ManualWatcher{Namespace: system.Namespace()}

	controller := newControllerWithOptions(ctx,
		configMapWatcher,
		func(r *Reconciler) {
			r.statusManager = &fakeStatusManager{
				FakeIsReady: func(ctx context.Context, ing *v1alpha1.Ingress) (bool, error) {
					return true, nil
				},
			}
		})

	cms := append([]*corev1.ConfigMap{{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.IstioConfigName,
			Namespace: system.Namespace(),
		},
		Data: originGateways,
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name:      network.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"autoTLS": "Disabled",
		},
	}}, configs...)

	for _, cfg := range cms {
		configMapWatcher.OnChange(cfg)
	}

	return ctx, cancel, informers, controller, configMapWatcher
}

func TestGlobalResyncOnUpdateGatewayConfigMap(t *testing.T) {
	ctx, cancel, informers, ctrl, watcher := newTestSetup(t)

	grp := errgroup.Group{}

	networkingclient := fakenetworkingclient.Get(ctx)

	h := NewHooks()

	// Check for Ingress created as a signal that syncHandler ran
	h.OnUpdate(&networkingclient.Fake, "ingresses", func(obj runtime.Object) HookResult {
		ci := obj.(*v1alpha1.Ingress)
		t.Logf("Ingress updated: %q", ci.Name)

		gateways := ci.Status.PublicLoadBalancer.Ingress
		if len(gateways) != 1 {
			t.Log("Unexpected gateways:", gateways)
			return HookIncomplete
		}
		if got, want := gateways[0].DomainInternal, newDomainInternal; got != want {
			t.Logf("Gateway = %q, want: %q", got, want)
			return HookIncomplete
		}

		return HookComplete
	})

	waitInformers, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}
	defer func() {
		cancel()
		if err := grp.Wait(); err != nil {
			t.Error("Wait() =", err)
		}
		waitInformers()
	}()

	if err := watcher.Start(ctx.Done()); err != nil {
		t.Fatal("Failed to start ingress manager:", err)
	}
	grp.Go(func() error { return ctrl.Run(1, ctx.Done()) })

	ingress := ingressWithStatus("config-update", 1234,
		v1alpha1.IngressStatus{
			PublicLoadBalancer: &v1alpha1.LoadBalancerStatus{
				Ingress: []v1alpha1.LoadBalancerIngressStatus{
					{DomainInternal: ""},
				},
			},
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   v1alpha1.IngressConditionLoadBalancerReady,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.IngressConditionNetworkConfigured,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.IngressConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
	)
	ingressClient := networkingclient.NetworkingV1alpha1().Ingresses(testNS)

	// Create a ingress.
	ingressClient.Create(ctx, ingress, metav1.CreateOptions{})
	il := fakeingressclient.Get(ctx).Lister()
	if err := wait.PollImmediate(10*time.Millisecond, 5*time.Second, func() (bool, error) {
		l, err := il.List(labels.Everything())
		if err != nil {
			return false, err
		}
		// We only create a single ingress.
		return len(l) > 0, nil

	}); err != nil {
		t.Fatal("Failed to see ingress propagation:", err)
	}

	// Test changes in gateway config map. Ingress should get updated appropriately.
	domainConfig := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.IstioConfigName,
			Namespace: system.Namespace(),
		},
		Data: newGateways,
	}
	watcher.OnChange(&domainConfig)

	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error(err)
	}
}

func insertProbe(ing *v1alpha1.Ingress) *v1alpha1.Ingress {
	ing = ing.DeepCopy()
	ingress.InsertProbe(ing)
	return ing
}

func TestGlobalResyncOnUpdateNetwork(t *testing.T) {
	ctx, cancel, informers, ctrl, watcher := newTestSetup(t)

	grp := errgroup.Group{}

	istioClient := fakeistioclient.Get(ctx)

	h := NewHooks()

	// Check for Gateway created as a signal that syncHandler ran
	h.OnUpdate(&istioClient.Fake, "gateways", func(obj runtime.Object) HookResult {
		createdGateway := obj.(*v1alpha3.Gateway)
		// The expected gateway should include the Istio TLS server.
		expectedGateway := gateway(perIngressGatewayName, testNS,
			[]*istiov1alpha3.Server{ingressTLSServer}, withOwnerRef(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)),
			withLabels(gwLabels), withSelector(selector))
		if diff := cmp.Diff(createdGateway, expectedGateway); diff != "" {
			t.Log("Unexpected Gateway (-want, +got):", diff)
			return HookIncomplete
		}

		return HookComplete
	})

	waitInformers, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		t.Fatal("Failed to start ingress manager:", err)
	}
	defer func() {
		cancel()
		if err := grp.Wait(); err != nil {
			t.Error("Wait() =", err)
		}
		waitInformers()
	}()

	if err := watcher.Start(ctx.Done()); err != nil {
		t.Fatal("Failed to start watcher:", err)
	}

	grp.Go(func() error { return ctrl.Run(1, ctx.Done()) })

	ingress := ingressWithTLSAndStatus("reconciling-ingress", 1234,
		ingressTLS,
		v1alpha1.IngressStatus{
			PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{
				Ingress: []v1alpha1.LoadBalancerIngressStatus{
					{DomainInternal: originDomainInternal},
				},
			},
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   v1alpha1.IngressConditionLoadBalancerReady,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.IngressConditionNetworkConfigured,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.IngressConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
	)

	// Create a ingress.
	ingressClient := fakenetworkingclient.Get(ctx).NetworkingV1alpha1().Ingresses(testNS)
	ingressClient.Create(ctx, ingress, metav1.CreateOptions{})

	gatewayClient := istioClient.NetworkingV1alpha3().Gateways(system.Namespace())
	// Create a Gateway
	if _, err := gatewayClient.Create(ctx, gateway("knative-test-gateway", system.Namespace(), []*istiov1alpha3.Server{}), metav1.CreateOptions{}); err != nil {
		t.Fatal("Error creating gateway:", err)
	}

	// Create an Ingress gateway
	ingressGatewayClient := istioClient.NetworkingV1alpha3().Gateways(testNS)
	ingressGateway := gateway(perIngressGatewayName, testNS,
		[]*istiov1alpha3.Server{}, withOwnerRef(ingressWithTLS("reconciling-ingress", 1234, ingressTLS)),
		withLabels(gwLabels), withSelector(selector))
	if _, err := ingressGatewayClient.Create(ctx, ingressGateway, metav1.CreateOptions{}); err != nil {
		t.Fatal("Error creating gateway:", err)
	}

	// Create origin secret. "ns" namespace is the namespace of ingress gateway service.
	secretClient := fakekubeclient.Get(ctx).CoreV1().Secrets("istio-system")
	if _, err := secretClient.Create(ctx, nonWildcardCert, metav1.CreateOptions{}); err != nil {
		t.Fatal("Error creating secret:", err)
	}

	// Create ingress service.
	serviceClient := fakekubeclient.Get(ctx).CoreV1().Services("istio-system")
	if _, err := serviceClient.Create(ctx, ingressService, metav1.CreateOptions{}); err != nil {
		t.Fatal("Error creating service:", err)
	}

	// Test changes in autoTLS of config-network ConfigMap. Ingress should get updated appropriately.
	networkConfig := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      network.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"autoTLS":      "Enabled",
			"httpProtocol": "Redirected",
		},
	}
	watcher.OnChange(&networkConfig)

	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error(err)
	}
}

func makeGatewayMap(publicGateways []string, privateGateways []string) map[v1alpha1.IngressVisibility]sets.String {
	return map[v1alpha1.IngressVisibility]sets.String{
		v1alpha1.IngressVisibilityExternalIP:   sets.NewString(publicGateways...),
		v1alpha1.IngressVisibilityClusterLocal: sets.NewString(privateGateways...),
	}
}

type fakeStatusManager struct {
	FakeIsReady func(ctx context.Context, ing *v1alpha1.Ingress) (bool, error)
}

func (m *fakeStatusManager) IsReady(ctx context.Context, ing *v1alpha1.Ingress) (bool, error) {
	return m.FakeIsReady(ctx, ing)
}
