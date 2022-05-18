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

package main

import (
	"istio.io/api/networking/v1beta1"
	"knative.dev/net-istio/pkg/reconciler/informerfiltering"
	"knative.dev/net-istio/pkg/reconciler/ingress"
	"knative.dev/net-istio/pkg/reconciler/serverlessservice"
	"knative.dev/pkg/signals"

	// This defines the shared main for injected controllers.
	"knative.dev/pkg/injection/sharedmain"
)

func main() {
	// Allow unknown fields in Istio API client. This is to be more
	// resilient to clusters containing malformed resources.
	v1beta1.VirtualServiceUnmarshaler.AllowUnknownFields = true
	v1beta1.GatewayUnmarshaler.AllowUnknownFields = true

	ctx := informerfiltering.GetContextWithFilteringLabelSelector(signals.NewContext())
	sharedmain.MainWithContext(ctx, "net-istio-controller", ingress.NewController, serverlessservice.NewController)
}
