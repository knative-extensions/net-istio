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

package addressableservice

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	corev1listers "k8s.io/client-go/listers/core/v1"

	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/network"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/tracker"
	samplesv1alpha1 "knative.dev/sample-controller/pkg/apis/samples/v1alpha1"
	addressableservicereconciler "knative.dev/sample-controller/pkg/client/injection/reconciler/samples/v1alpha1/addressableservice"
)

// newReconciledNormal makes a new reconciler event with event type Normal, and
// reason AddressableServiceReconciled.
func newReconciledNormal(namespace, name string) reconciler.Event {
	return reconciler.NewEvent(corev1.EventTypeNormal, "AddressableServiceReconciled", "AddressableService reconciled: \"%s/%s\"", namespace, name)
}

// Reconciler implements addressableservicereconciler.Interface for
// AddressableService resources.
type Reconciler struct {
	// Tracker builds an index of what resources are watching other resources
	// so that we can immediately react to changes tracked resources.
	Tracker tracker.Interface

	// Listers index properties about resources
	ServiceLister corev1listers.ServiceLister
}

// Check that our Reconciler implements Interface
var _ addressableservicereconciler.Interface = (*Reconciler)(nil)

// ReconcileKind implements Interface.ReconcileKind.
func (r *Reconciler) ReconcileKind(ctx context.Context, o *samplesv1alpha1.AddressableService) reconciler.Event {
	if o.GetDeletionTimestamp() != nil {
		// Check for a DeletionTimestamp.  If present, elide the normal reconcile logic.
		// When a controller needs finalizer handling, it would go here.
		return nil
	}
	o.Status.InitializeConditions()

	if err := r.reconcileForService(ctx, o); err != nil {
		return err
	}

	o.Status.ObservedGeneration = o.Generation
	return newReconciledNormal(o.Namespace, o.Name)
}

func (r *Reconciler) reconcileForService(ctx context.Context, asvc *samplesv1alpha1.AddressableService) error {
	logger := logging.FromContext(ctx)

	if err := r.Tracker.TrackReference(tracker.Reference{
		APIVersion: "v1",
		Kind:       "Service",
		Name:       asvc.Spec.ServiceName,
		Namespace:  asvc.Namespace,
	}, asvc); err != nil {
		logger.Errorf("Error tracking service %s: %v", asvc.Spec.ServiceName, err)
		return err
	}

	_, err := r.ServiceLister.Services(asvc.Namespace).Get(asvc.Spec.ServiceName)
	if apierrs.IsNotFound(err) {
		logger.Info("Service does not yet exist:", asvc.Spec.ServiceName)
		asvc.Status.MarkServiceUnavailable(asvc.Spec.ServiceName)
		return nil
	} else if err != nil {
		logger.Errorf("Error reconciling service %s: %v", asvc.Spec.ServiceName, err)
		return err
	}

	asvc.Status.MarkServiceAvailable()
	asvc.Status.Address = &duckv1.Addressable{
		URL: &apis.URL{
			Scheme: "http",
			Host:   network.GetServiceHostname(asvc.Spec.ServiceName, asvc.Namespace),
		},
	}
	return nil
}
