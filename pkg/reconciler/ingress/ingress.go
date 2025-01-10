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
	"fmt"
	"sort"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/zap"
	"google.golang.org/protobuf/testing/protocmp"
	istiov1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	istiolisters "knative.dev/net-istio/pkg/client/istio/listers/networking/v1beta1"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/pkg/tracker"

	istioclientset "knative.dev/net-istio/pkg/client/istio/clientset/versioned"
	kaccessor "knative.dev/net-istio/pkg/reconciler/accessor"
	coreaccessor "knative.dev/net-istio/pkg/reconciler/accessor/core"
	istioaccessor "knative.dev/net-istio/pkg/reconciler/accessor/istio"
	"knative.dev/net-istio/pkg/reconciler/ingress/config"
	"knative.dev/net-istio/pkg/reconciler/ingress/resources"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	ingressreconciler "knative.dev/networking/pkg/client/injection/reconciler/networking/v1alpha1/ingress"
	netconfig "knative.dev/networking/pkg/config"
	"knative.dev/networking/pkg/status"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

const (
	virtualServiceNotReconciled = "ReconcileVirtualServiceFailed"
	notReconciledReason         = "ReconcileIngressFailed"
	notReconciledMessage        = "Ingress reconciliation failed"
)

// Reconciler implements the control loop for the Ingress resources.
type Reconciler struct {
	kubeclient kubernetes.Interface

	istioClientSet       istioclientset.Interface
	virtualServiceLister istiolisters.VirtualServiceLister
	gatewayLister        istiolisters.GatewayLister
	secretLister         corev1listers.SecretLister
	svcLister            corev1listers.ServiceLister

	tracker tracker.Interface

	statusManager status.Manager
}

var (
	_ ingressreconciler.Interface          = (*Reconciler)(nil)
	_ ingressreconciler.Finalizer          = (*Reconciler)(nil)
	_ coreaccessor.SecretAccessor          = (*Reconciler)(nil)
	_ istioaccessor.VirtualServiceAccessor = (*Reconciler)(nil)
)

// ReconcileKind compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Ingress resource
// with the current status of the resource.
func (r *Reconciler) ReconcileKind(ctx context.Context, ingress *v1alpha1.Ingress) pkgreconciler.Event {
	logger := logging.FromContext(ctx)

	reconcileErr := r.reconcileIngress(ctx, ingress)
	if reconcileErr != nil {
		logger.Errorw("Failed to reconcile Ingress: ", zap.Error(reconcileErr))
		ingress.Status.MarkIngressNotReady(notReconciledReason, notReconciledMessage)
		return reconcileErr
	}
	return nil
}

