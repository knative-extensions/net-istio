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
	_ "knative.dev/net-istio/pkg/client/istio/injection/informers/networking/v1beta1/gateway/fake"
	_ "knative.dev/net-istio/pkg/client/istio/injection/informers/networking/v1beta1/virtualservice/fake"
	fakenetworkingclient "knative.dev/networking/pkg/client/injection/client/fake"
	fakeingressclient "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/ingress/fake"
	"knative.dev/networking/pkg/ingress"
	"knative.dev/networking/pkg/status"
	fakestatusmanager "knative.dev/networking/pkg/testing/status"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/pod/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/secret/filtered/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	filteredFactory "knative.dev/pkg/client/injection/kube/informers/factory/filtered"
	_ "knative.dev/pkg/client/injection/kube/informers/factory/filtered/fake"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	istiov1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgotesting "k8s.io/client-go/testing"

	"knative.dev/net-istio/pkg/reconciler/ingress/config"
	"knative.dev/net-istio/pkg/reconciler/ingress/resources"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	ingressreconciler "knative.dev/networking/pkg/client/injection/reconciler/networking/v1alpha1/ingress"
	netconfig "knative.dev/networking/pkg/config"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	pkgnet "knative.dev/pkg/network"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/pkg/system"

	. "knative.dev/net-istio/pkg/reconciler/testing"
	. "knative.dev/pkg/reconciler/testing"
	_ "knative.dev/pkg/system/testing"
)

const (
	originDomainInternal = "istio-ingressgateway.istio-system.svc.cluster.local"
	newDomainInternal    = "custom.istio-system.svc.cluster.local"
	targetSecretName     = "reconciling-ingress-uid"
	testNS               = "test-ns"
)

// ingressfinalizer is the name that we put into the resource finalizer list, e.g.
//
//	metadata:
//	  finalizers:
//	  - ingresses.networking.internal.knative.dev
var (
	ingressResource  = v1alpha1.Resource("ingresses")
	ingressFinalizer = ingressResource.String()
)

var (
	nonWildcardCert, _ = resources.GenerateCertificate([]string{"host-1.example.com"}, "secret0", "istio-system")
	wildcardCert, _    = resources.GenerateCertificate([]string{"*.example.com"}, "secret0", "istio-system")
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
	wildcardTLSServer = &istiov1beta1.Server{
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
			CredentialName:     "secret0",
			MinProtocolVersion: istiov1beta1.ServerTLSSettings_TLSV1_2,
		},
	}
	originGateways = map[string]string{
		"gateway.knative-test-gateway": originDomainInternal,
	}
	newGateways = map[string]string{
		"gateway." + config.KnativeIngressGateway: newDomainInternal,
		"gateway.knative-test-gateway":            originDomainInternal,
	}
	ingressGateway = map[v1alpha1.IngressVisibility]sets.Set[string]{
		v1alpha1.IngressVisibilityExternalIP: sets.New(config.KnativeIngressGateway),
	}
	gateways = map[v1alpha1.IngressVisibility]sets.Set[string]{
		v1alpha1.IngressVisibilityExternalIP: sets.New("knative-test-gateway", config.KnativeIngressGateway),
	}
	perIngressGatewayName = resources.GatewayName(ingressWithTLS("reconciling-ingress", ingressTLS), ingressService)
)

