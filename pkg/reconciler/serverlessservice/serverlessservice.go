/*
Copyright 2021 The Knative Authors

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

package serverlessservice

import (
	"context"

	"k8s.io/client-go/kubernetes"
	istioclientset "knative.dev/net-istio/pkg/client/istio/clientset/versioned"
	istiolisters "knative.dev/net-istio/pkg/client/istio/listers/networking/v1alpha3"
	sksreconciler "knative.dev/networking/pkg/client/injection/reconciler/networking/v1alpha1/serverlessservice"

	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	pkgreconciler "knative.dev/pkg/reconciler"
)

// reconciler implements controller.Reconciler for SKS resources.
type reconciler struct {
	kubeclient  kubernetes.Interface
	istioclient istioclientset.Interface

	virtualServiceLister  istiolisters.VirtualServiceLister
	destinationRuleLister istiolisters.DestinationRuleLister
}

// Check that our Reconciler implements Interface
var _ sksreconciler.Interface = (*reconciler)(nil)

// Reconcile compares the actual state with the desired, and attempts to converge the two.
func (r *reconciler) ReconcileKind(ctx context.Context, sks *netv1alpha1.ServerlessService) pkgreconciler.Event {
	// TODO(markusthoemmes): Actually implement the reconciler.
	return nil
}