func (r *Reconciler) reconcileIngress(ctx context.Context, ing *v1alpha1.Ingress) error {
	logger := logging.FromContext(ctx)

	// We may be reading a version of the object that was stored at an older version
	// and may not have had all the assumed defaults specified.  This won't result
	// in this getting written back to the API Server, but let's downstream logic make
	// assumptions about defaulting.
	ing.SetDefaults(ctx)

	ing.Status.InitializeConditions()
	logger.Infof("Reconciling ingress: %#v", ing)

	defaultGateways, err := resources.GatewaysFromContext(ctx, ing)
	if err != nil {
		return err
	}

	gatewayNames := map[v1alpha1.IngressVisibility]sets.Set[string]{
		v1alpha1.IngressVisibilityClusterLocal: sets.New[string](),
		v1alpha1.IngressVisibilityExternalIP:   sets.New[string](),
	}

	for _, gateway := range defaultGateways[v1alpha1.IngressVisibilityClusterLocal] {
		gatewayNames[v1alpha1.IngressVisibilityClusterLocal].Insert(gateway.QualifiedName())
	}

	externalIngressGateways := []*v1beta1.Gateway{}
	if shouldReconcileExternalDomainTLS(ing) {
		originSecrets, err := resources.GetSecrets(ing, v1alpha1.IngressVisibilityExternalIP, r.secretLister)
		if err != nil {
			return err
		}
		nonWildcardSecrets, wildcardSecrets, err := resources.CategorizeSecrets(originSecrets)
		if err != nil {
			return err
		}
		targetNonwildcardSecrets, err := resources.MakeSecrets(ctx, nonWildcardSecrets, ing)
		if err != nil {
			return err
		}
		targetWildcardSecrets, err := resources.MakeWildcardSecrets(ctx, wildcardSecrets, ing)
		if err != nil {
			return err
		}
		targetSecrets := make([]*corev1.Secret, 0, len(targetNonwildcardSecrets)+len(targetWildcardSecrets))
		targetSecrets = append(targetSecrets, targetNonwildcardSecrets...)
		targetSecrets = append(targetSecrets, targetWildcardSecrets...)
		if err := r.reconcileCertSecrets(ctx, ing, targetSecrets); err != nil {
			return err
		}

		nonWildcardIngressTLS := resources.GetNonWildcardIngressTLS(ing.GetIngressTLSForVisibility(v1alpha1.IngressVisibilityExternalIP), nonWildcardSecrets)
		externalIngressGateways, err = resources.MakeIngressTLSGateways(ctx, ing, v1alpha1.IngressVisibilityExternalIP,
			nonWildcardIngressTLS, nonWildcardSecrets, r.svcLister)
		if err != nil {
			return err
		}

		// For Ingress TLS referencing wildcard certificates, we reconcile a separate Gateway
		// that will be shared by other Ingresses that reference the
		// same wildcard host. We need to handle wildcard certificate specially because Istio does
		// not fully support multiple TLS Servers (or Gateways) share the same certificate.
		// https://istio.io/docs/ops/common-problems/network-issues/
		desiredWildcardGateways, err := resources.MakeWildcardTLSGateways(ctx, ing, wildcardSecrets, r.svcLister)
		if err != nil {
			return err
		}
		if err := r.reconcileWildcardGateways(ctx, desiredWildcardGateways, ing); err != nil {
			return err
		}
		gatewayNames[v1alpha1.IngressVisibilityExternalIP].Insert(resources.GetQualifiedGatewayNames(desiredWildcardGateways)...)
	}

	cfg := config.FromContext(ctx)
	clusterLocalIngressGateways := []*v1beta1.Gateway{}
	if cfg.Network.ClusterLocalDomainTLS == netconfig.EncryptionEnabled && shouldReconcileClusterLocalDomainTLS(ing) {
		originSecrets, err := resources.GetSecrets(ing, v1alpha1.IngressVisibilityClusterLocal, r.secretLister)
		if err != nil {
			return err
		}
		targetSecrets, err := resources.MakeSecrets(ctx, originSecrets, ing)
		if err != nil {
			return err
		}
		if err = r.reconcileCertSecrets(ctx, ing, targetSecrets); err != nil {
			return err
		}
		clusterLocalIngressGateways, err = resources.MakeIngressTLSGateways(ctx, ing, v1alpha1.IngressVisibilityClusterLocal,
			ing.GetIngressTLSForVisibility(v1alpha1.IngressVisibilityClusterLocal), originSecrets, r.svcLister)
		if err != nil {
			return err
		}
	}

	if shouldReconcileHTTPServer(ing) {
		httpServer := resources.MakeHTTPServer(ing.Spec.HTTPOption, getPublicHosts(ing))
		if len(externalIngressGateways) == 0 {
			var err error
			if externalIngressGateways, err = resources.MakeExternalIngressGateways(ctx, ing, []*istiov1beta1.Server{httpServer}, r.svcLister); err != nil {
				return err
			}
		} else {
			// add HTTP Server into ingressGateways.
			for i := range externalIngressGateways {
				externalIngressGateways[i].Spec.Servers = append(externalIngressGateways[i].Spec.Servers, httpServer)
			}
		}
	} else {
		// Otherwise, we fall back to the default global Gateways for HTTP behavior.
		// We need this for the backward compatibility.
		defaultGlobalHTTPGateways := defaultGateways[v1alpha1.IngressVisibilityExternalIP]

		for _, gateway := range defaultGlobalHTTPGateways {
			gatewayNames[v1alpha1.IngressVisibilityExternalIP].Insert(gateway.QualifiedName())
		}
	}

	if err := r.reconcileIngressGateways(ctx, externalIngressGateways); err != nil {
		return err
	}
	gatewayNames[v1alpha1.IngressVisibilityExternalIP].Insert(resources.GetQualifiedGatewayNames(externalIngressGateways)...)

	if err := r.reconcileIngressGateways(ctx, clusterLocalIngressGateways); err != nil {
		return err
	}
	gatewayNames[v1alpha1.IngressVisibilityClusterLocal].Insert(resources.GetQualifiedGatewayNames(clusterLocalIngressGateways)...)

	vses, err := resources.MakeVirtualServices(ing, gatewayNames)
	if err != nil {
		return err
	}

	logger.Info("Creating/Updating VirtualServices")
	if err := r.reconcileVirtualServices(ctx, ing, vses); err != nil {
		ing.Status.MarkLoadBalancerFailed(virtualServiceNotReconciled, err.Error())
		return err
	}

	// Update status
	ing.Status.MarkNetworkConfigured()

	var ready bool
	if ing.IsReady() {
		// When the kingress has already been marked Ready for this generation,
		// then it must have been successfully probed.  The status manager has
		// caching built-in, which makes this exception unnecessary for the case
		// of global resyncs.  HOWEVER, that caching doesn't help at all for
		// the failover case (cold caches), and the initial sync turns into a
		// thundering herd.
		// As this is an optimization, we don't worry about the ObservedGeneration
		// skew we might see when the resource is actually in flux, we simply care
		// about the steady state.
		logger.Debug("Kingress is ready, skipping probe.")
		ready = true
	} else {
		readyStatus, err := r.statusManager.IsReady(ctx, ing)
		if err != nil {
			return fmt.Errorf("failed to probe Ingress %s/%s: %w", ing.GetNamespace(), ing.GetName(), err)
		}
		ready = readyStatus
	}

	if ready {
		publicGatewayURL := gatewayServiceURL(defaultGateways[v1alpha1.IngressVisibilityExternalIP])
		publicLbs := getLBStatus(publicGatewayURL)

		privateGatewayURL := gatewayServiceURL(defaultGateways[v1alpha1.IngressVisibilityClusterLocal])
		privateLbs := getLBStatus(privateGatewayURL)

		ing.Status.MarkLoadBalancerReady(publicLbs, privateLbs)
	} else {
		ing.Status.MarkLoadBalancerNotReady()
	}

	// TODO(zhiminx): Mark Route status to indicate that Gateway is configured.
	logger.Info("Ingress successfully synced")
	return nil
}