var (
	ingressRules = []v1alpha1.IngressRule{{
		Hosts: []string{
			"host-tls.example.com",
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
			}},
		},
		Visibility: v1alpha1.IngressVisibilityExternalIP,
	}, {
		Hosts: []string{
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
			}},
		},
		Visibility: v1alpha1.IngressVisibilityClusterLocal,
	}}

	ingressTLS = []v1alpha1.IngressTLS{{
		Hosts:           []string{"host-tls.example.com"},
		SecretName:      "secret0",
		SecretNamespace: "istio-system",
	}}

	// The gateway server according to ingressTLS.
	ingressTLSServer = &istiov1beta1.Server{
		Hosts: []string{"host-tls.example.com"},
		Port: &istiov1beta1.Port{
			Name:     "test-ns/reconciling-ingress:0",
			Number:   443,
			Protocol: "HTTPS",
		},
		Tls: &istiov1beta1.ServerTLSSettings{
			Mode:               istiov1beta1.ServerTLSSettings_SIMPLE,
			ServerCertificate:  "tls.crt",
			PrivateKey:         "tls.key",
			CredentialName:     "secret0",
			MinProtocolVersion: istiov1beta1.ServerTLSSettings_TLSV1_2,
		},
	}

	ingressHTTPServer = &istiov1beta1.Server{
		Hosts: []string{"host-tls.example.com"},
		Port: &istiov1beta1.Port{
			Name:     "http-server",
			Number:   80,
			Protocol: "HTTP",
		},
	}

	ingressHTTPRedirectServer = &istiov1beta1.Server{
		Hosts: []string{"*"},
		Port: &istiov1beta1.Port{
			Name:     "http-server",
			Number:   80,
			Protocol: "HTTP",
		},
		Tls: &istiov1beta1.ServerTLSSettings{
			HttpsRedirect: true,
		},
	}

	// The gateway server irrelevant to ingressTLS.
	irrelevantServer = &istiov1beta1.Server{
		Hosts: []string{"host-tls.example.com", "host-tls.test-ns.svc.cluster.local"},
		Port: &istiov1beta1.Port{
			Name:     "test:0",
			Number:   443,
			Protocol: "HTTPS",
		},
		Tls: &istiov1beta1.ServerTLSSettings{
			Mode:               istiov1beta1.ServerTLSSettings_SIMPLE,
			ServerCertificate:  "tls.crt",
			PrivateKey:         "tls.key",
			CredentialName:     "other-secret",
			MinProtocolVersion: istiov1beta1.ServerTLSSettings_TLSV1_2,
		},
	}
	irrelevantServer1 = &istiov1beta1.Server{
		Hosts: []string{"*"},
		Port: &istiov1beta1.Port{
			Name:     "http-server",
			Number:   80,
			Protocol: "HTTP",
		},
	}

	deletionTime = metav1.NewTime(time.Unix(1e9, 0))

	defaultCmpOptsList = []cmp.Option{protocmp.Transform()}
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
			addAnnotations(ing("no-virtualservice-yet"),
				map[string]string{networking.IngressClassAnnotationKey: "fake-controller"}),
		},
	}, {
		Name:    "observed generation is updated when error is encountered in reconciling, and ingress ready status is unknown",
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "virtualservices"),
		},
		Objects: []runtime.Object{
			ingressWithStatus("reconcile-failed",
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
			&v1beta1.VirtualService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "reconcile-failed-ingress",
					Namespace: testNS,
					Labels: map[string]string{
						networking.IngressLabelKey: "reconcile-failed",
					},
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing("reconcile-failed"))},
				},
				Spec: istiov1beta1.VirtualService{},
			},
		},
		WantCreates: []runtime.Object{
			resources.MakeMeshVirtualService(insertProbe(ing("reconcile-failed")), gateways),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: resources.MakeIngressVirtualService(insertProbe(ing("reconcile-failed")), makeGatewayMap([]string{"knative-testing/knative-test-gateway", "knative-testing/" + config.KnativeIngressGateway}, nil)),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithStatus("reconcile-failed",
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
		Key:     "test-ns/reconcile-failed",
		CmpOpts: defaultCmpOptsList,
	}, {
		Name: "reconcile VirtualService to match desired one",
		Objects: []runtime.Object{
			ing("reconcile-virtualservice"),
			gateway("knative-ingress-gateway", system.Namespace(), []*istiov1beta1.Server{irrelevantServer1}),
			gateway("knative-test-gateway", system.Namespace(), []*istiov1beta1.Server{irrelevantServer1}),
			&v1beta1.VirtualService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "reconcile-virtualservice-ingress",
					Namespace: testNS,
					Labels: map[string]string{
						networking.IngressLabelKey: "reconcile-virtualservice",
					},
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing("reconcile-virtualservice"))},
				},
				Spec: istiov1beta1.VirtualService{},
			},
			&v1beta1.VirtualService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "reconcile-virtualservice-extra",
					Namespace: testNS,
					Labels: map[string]string{
						networking.IngressLabelKey: "reconcile-virtualservice",
					},
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing("reconcile-virtualservice"))},
				},
				Spec: istiov1beta1.VirtualService{},
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: resources.MakeIngressVirtualService(insertProbe(ing("reconcile-virtualservice")), makeGatewayMap([]string{"knative-testing/knative-test-gateway", "knative-testing/" + config.KnativeIngressGateway}, nil)),
		}},
		WantCreates: []runtime.Object{
			resources.MakeMeshVirtualService(insertProbe(ing("reconcile-virtualservice")), gateways),
		},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: testNS,
				Verb:      "delete",
				Resource:  v1beta1.SchemeGroupVersion.WithResource("virtualservices"),
			},
			Name: "reconcile-virtualservice-extra",
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithStatus("reconcile-virtualservice",
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
		PostConditions: []func(*testing.T, *TableRow){proberCalledTimes(1)},
		Key:            "test-ns/reconcile-virtualservice",
		CmpOpts:        defaultCmpOptsList,
	}, {
		Name: "if ingress is already ready, we shouldn't call statusManager.IsReady",
		Key:  "test-ns/ingress-ready",
		Objects: []runtime.Object{
			basicReconciledIngress("ingress-ready"),
			resources.MakeMeshVirtualService(insertProbe(ing("ingress-ready")), makeGatewayMap([]string{"knative-testing/knative-test-gateway", "knative-testing/" + config.KnativeIngressGateway}, nil)),
			resources.MakeIngressVirtualService(insertProbe(ing("ingress-ready")), makeGatewayMap([]string{"knative-testing/knative-test-gateway", "knative-testing/" + config.KnativeIngressGateway}, nil)),
		},
		PostConditions: []func(*testing.T, *TableRow){proberCalledTimes(0)},
		CmpOpts:        defaultCmpOptsList,
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		r := &Reconciler{
			kubeclient:           kubeclient.Get(ctx),
			istioClientSet:       istioclient.Get(ctx),
			virtualServiceLister: listers.GetVirtualServiceLister(),
			gatewayLister:        listers.GetGatewayLister(),
			statusManager:        ctx.Value(FakeStatusManagerKey).(status.Manager),
		}

		return ingressreconciler.NewReconciler(ctx, logging.FromContext(ctx), fakenetworkingclient.Get(ctx),
			listers.GetIngressLister(), controller.GetEventRecorder(ctx), r, netconfig.IstioIngressClassName, controller.Options{
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
			ingressWithTLS("reconciling-ingress", ingressTLS),
			originSecret("istio-system", "secret0"),
			ingressService,
		},
		WantCreates: []runtime.Object{
			// The newly created per-Ingress Gateway.
			gateway(perIngressGatewayName, testNS, []*istiov1beta1.Server{ingressTLSServer, ingressHTTPServer},
				withOwnerRef(ingressWithTLS("reconciling-ingress", ingressTLS)),
				withLabels(gwLabels), withSelector(selector)),
			resources.MakeMeshVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", ingressTLS)), ingressGateway),
			resources.MakeIngressVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", ingressTLS)), makeGatewayMap([]string{"test-ns/" + perIngressGatewayName}, nil)),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ingressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-ingress",
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
		Key:     "test-ns/reconciling-ingress",
		CmpOpts: defaultCmpOptsList,
	}, {
		Name:                    "Update Ingress Gateway to match Ingress",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-ingress", ingressTLS),
			// The existing Ingress gateway does not have HTTPS server.
			gateway(perIngressGatewayName, testNS,
				[]*istiov1beta1.Server{}, withOwnerRef(ingressWithTLS("reconciling-ingress", ingressTLS)),
				withLabels(gwLabels), withSelector(selector)),
			originSecret("istio-system", "secret0"),
			ingressService,
		},
		WantCreates: []runtime.Object{
			gateway(perIngressGatewayName, testNS,
				[]*istiov1beta1.Server{}, withOwnerRef(ingressWithTLS("reconciling-ingress", ingressTLS)),
				withLabels(gwLabels), withSelector(selector)),

			resources.MakeMeshVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", ingressTLS)), ingressGateway),
			resources.MakeIngressVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", ingressTLS)), makeGatewayMap([]string{"test-ns/" + perIngressGatewayName}, nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: gateway(perIngressGatewayName, testNS,
				[]*istiov1beta1.Server{ingressTLSServer, ingressHTTPServer}, withOwnerRef(ingressWithTLS("reconciling-ingress", ingressTLS)),
				withLabels(gwLabels), withSelector(selector)),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ingressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-ingress",
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
		Key:     "test-ns/reconciling-ingress",
		CmpOpts: defaultCmpOptsList,
	}, {
		Name:                    "new Ingress using wildcard certificate",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-ingress", ingressTLS),
			wildcardCert,
			ingressService,
		},
		WantCreates: []runtime.Object{
			wildcardGateway(resources.WildcardGatewayName(wildcardCert.Name, ingressService.Namespace, ingressService.Name), "istio-system",
				[]*istiov1beta1.Server{wildcardTLSServer}, selector),
			// The newly created per-Ingress Gateway.
			gateway(perIngressGatewayName, testNS, []*istiov1beta1.Server{ingressHTTPServer},
				withOwnerRef(ingressWithTLS("reconciling-ingress", ingressTLS)),
				withLabels(gwLabels), withSelector(selector)),

			resources.MakeMeshVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", ingressTLS)), ingressGateway),
			resources.MakeIngressVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", ingressTLS)), makeGatewayMap([]string{"istio-system/" + resources.WildcardGatewayName(wildcardCert.Name, ingressService.Namespace, ingressService.Name),
				"test-ns/" + perIngressGatewayName}, nil)),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ingressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-ingress",
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
		Key:     "test-ns/reconciling-ingress",
		CmpOpts: defaultCmpOptsList,
	}, {
		Name: "No preinstalled Ingress service",
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-ingress", ingressTLS),
			originSecret("istio-system", "secret0"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ingressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-ingress",
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
			ingressWithFinalizers("reconciling-ingress", ingressTLS, []string{ingressFinalizer}, &deletionTime),
			// ingressHTTPRedirectServer should not be deleted when deleting ingress related TLS server..
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1beta1.Server{irrelevantServer, ingressTLSServer, ingressHTTPRedirectServer}),
		},
		WantCreates: []runtime.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1beta1.Server{irrelevantServer, ingressTLSServer, ingressHTTPRedirectServer}),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1beta1.Server{ingressHTTPRedirectServer, irrelevantServer}),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ""),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Gateway %s/%s", system.Namespace(), config.KnativeIngressGateway),
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "reconciling-ingress"),
		},
		Key:     "test-ns/reconciling-ingress",
		CmpOpts: defaultCmpOptsList,
	}, {
		Name:                    "delete ingress with leftover secrets",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithFinalizers("reconciling-ingress", ingressTLS, []string{ingressFinalizer}, &deletionTime),
			// ingressHTTPRedirectServer should not be deleted when deleting ingress related TLS server..
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1beta1.Server{irrelevantServer, ingressTLSServer, ingressHTTPRedirectServer}),
			targetSecret("istio-system", "targetSecret", resources.MakeTargetSecretLabels("secret0", "istio-system")),
		},
		WantCreates: []runtime.Object{
			// The creation of gateways are triggered when setting up the test.
			gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1beta1.Server{irrelevantServer, ingressTLSServer, ingressHTTPRedirectServer}),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: gateway(config.KnativeIngressGateway, system.Namespace(), []*istiov1beta1.Server{ingressHTTPRedirectServer, irrelevantServer}),
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
		Key:     "test-ns/reconciling-ingress",
		CmpOpts: defaultCmpOptsList,
	}, {
		Name:                    "TLS Secret is not in the namespace of Istio gateway service",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-ingress", ingressTLSWithSecretNamespace("knative-serving")),
			// The namespace (`knative-serving`) of the origin secret is different
			// from the namespace (`istio-system`) of Istio gateway service.
			originSecret("knative-serving", "secret0"),
			ingressService,
		},
		WantCreates: []runtime.Object{
			// The newly created per-Ingress Gateway.
			gateway(perIngressGatewayName, testNS,
				[]*istiov1beta1.Server{withCredentialName(deepCopy(ingressTLSServer), targetSecretName), ingressHTTPServer},
				withOwnerRef(ingressWithTLS("reconciling-ingress", ingressTLS)),
				withLabels(gwLabels), withSelector(selector)),

			resources.MakeMeshVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", ingressTLSWithSecretNamespace("knative-serving"))), ingressGateway),
			resources.MakeIngressVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", ingressTLSWithSecretNamespace("knative-serving"))), makeGatewayMap([]string{"test-ns/" + perIngressGatewayName}, nil)),

			// The secret copy under istio-system.
			targetSecret("istio-system", targetSecretName, map[string]string{
				"networking.internal.knative.dev/certificate-uid": "",
				networking.OriginSecretNameLabelKey:               "secret0",
				networking.OriginSecretNamespaceLabelKey:          "knative-serving",
			}),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("reconciling-ingress", ingressFinalizer),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatus("reconciling-ingress",
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
		Key:     "test-ns/reconciling-ingress",
		CmpOpts: defaultCmpOptsList,
	}, {
		Name:                    "Reconcile Target secret",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLS("reconciling-ingress", ingressTLSWithSecretNamespace("knative-serving")),

			// The newly created per-Ingress Gateway.
			gateway(perIngressGatewayName, testNS,
				[]*istiov1beta1.Server{withCredentialName(deepCopy(ingressTLSServer), targetSecretName), ingressHTTPServer},
				withOwnerRef(ingressWithTLS("reconciling-ingress", ingressTLS)),
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
			gateway(perIngressGatewayName, testNS,
				[]*istiov1beta1.Server{withCredentialName(deepCopy(ingressTLSServer), targetSecretName), ingressHTTPServer},
				withOwnerRef(ingressWithTLS("reconciling-ingress", ingressTLS)),
				withLabels(gwLabels), withSelector(selector)),

			resources.MakeMeshVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", ingressTLSWithSecretNamespace("knative-serving"))), ingressGateway),
			resources.MakeIngressVirtualService(insertProbe(ingressWithTLS("reconciling-ingress", ingressTLSWithSecretNamespace("knative-serving"))), makeGatewayMap([]string{"test-ns/" + perIngressGatewayName}, nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      targetSecretName,
					Namespace: "istio-system",
					Labels: map[string]string{
						"networking.internal.knative.dev/certificate-uid": "",
						networking.OriginSecretNameLabelKey:               "secret0",
						networking.OriginSecretNamespaceLabelKey:          "knative-serving",
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
			Object: ingressWithTLSAndStatus("reconciling-ingress",
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
		Key:     "test-ns/reconciling-ingress",
		CmpOpts: defaultCmpOptsList,
	}, {
		Name:                    "Reconcile with autoTLS but cluster local visibilty, mesh only",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingressWithTLSClusterLocal("reconciling-ingress", ingressTLS),
			originSecret("istio-system", "secret0"),
		},
		WantCreates: []runtime.Object{
			resources.MakeMeshVirtualService(insertProbe(ingressWithTLSClusterLocal("reconciling-ingress", ingressTLS)), ingressGateway),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithTLSAndStatusClusterLocal("reconciling-ingress",
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
		Key:     "test-ns/reconciling-ingress",
		CmpOpts: defaultCmpOptsList,
	}}
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {

		// As we use a customized resource name for Gateway CRD (i.e. `gateways`), not the one
		// originally generated by kubernetes code generator (i.e. `gatewaies`), we have to
		// explicitly create gateways when setting up the test per suggestion
		// https://github.com/knative/serving/blob/a6852fc3b6cdce72b99c5d578dd64f2e03dabb8b/vendor/k8s.io/client-go/testing/fixture.go#L292
		gateways := getGatewaysFromObjects(listers.GetIstioObjects())
		for _, gateway := range gateways {
			fakeistioclient.Get(ctx).NetworkingV1beta1().Gateways(gateway.Namespace).Create(ctx, gateway, metav1.CreateOptions{})
		}

		r := &Reconciler{
			kubeclient:           kubeclient.Get(ctx),
			istioClientSet:       istioclient.Get(ctx),
			virtualServiceLister: listers.GetVirtualServiceLister(),
			gatewayLister:        listers.GetGatewayLister(),
			secretLister:         listers.GetSecretLister(),
			svcLister:            listers.GetK8sServiceLister(),
			tracker:              &NullTracker{},
			statusManager: &fakestatusmanager.FakeStatusManager{
				FakeIsReady: func(ctx context.Context, ing *v1alpha1.Ingress) (bool, error) {
					return true, nil
				},
			},
		}

		return ingressreconciler.NewReconciler(ctx, logging.FromContext(ctx), fakenetworkingclient.Get(ctx),
			listers.GetIngressLister(), controller.GetEventRecorder(ctx), r, netconfig.IstioIngressClassName, controller.Options{
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
						Network: &netconfig.Config{
							HTTPProtocol: netconfig.HTTPDisabled,
							AutoTLS:      true,
						},
					},
				},
			})
	}))
}

func getGatewaysFromObjects(objects []runtime.Object) []*v1beta1.Gateway {
	gateways := []*v1beta1.Gateway{}
	for _, object := range objects {
		if gateway, ok := object.(*v1beta1.Gateway); ok {
			gateways = append(gateways, gateway)
		}
	}
	sort.Slice(gateways, func(i, j int) bool {
		return strings.Compare(gateways[i].Name, gateways[j].Name) == -1
	})
	return gateways
}

type GatewayOpt func(*v1beta1.Gateway)

func gateway(name, namespace string, servers []*istiov1beta1.Server, opts ...GatewayOpt) *v1beta1.Gateway {
	gw := &v1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: istiov1beta1.Gateway{
			Servers: servers,
		},
	}
	for _, opt := range opts {
		opt(gw)
	}
	return gw
}

