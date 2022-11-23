/*
Copyright 2021 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package istio

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	istioclientset "knative.dev/net-istio/pkg/client/istio/clientset/versioned"
	istiolisters "knative.dev/net-istio/pkg/client/istio/listers/networking/v1alpha3"
	kaccessor "knative.dev/net-istio/pkg/reconciler/accessor"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
)

// DestinationRuleAccessor is an interface for accessing DestinationRule.
type DestinationRuleAccessor interface {
	GetIstioClient() istioclientset.Interface
	GetDestinationRuleLister() istiolisters.DestinationRuleLister
}

func destionationRuleIsDifferent(current, desired *v1alpha3.DestinationRule) bool {
	return !cmp.Equal(&current.Spec, &desired.Spec, protocmp.Transform()) ||
		!cmp.Equal(current.Labels, desired.Labels) ||
		!cmp.Equal(current.Annotations, desired.Annotations)
}

// ReconcileDestinationRule reconciles DestinationRule to the desired status.
func ReconcileDestinationRule(ctx context.Context, owner kmeta.Accessor, desired *v1alpha3.DestinationRule,
	drAccessor DestinationRuleAccessor) (*v1alpha3.DestinationRule, error) {

	recorder := controller.GetEventRecorder(ctx)
	if recorder == nil {
		return nil, fmt.Errorf("recorder for reconciling DestinationRule %s/%s is not created", desired.Namespace, desired.Name)
	}
	ns := desired.Namespace
	name := desired.Name
	dr, err := drAccessor.GetDestinationRuleLister().DestinationRules(ns).Get(name)
	if apierrs.IsNotFound(err) {
		dr, err = drAccessor.GetIstioClient().NetworkingV1alpha3().DestinationRules(ns).Create(ctx, desired, metav1.CreateOptions{})
		if err != nil {
			recorder.Eventf(owner, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create DestinationRule %s/%s: %v", ns, name, err)
			return nil, fmt.Errorf("failed to create DestinationRule: %w", err)
		}
		recorder.Eventf(owner, corev1.EventTypeNormal, "Created", "Created DestinationRule %q", desired.Name)
	} else if err != nil {
		return nil, err
	} else if !metav1.IsControlledBy(dr, owner) {
		// Return an error with NotControlledBy information.
		return nil, kaccessor.NewAccessorError(
			fmt.Errorf("owner: %s with Type %T does not own DestinationRule: %q", owner.GetName(), owner, name),
			kaccessor.NotOwnResource)
	} else if destionationRuleIsDifferent(dr, desired) {
		// Don't modify the informers copy
		existing := dr.DeepCopy()
		existing.Spec = *desired.Spec.DeepCopy()
		existing.Labels = desired.Labels
		existing.Annotations = desired.Annotations
		dr, err = drAccessor.GetIstioClient().NetworkingV1alpha3().DestinationRules(ns).Update(ctx, existing, metav1.UpdateOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to update DestinationRule: %w", err)
		}
		recorder.Eventf(owner, corev1.EventTypeNormal, "Updated", "Updated DestinationRule %s/%s", ns, name)
	}
	return dr, nil
}
