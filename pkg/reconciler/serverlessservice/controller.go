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

	"k8s.io/client-go/tools/cache"
	istioclient "knative.dev/net-istio/pkg/client/istio/injection/client"
	destinationruleinformer "knative.dev/net-istio/pkg/client/istio/injection/informers/networking/v1beta1/destinationrule"
	virtualserviceinformer "knative.dev/net-istio/pkg/client/istio/injection/informers/networking/v1beta1/virtualservice"
	"knative.dev/net-istio/pkg/reconciler/ingress/config"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	sksinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/serverlessservice"
	sksreconciler "knative.dev/networking/pkg/client/injection/reconciler/networking/v1alpha1/serverlessservice"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
)

// NewController initializes the controller and is called by the generated code.
// Registers eventhandlers to enqueue events.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {

	logger := logging.FromContext(ctx)
	sksInformer := sksinformer.Get(ctx)
	virtualServiceInformer := virtualserviceinformer.Get(ctx)
	destinationRuleInformer := destinationruleinformer.Get(ctx)

	c := &reconciler{
		istioclient:           istioclient.Get(ctx),
		virtualServiceLister:  virtualServiceInformer.Lister(),
		destinationRuleLister: destinationRuleInformer.Lister(),
	}
	impl := sksreconciler.NewImpl(ctx, c, func(impl *controller.Impl) controller.Options {
		resync := configmap.TypeFilter(&config.Istio{})(func(string, interface{}) {
			impl.GlobalResync(sksInformer.Informer())
		})
		configStore := config.NewStore(logger.Named("config-store"), resync)
		configStore.WatchConfigs(cmw)

		return controller.Options{
			ConfigStore: configStore,
			// We're not owning the SKSs status, so we don't update it.
			SkipStatusUpdates: true,
		}
	})

	// Watch all the SKS objects.
	sksInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	// Watch all VirtualServices and DestinationRules created from SKS objects.
	handleMatchingControllers := cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterController(&netv1alpha1.ServerlessService{}),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	}
	virtualServiceInformer.Informer().AddEventHandler(handleMatchingControllers)
	destinationRuleInformer.Informer().AddEventHandler(handleMatchingControllers)

	return impl
}
