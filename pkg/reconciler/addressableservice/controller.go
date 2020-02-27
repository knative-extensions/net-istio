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

	"knative.dev/pkg/tracker"

	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"

	svcinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service"
	addressableserviceinformer "knative.dev/sample-controller/pkg/client/injection/informers/samples/v1alpha1/addressableservice"
	addressableservicereconciler "knative.dev/sample-controller/pkg/client/injection/reconciler/samples/v1alpha1/addressableservice"
)

// NewController creates a Reconciler and returns the result of NewImpl.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	logger := logging.FromContext(ctx)

	addressableserviceInformer := addressableserviceinformer.Get(ctx)
	svcInformer := svcinformer.Get(ctx)

	r := &Reconciler{
		ServiceLister: svcInformer.Lister(),
	}
	impl := addressableservicereconciler.NewImpl(ctx, r)
	r.Tracker = tracker.New(impl.EnqueueKey, controller.GetTrackerLease(ctx))

	logger.Info("Setting up event handlers.")

	addressableserviceInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	svcInformer.Informer().AddEventHandler(controller.HandleAll(
		// Call the tracker's OnChanged method, but we've seen the objects
		// coming through this path missing TypeMeta, so ensure it is properly
		// populated.
		controller.EnsureTypeMeta(
			r.Tracker.OnChanged,
			corev1.SchemeGroupVersion.WithKind("Service"),
		),
	))

	return impl
}