func getPublicHosts(ing *v1alpha1.Ingress) []string {
	hosts := sets.New[string]()
	for _, rule := range ing.Spec.Rules {
		if rule.Visibility == v1alpha1.IngressVisibilityExternalIP {
			hosts.Insert(rule.Hosts...)
		}
	}
	return sets.List(hosts)
}

func (r *Reconciler) reconcileCertSecrets(ctx context.Context, ing *v1alpha1.Ingress, desiredSecrets []*corev1.Secret) error {
	for _, certSecret := range desiredSecrets {
		// We track the origin and desired secrets so that desired secrets could be synced accordingly when the origin TLS certificate
		// secret is refreshed.
		r.tracker.TrackReference(resources.SecretRef(certSecret.Namespace, certSecret.Name), ing)
		r.tracker.TrackReference(resources.ExtractOriginSecretRef(certSecret), ing)
		if _, err := coreaccessor.ReconcileSecret(ctx, nil, certSecret, r); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) reconcileWildcardGateways(ctx context.Context, gateways []*v1beta1.Gateway, ing *v1alpha1.Ingress) error {
	for _, gateway := range gateways {
		r.tracker.TrackReference(resources.GatewayRef(gateway), ing)
		if err := r.reconcileSystemGeneratedGateway(ctx, gateway); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) reconcileIngressGateways(ctx context.Context, gateways []*v1beta1.Gateway) error {
	for _, gateway := range gateways {
		if err := r.reconcileSystemGeneratedGateway(ctx, gateway); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) reconcileSystemGeneratedGateway(ctx context.Context, desired *v1beta1.Gateway) error {
	existing, err := r.gatewayLister.Gateways(desired.Namespace).Get(desired.Name)
	if apierrs.IsNotFound(err) {
		if _, err := r.istioClientSet.NetworkingV1beta1().Gateways(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if !cmp.Equal(existing.Spec.DeepCopy(), desired.Spec.DeepCopy(), protocmp.Transform()) {
		deepCopy := existing.DeepCopy()
		deepCopy.Spec = *desired.Spec.DeepCopy()
		if _, err := r.istioClientSet.NetworkingV1beta1().Gateways(desired.Namespace).Update(ctx, deepCopy, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) reconcileVirtualServices(ctx context.Context, ing *v1alpha1.Ingress,
	desired []*v1beta1.VirtualService,
) error {
	// First, create all needed VirtualServices.
	kept := sets.New[string]()
	for _, d := range desired {
		if d.GetAnnotations()[networking.IngressClassAnnotationKey] != netconfig.IstioIngressClassName {
			// We do not create resources that do not have istio ingress class annotation.
			// As a result, obsoleted resources will be cleaned up.
			continue
		}
		if _, err := istioaccessor.ReconcileVirtualService(ctx, ing, d, r); err != nil {
			if kaccessor.IsNotOwned(err) {
				ing.Status.MarkResourceNotOwned("VirtualService", d.Name)
			}
			return err
		}
		kept.Insert(d.Name)
	}

	// Now, remove the extra ones.
	selectors := map[string]string{
		networking.IngressLabelKey: ing.GetName(),                            // VS created from 0.12 on
		resources.RouteLabelKey:    ing.GetLabels()[resources.RouteLabelKey], // VS created before 0.12
	}
	for k, v := range selectors {
		vses, err := r.virtualServiceLister.VirtualServices(ing.GetNamespace()).List(
			labels.SelectorFromSet(labels.Set{k: v}))
		if err != nil {
			return fmt.Errorf("failed to list VirtualServices: %w", err)
		}

		// Sort the virtual services by name to get a stable deletion order.
		sort.Slice(vses, func(i, j int) bool {
			return vses[i].Name < vses[j].Name
		})

		for _, vs := range vses {
			n, ns := vs.Name, vs.Namespace
			if kept.Has(n) {
				continue
			}
			if !metav1.IsControlledBy(vs, ing) {
				// We shouldn't remove resources not controlled by us.
				continue
			}
			if err = r.istioClientSet.NetworkingV1beta1().VirtualServices(ns).Delete(ctx, n, metav1.DeleteOptions{}); err != nil {
				return fmt.Errorf("failed to delete VirtualService: %w", err)
			}
		}
	}
	return nil
}

func (r *Reconciler) FinalizeKind(ctx context.Context, ing *v1alpha1.Ingress) pkgreconciler.Event {
	logger := logging.FromContext(ctx)
	istiocfg := config.FromContext(ctx).Istio
	logger.Info("Cleaning up Gateway Servers")
	for _, gws := range [][]config.Gateway{istiocfg.IngressGateways, istiocfg.LocalGateways} {
		for _, gw := range gws {
			if err := r.reconcileIngressServers(ctx, ing, gw, []*istiov1beta1.Server{}); err != nil {
				return err
			}
		}
	}

	return r.cleanupCertificateSecrets(ctx, ing)
}

func (r *Reconciler) cleanupCertificateSecrets(ctx context.Context, ing *v1alpha1.Ingress) error {
	if !shouldReconcileExternalDomainTLS(ing) && !shouldReconcileClusterLocalDomainTLS(ing) {
		return nil
	}

	errs := []error{}
	for _, tls := range ing.Spec.TLS {
		nameNamespaces, err := resources.GetIngressGatewaySvcNameNamespaces(ctx, ing)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, nameNamespace := range nameNamespaces {
			secrets, err := r.GetSecretLister().Secrets(nameNamespace.Namespace).List(labels.SelectorFromSet(
				resources.MakeTargetSecretLabels(tls.SecretName, tls.SecretNamespace)))
			if err != nil {
				errs = append(errs, err)
				continue
			}
			for _, secret := range secrets {
				if err := r.GetKubeClient().CoreV1().Secrets(secret.Namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{}); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	return errors.NewAggregate(errs)
}

func (r *Reconciler) reconcileIngressServers(ctx context.Context, ing *v1alpha1.Ingress, gw config.Gateway, desired []*istiov1beta1.Server) error {
	gateway, err := r.gatewayLister.Gateways(gw.Namespace).Get(gw.Name)
	if err != nil {
		// Unlike VirtualService, a default gateway needs to be existent.
		// It should be installed when installing Knative.
		return fmt.Errorf("failed to get Gateway: %w", err)
	}
	existing := resources.GetServers(gateway, ing)
	return r.reconcileGateway(ctx, ing, gateway, existing, desired)
}

func (r *Reconciler) reconcileGateway(ctx context.Context, ing *v1alpha1.Ingress, gateway *v1beta1.Gateway, existing []*istiov1beta1.Server, desired []*istiov1beta1.Server) error {
	if cmp.Equal(existing, desired, protocmp.Transform()) {
		return nil
	}

	deepCopy := gateway.DeepCopy()
	deepCopy = resources.UpdateGateway(deepCopy, desired, existing)
	if _, err := r.istioClientSet.NetworkingV1beta1().Gateways(deepCopy.Namespace).Update(ctx, deepCopy, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update Gateway: %w", err)
	}
	controller.GetEventRecorder(ctx).Eventf(ing, corev1.EventTypeNormal,
		"Updated", "Updated Gateway %s/%s", gateway.Namespace, gateway.Name)
	return nil
}

// GetKubeClient returns the client to access k8s resources.
func (r *Reconciler) GetKubeClient() kubernetes.Interface {
	return r.kubeclient
}

// GetSecretLister returns the lister for Secret.
func (r *Reconciler) GetSecretLister() corev1listers.SecretLister {
	return r.secretLister
}

// GetIstioClient returns the client to access Istio resources.
func (r *Reconciler) GetIstioClient() istioclientset.Interface {
	return r.istioClientSet
}

// GetVirtualServiceLister returns the lister for VirtualService.
func (r *Reconciler) GetVirtualServiceLister() istiolisters.VirtualServiceLister {
	return r.virtualServiceLister
}

func gatewayServiceURL(gateways []config.Gateway) string {
	if len(gateways) == 0 {
		return ""
	}

	return gateways[0].ServiceURL
}

// getLBStatus gets the LB Status.
func getLBStatus(gatewayServiceURL string) []v1alpha1.LoadBalancerIngressStatus {
	// The Ingress isn't load-balanced by any particular
	// Service, but through a Service mesh.
	if gatewayServiceURL == "" {
		return []v1alpha1.LoadBalancerIngressStatus{
			{MeshOnly: true},
		}
	}
	return []v1alpha1.LoadBalancerIngressStatus{
		{DomainInternal: gatewayServiceURL},
	}
}

func shouldReconcileExternalDomainTLS(ing *v1alpha1.Ingress) bool {
	return isIngressPublic(ing) && len(ing.GetIngressTLSForVisibility(v1alpha1.IngressVisibilityExternalIP)) > 0
}

func shouldReconcileClusterLocalDomainTLS(ing *v1alpha1.Ingress) bool {
	return len(ing.GetIngressTLSForVisibility(v1alpha1.IngressVisibilityClusterLocal)) > 0
}

func shouldReconcileHTTPServer(ing *v1alpha1.Ingress) bool {
	// We will create an Ingress specific HTTPServer when
	// 1. external-domain-tls is enabled as in this case users want us to fully handle the TLS/HTTP behavior,
	// 2. HTTPOption is set to Redirected as we don't have default HTTP server supporting HTTP redirection.
	return isIngressPublic(ing) && (ing.Spec.HTTPOption == v1alpha1.HTTPOptionRedirected || len(ing.GetIngressTLSForVisibility(v1alpha1.IngressVisibilityExternalIP)) > 0)
}

func isIngressPublic(ing *v1alpha1.Ingress) bool {
	for _, rule := range ing.Spec.Rules {
		if rule.Visibility == v1alpha1.IngressVisibilityExternalIP {
			return true
		}
	}
	return false
}
