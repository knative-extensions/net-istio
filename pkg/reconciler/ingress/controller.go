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

	"go.uber.org/zap"
	istioclient "knative.dev/net-istio/pkg/client/istio/injection/client"
	gatewayinformer "knative.dev/net-istio/pkg/client/istio/injection/informers/networking/v1alpha3/gateway"
	virtualserviceinformer "knative.dev/net-istio/pkg/client/istio/injection/informers/networking/v1alpha3/virtualservice"
	"knative.dev/net-istio/pkg/reconciler/ingress/config"
	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	ingressinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/ingress"
	ingressreconciler "knative.dev/networking/pkg/client/injection/reconciler/networking/v1alpha1/ingress"
	"knative.dev/networking/pkg/status"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	podinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"
	secretinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/secret"
	serviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/tracker"

	v1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
)

const controllerAgentName = "istio-ingress-controller"

type ingressOption func(*Reconciler)

// NewController works as a constructor for Ingress Controller
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	return newControllerWithOptions(ctx, cmw)
}

// AnnotateLoggerWithName names the logger in the context with the supplied name
//
// This is a stop gap until the generated reconcilers can do this
// automatically for you
func AnnotateLoggerWithName(ctx context.Context, name string) context.Context {
	logger := logging.FromContext(ctx).
		Named(name).
		With(zap.String(logkey.ControllerType, name))

	return logging.WithLogger(ctx, logger)

}
func newControllerWithOptions(
	ctx context.Context,
	cmw configmap.Watcher,
	opts ...ingressOption,
) *controller.Impl {

	ctx = AnnotateLoggerWithName(ctx, controllerAgentName)
	logger := logging.FromContext(ctx)
	virtualServiceInformer := virtualserviceinformer.Get(ctx)
	gatewayInformer := gatewayinformer.Get(ctx)
	secretInformer := secretinformer.Get(ctx)
	serviceInformer := serviceinformer.Get(ctx)
	ingressInformer := ingressinformer.Get(ctx)

	c := &Reconciler{
		kubeclient:           kubeclient.Get(ctx),
		istioClientSet:       istioclient.Get(ctx),
		virtualServiceLister: virtualServiceInformer.Lister(),
		gatewayLister:        gatewayInformer.Lister(),
		secretLister:         secretInformer.Lister(),
		svcLister:            serviceInformer.Lister(),
	}
	myFilterFunc := reconciler.AnnotationFilterFunc(networking.IngressClassAnnotationKey, network.IstioIngressClassName, true)

	impl := ingressreconciler.NewImpl(ctx, c, network.IstioIngressClassName, func(impl *controller.Impl) controller.Options {
		logger.Info("Setting up ConfigMap receivers")
		configsToResync := []interface{}{
			&config.Istio{},
			&network.Config{},
		}
		resyncIngressesOnConfigChange := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
			impl.FilteredGlobalResync(myFilterFunc, ingressInformer.Informer())
		})
		configStore := config.NewStore(logger.Named("config-store"), resyncIngressesOnConfigChange)
		configStore.WatchConfigs(cmw)
		return controller.Options{ConfigStore: configStore}
	})

	logger.Info("Setting up Ingress event handlers")
	ingressHandler := cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler:    controller.HandleAll(impl.Enqueue),
	}
	ingressInformer.Informer().AddEventHandler(ingressHandler)

	virtualServiceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	logger.Info("Setting up statusManager")
	endpointsInformer := endpointsinformer.Get(ctx)
	podInformer := podinformer.Get(ctx)
	resyncOnIngressReady := func(ing *v1alpha1.Ingress) {
		impl.EnqueueKey(types.NamespacedName{Namespace: ing.GetNamespace(), Name: ing.GetName()})
	}
	statusProber := status.NewProber(
		logger.Named("status-manager"),
		NewProbeTargetLister(
			logger.Named("probe-lister"),
			gatewayInformer.Lister(),
			endpointsInformer.Lister(),
			serviceInformer.Lister()),
		resyncOnIngressReady)
	c.statusManager = statusProber
	statusProber.Start(ctx.Done())

	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		// Cancel probing when a Pod is deleted
		DeleteFunc: statusProber.CancelPodProbing,
	})

	logger.Info("Setting up secret informer event handler")
	tracker := tracker.New(impl.EnqueueKey, controller.GetTrackerLease(ctx))
	c.tracker = tracker

	secretInformer.Informer().AddEventHandler(controller.HandleAll(
		controller.EnsureTypeMeta(
			tracker.OnChanged,
			corev1.SchemeGroupVersion.WithKind("Secret"),
		),
	))

	gatewayInformer.Informer().AddEventHandler(controller.HandleAll(
		controller.EnsureTypeMeta(
			tracker.OnChanged,
			v1alpha3.SchemeGroupVersion.WithKind("Gateway"),
		),
	))

	ingressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		// Cancel probing when a Ingress is deleted
		DeleteFunc: combineFunc(
			statusProber.CancelIngressProbing,
			tracker.OnDeletedObserver,
		),
	})

	for _, opt := range opts {
		opt(c)
	}
	return impl
}

func combineFunc(functions ...func(interface{})) func(interface{}) {
	return func(obj interface{}) {
		for _, f := range functions {
			f(obj)
		}
	}
}