func withOwnerRef(ing *v1alpha1.Ingress) GatewayOpt {
	return func(gw *v1beta1.Gateway) {
		gw.OwnerReferences = []metav1.OwnerReference{*kmeta.NewControllerRef(ing)}
	}
}

func withLabels(labels map[string]string) GatewayOpt {
	return func(gw *v1beta1.Gateway) {
		gw.Labels = labels
	}
}

func withSelector(selector map[string]string) GatewayOpt {
	return func(gw *v1beta1.Gateway) {
		gw.Spec.Selector = selector
	}
}

func wildcardGateway(name, namespace string, servers []*istiov1beta1.Server, selector map[string]string) *v1beta1.Gateway {
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
	secret.OwnerReferences = []metav1.OwnerReference{*kmeta.NewControllerRef(ing("reconciling-ingress"))}
	secret.Labels = labels
	return &secret
}

func withCredentialName(tlsServer *istiov1beta1.Server, credentialName string) *istiov1beta1.Server {
	tlsServer.Tls.CredentialName = credentialName
	return tlsServer
}

// Open-coded deepCopy since istio.io/api's Server doesn't provide one currently
func deepCopy(server *istiov1beta1.Server) *istiov1beta1.Server {
	return proto.Clone(server).(*istiov1beta1.Server)
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
		Network: &netconfig.Config{
			AutoTLS: false,
		},
	}
}

