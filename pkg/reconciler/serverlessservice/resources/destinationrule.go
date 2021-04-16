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

package resources

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/kmeta"
	pkgnetwork "knative.dev/pkg/network"
)

const (
	subsetNormal = "normal"
	subsetDirect = "direct"
)

// MakeDestinationRule creates a DestinationRule that defines a "normal" and a "direct"
// loadbalancer for the service in question, to allow for pod addressability, even in mesh.
func MakeDestinationRule(sks *v1alpha1.ServerlessService) *v1alpha3.DestinationRule {
	ns := sks.Namespace
	name := sks.Status.PrivateServiceName
	host := pkgnetwork.GetServiceHostname(name, ns)

	return &v1alpha3.DestinationRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       ns,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(sks)},
		},
		Spec: istiov1alpha3.DestinationRule{
			Host: host,
			Subsets: []*istiov1alpha3.Subset{{
				Name: subsetNormal,
				TrafficPolicy: &istiov1alpha3.TrafficPolicy{
					LoadBalancer: &istiov1alpha3.LoadBalancerSettings{
						LbPolicy: &istiov1alpha3.LoadBalancerSettings_Simple{
							Simple: istiov1alpha3.LoadBalancerSettings_LEAST_CONN,
						},
					},
				},
			}, {
				Name: subsetDirect,
				TrafficPolicy: &istiov1alpha3.TrafficPolicy{
					LoadBalancer: &istiov1alpha3.LoadBalancerSettings{
						LbPolicy: &istiov1alpha3.LoadBalancerSettings_Simple{
							Simple: istiov1alpha3.LoadBalancerSettings_PASSTHROUGH,
						},
					},
				},
			}},
		},
	}
}
