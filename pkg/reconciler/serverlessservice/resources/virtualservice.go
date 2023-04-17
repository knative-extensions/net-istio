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

	istiov1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/pkg/http/header"
	"knative.dev/pkg/kmeta"
	pkgnetwork "knative.dev/pkg/network"
)

// MakeVirtualService creates a placeholder virtual service to allow direct
// pod addressability, even for mesh cases.
func MakeVirtualService(sks *v1alpha1.ServerlessService) *v1beta1.VirtualService {
	ns := sks.Namespace
	name := sks.Status.PrivateServiceName
	host := pkgnetwork.GetServiceHostname(name, ns)

	return &v1beta1.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       sks.Namespace,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(sks)},
		},
		Spec: istiov1beta1.VirtualService{
			Hosts: []string{host},
			Http: []*istiov1beta1.HTTPRoute{{
				Match: []*istiov1beta1.HTTPMatchRequest{{
					Headers: map[string]*istiov1beta1.StringMatch{
						header.PassthroughLoadbalancingKey: {
							MatchType: &istiov1beta1.StringMatch_Exact{
								Exact: "true",
							},
						},
					},
				}},
				Route: []*istiov1beta1.HTTPRouteDestination{{
					Destination: &istiov1beta1.Destination{
						Host:   host,
						Subset: subsetDirect,
					},
				}},
			}, {
				Route: []*istiov1beta1.HTTPRouteDestination{{
					Destination: &istiov1beta1.Destination{
						Host:   host,
						Subset: subsetNormal,
					},
				}},
			}},
		},
	}
}