func ingressWithStatusAndFinalizers(name string, status v1alpha1.IngressStatus, finalizers []string) *v1alpha1.Ingress {
	ing := ingressWithStatus(name, status)
	ing.Finalizers = finalizers
	return ing
}

func ingressWithStatus(name string, status v1alpha1.IngressStatus) *v1alpha1.Ingress {
	return &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS,
			Labels: map[string]string{
				resources.RouteLabelKey:          "test-route",
				resources.RouteNamespaceLabelKey: testNS,
			},
			Annotations:     map[string]string{networking.IngressClassAnnotationKey: netconfig.IstioIngressClassName},
			ResourceVersion: "v1",
		},
		Spec: v1alpha1.IngressSpec{
			HTTPOption: v1alpha1.HTTPOptionEnabled,
			Rules:      ingressRules,
		},
		Status: status,
	}
}

func ing(name string) *v1alpha1.Ingress {
	return ingressWithStatus(name, v1alpha1.IngressStatus{})
}

func ingressWithFinalizers(name string, tls []v1alpha1.IngressTLS, finalizers []string, deletionTime *metav1.Time) *v1alpha1.Ingress {
	ingress := ingressWithTLS(name, tls)
	ingress.ObjectMeta.Finalizers = finalizers
	if deletionTime != nil {
		ingress.ObjectMeta.DeletionTimestamp = deletionTime
	}
	return ingress
}

