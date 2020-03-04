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
	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/webhook"
)

// TODO(nghia): Validate config-istio
// func NewConfigValidationController(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
// 	return configmaps.NewAdmissionController(ctx,

// 		// Name of the configmap webhook.
// 		fmt.Sprintf("config.webhook.%s.knative.dev", system.Namespace()),

// 		// The path on which to serve the webhook.
// 		"/config-validation",

// 		// The configmaps to validate.
// 		configmap.Constructors{
// 			logging.ConfigMapName(): logging.NewConfigFromConfigMap,
// 		},
// 	)
// }

func main() {
	ctx := webhook.WithOptions(signals.NewContext(), webhook.Options{
		ServiceName: "webhook",
		Port:        8443,
		SecretName:  "webhook-certs",
	})

	sharedmain.MainWithContext(
		ctx, "webhook",
		// TODO(nghia): NewConfigValidationController,
	)
}