func basicReconciledIngress(name string) *v1alpha1.Ingress {
	ingress := ingressWithStatusAndFinalizers(name, v1alpha1.IngressStatus{
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
		PrivateLoadBalancer: &v1alpha1.LoadBalancerStatus{Ingress: []v1alpha1.LoadBalancerIngressStatus{{MeshOnly: true}}},
		PublicLoadBalancer:  &v1alpha1.LoadBalancerStatus{Ingress: []v1alpha1.LoadBalancerIngressStatus{{DomainInternal: "test-ingressgateway.istio-system.svc.cluster.local"}}},
	}, []string{"ingresses.networking.internal.knative.dev"})

	return ingress
}

func ingressWithTLS(name string, tls []v1alpha1.IngressTLS) *v1alpha1.Ingress {
	return ingressWithTLSAndStatus(name, tls, v1alpha1.IngressStatus{})
}

func ingressWithTLSClusterLocal(name string, tls []v1alpha1.IngressTLS) *v1alpha1.Ingress {
	ci := ingressWithTLSAndStatus(name, tls, v1alpha1.IngressStatus{}).DeepCopy()
	rules := ci.Spec.Rules
	for i, rule := range rules {
		rCopy := rule.DeepCopy()
		rCopy.Visibility = v1alpha1.IngressVisibilityClusterLocal
		rules[i] = *rCopy
	}

	ci.Spec.Rules = rules

	return ci
}

func ingressWithTLSAndStatus(name string, tls []v1alpha1.IngressTLS, status v1alpha1.IngressStatus) *v1alpha1.Ingress {
	ci := ingressWithStatus(name, status)
	ci.Spec.TLS = tls
	return ci
}

func ingressWithTLSAndStatusClusterLocal(name string, tls []v1alpha1.IngressTLS, status v1alpha1.IngressStatus) *v1alpha1.Ingress {
	ci := ingressWithTLSClusterLocal(name, tls)
	ci.Status = status
	return ci
}

func newTestSetup(t *testing.T, configs ...*corev1.ConfigMap) (
	context.Context,
	context.CancelFunc,
	[]controller.Informer,
	*controller.Impl,
	*configmap.ManualWatcher) {

	ctx, cancel, informers := SetupFakeContextWithCancel(t, func(ctx context.Context) context.Context {
		return filteredFactory.WithSelectors(ctx, networking.CertificateUIDLabelKey)
	})
	configMapWatcher := &configmap.ManualWatcher{Namespace: system.Namespace()}

	controller := newControllerWithOptions(ctx,
		configMapWatcher,
		func(r *Reconciler) {
			r.statusManager = &fakestatusmanager.FakeStatusManager{
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
			Name:      netconfig.ConfigMapName,
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

	waitInformers, err := RunAndSyncInformers(ctx, informers...)
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
	grp.Go(func() error { return ctrl.RunContext(ctx, 1) })

	ingress := ingressWithStatus("config-update",
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
		createdGateway := obj.(*v1beta1.Gateway)
		// The expected gateway should include the Istio TLS server.
		expectedGateway := gateway(perIngressGatewayName, testNS,
			[]*istiov1beta1.Server{ingressTLSServer, ingressHTTPServer}, withOwnerRef(ingressWithTLS("reconciling-ingress", ingressTLS)),
			withLabels(gwLabels), withSelector(selector))
		if diff := cmp.Diff(createdGateway, expectedGateway, protocmp.Transform()); diff != "" {
			t.Log("Unexpected Gateway (-want, +got):", diff)
			return HookIncomplete
		}

		return HookComplete
	})

	waitInformers, err := RunAndSyncInformers(ctx, informers...)
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

	grp.Go(func() error { return ctrl.RunContext(ctx, 1) })

	ingress := ingressWithTLSAndStatus("reconciling-ingress",
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

	gatewayClient := istioClient.NetworkingV1beta1().Gateways(system.Namespace())
	// Create a Gateway
	if _, err := gatewayClient.Create(ctx, gateway("knative-test-gateway", system.Namespace(), []*istiov1beta1.Server{}), metav1.CreateOptions{}); err != nil {
		t.Fatal("Error creating gateway:", err)
	}

	// Create an Ingress gateway
	ingressGatewayClient := istioClient.NetworkingV1beta1().Gateways(testNS)
	ingressGateway := gateway(perIngressGatewayName, testNS,
		[]*istiov1beta1.Server{}, withOwnerRef(ingressWithTLS("reconciling-ingress", ingressTLS)),
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
			Name:      netconfig.ConfigMapName,
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

func makeGatewayMap(publicGateways []string, privateGateways []string) map[v1alpha1.IngressVisibility]sets.Set[string] {
	return map[v1alpha1.IngressVisibility]sets.Set[string]{
		v1alpha1.IngressVisibilityExternalIP:   sets.New(publicGateways...),
		v1alpha1.IngressVisibilityClusterLocal: sets.New(privateGateways...),
	}
}

func proberCalledTimes(n int) func(*testing.T, *TableRow) {
	return func(t *testing.T, tr *TableRow) {
		// ensure that prober gets invoked the required number of times
		statusManager := tr.Ctx.Value(FakeStatusManagerKey).(*fakestatusmanager.FakeStatusManager)
		callCount := statusManager.IsReadyCallCount(tr.Objects[0].(*v1alpha1.Ingress))
		if callCount != n {
			t.Errorf("statusManager.IsReady called %v times, wanted %v", callCount, n)
		}
	}
}
