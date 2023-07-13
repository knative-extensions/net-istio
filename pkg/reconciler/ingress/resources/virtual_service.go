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

package resources

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	istiov1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	"knative.dev/net-istio/pkg/reconciler/ingress/resources/names"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/pkg/ingress"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/network"
	"knative.dev/pkg/system"
)

// VirtualServiceNamespace gives the namespace of the child
// VirtualServices for a given Ingress.
func VirtualServiceNamespace(ing *v1alpha1.Ingress) string {
	if len(ing.GetNamespace()) == 0 {
		return system.Namespace()
	}
	return ing.GetNamespace()
}

// MakeIngressVirtualService creates Istio VirtualService as network
// programming for Istio Gateways other than 'mesh'.
func MakeIngressVirtualService(ing *v1alpha1.Ingress, gateways map[v1alpha1.IngressVisibility]sets.Set[string]) *v1beta1.VirtualService {
	vs := &v1beta1.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.IngressVirtualService(ing),
			Namespace:       VirtualServiceNamespace(ing),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing)},
			Annotations:     ing.GetAnnotations(),
		},
		Spec: *makeVirtualServiceSpec(ing, gateways, expandedHosts(getHosts(ing))),
	}

	// Populate the Ingress labels.
	vs.Labels = kmeta.FilterMap(ing.GetLabels(), func(k string) bool {
		return k != RouteLabelKey && k != RouteNamespaceLabelKey
	})
	vs.Labels[networking.IngressLabelKey] = ing.Name
	return vs
}

// MakeMeshVirtualService creates a mesh Virtual Service
func MakeMeshVirtualService(ing *v1alpha1.Ingress, gateways map[v1alpha1.IngressVisibility]sets.Set[string]) *v1beta1.VirtualService {
	hosts := keepLocalHostnames(getHosts(ing))
	// If cluster local gateway is configured, we need to expand hosts because of
	// https://github.com/knative/serving/issues/6488#issuecomment-573513768.
	if len(gateways[v1alpha1.IngressVisibilityClusterLocal]) != 0 {
		hosts = expandedHosts(hosts)
	}
	if len(hosts) == 0 {
		return nil
	}
	vs := &v1beta1.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.MeshVirtualService(ing),
			Namespace:       VirtualServiceNamespace(ing),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing)},
			Annotations:     ing.GetAnnotations(),
		},
		Spec: *makeVirtualServiceSpec(ing, map[v1alpha1.IngressVisibility]sets.Set[string]{
			v1alpha1.IngressVisibilityExternalIP:   sets.New("mesh"),
			v1alpha1.IngressVisibilityClusterLocal: sets.New("mesh"),
		}, hosts),
	}
	// Populate the Ingress labels.
	vs.Labels = kmeta.FilterMap(ing.GetLabels(), func(k string) bool {
		return k != RouteLabelKey && k != RouteNamespaceLabelKey
	})
	vs.Labels[networking.IngressLabelKey] = ing.Name
	return vs
}

// MakeVirtualServices creates a mesh VirtualService and a virtual service for each gateway
func MakeVirtualServices(ing *v1alpha1.Ingress, gateways map[v1alpha1.IngressVisibility]sets.Set[string]) ([]*v1beta1.VirtualService, error) {
	// Insert probe header
	ing = ing.DeepCopy()
	if _, err := ingress.InsertProbe(ing); err != nil {
		return nil, fmt.Errorf("failed to insert a probe into the Ingress: %w", err)
	}
	vss := []*v1beta1.VirtualService{}
	if meshVs := MakeMeshVirtualService(ing, gateways); meshVs != nil {
		vss = append(vss, meshVs)
	}
	requiredGatewayCount := 0
	if len(getPublicIngressRules(ing)) > 0 {
		requiredGatewayCount += gateways[v1alpha1.IngressVisibilityExternalIP].Len()
	}

	if len(getClusterLocalIngressRules(ing)) > 0 {
		requiredGatewayCount += gateways[v1alpha1.IngressVisibilityClusterLocal].Len()
	}

	if requiredGatewayCount > 0 {
		vss = append(vss, MakeIngressVirtualService(ing, gateways))
	}

	return vss, nil
}

func makeVirtualServiceSpec(ing *v1alpha1.Ingress, gateways map[v1alpha1.IngressVisibility]sets.Set[string], hosts sets.Set[string]) *istiov1beta1.VirtualService {
	spec := istiov1beta1.VirtualService{
		Hosts: sets.List(hosts),
	}

	gw := sets.New[string]()
	for _, rule := range ing.Spec.Rules {
		for i := range rule.HTTP.Paths {
			p := rule.HTTP.Paths[i]
			hosts := hosts.Intersection(sets.New(rule.Hosts...))
			if hosts.Len() != 0 {
				http := makeVirtualServiceRoute(hosts, &p, gateways, rule.Visibility)
				// Add all the Gateways that exist inside the http.match section of
				// the VirtualService.
				// This ensures that we are only using the Gateways that actually appear
				// in VirtualService routes.
				for _, m := range http.Match {
					gw = gw.Union(sets.New(m.Gateways...))
				}
				spec.Http = append(spec.Http, http)
			}
		}
	}
	spec.Gateways = sets.List(gw)
	return &spec
}

func makeVirtualServiceRoute(hosts sets.Set[string], http *v1alpha1.HTTPIngressPath, gateways map[v1alpha1.IngressVisibility]sets.Set[string], visibility v1alpha1.IngressVisibility) *istiov1beta1.HTTPRoute {
	matches := []*istiov1beta1.HTTPMatchRequest{}
	// Deduplicate hosts to avoid excessive matches, which cause a combinatorial expansion in Istio
	distinctHosts := getDistinctHostPrefixes(hosts)

	for _, host := range sets.List(distinctHosts) {
		matches = append(matches, makeMatch(host, http.Path, http.Headers, gateways[visibility]))
	}

	weights := []*istiov1beta1.HTTPRouteDestination{}
	for _, split := range http.Splits {
		var h *istiov1beta1.Headers
		if len(split.AppendHeaders) > 0 {
			h = &istiov1beta1.Headers{
				Request: &istiov1beta1.Headers_HeaderOperations{
					Set: split.AppendHeaders,
				},
			}
		}

		weights = append(weights, &istiov1beta1.HTTPRouteDestination{
			Destination: &istiov1beta1.Destination{
				Host: network.GetServiceHostname(
					split.ServiceName, split.ServiceNamespace),
				Port: &istiov1beta1.PortSelector{
					Number: uint32(split.ServicePort.IntValue()),
				},
			},
			Weight:  int32(split.Percent),
			Headers: h,
		})
	}

	var h *istiov1beta1.Headers
	if len(http.AppendHeaders) > 0 {
		h = &istiov1beta1.Headers{
			Request: &istiov1beta1.Headers_HeaderOperations{
				Set: http.AppendHeaders,
			},
		}
	}

	var rewrite *istiov1beta1.HTTPRewrite
	if http.RewriteHost != "" {
		rewrite = &istiov1beta1.HTTPRewrite{
			Authority: http.RewriteHost,
		}
	}

	route := &istiov1beta1.HTTPRoute{
		Retries: &istiov1beta1.HTTPRetry{}, // Override default istio behaviour of retrying twice.
		Match:   matches,
		Route:   weights,
		Rewrite: rewrite,
		Headers: h,
	}
	return route
}

// getDistinctHostPrefixes deduplicate a set of prefix matches. For example, the set {a, aabb} can be
// reduced to {a}, as a prefix match on {a} accepts all the same inputs as {a, aabb}.
func getDistinctHostPrefixes(hosts sets.Set[string]) sets.Set[string] {
	// First we sort the list. This ensures that we always process the smallest elements (which match against
	// the most patterns, as they are less specific) first.
	all := sets.List(hosts)
	ns := sets.New[string]()
	for _, h := range all {
		prefixExists := false
		h = hostPrefix(h)
		// For each element, check if any existing elements are a prefix. We only insert if none are
		// For example, if we already have {a} and we are looking at "ab", we would not add it as it has a prefix of "a"
		for e := range ns {
			if strings.HasPrefix(h, e) {
				prefixExists = true
				break
			}
		}
		if !prefixExists {
			ns.Insert(h)
		}
	}
	return ns
}

func keepLocalHostnames(hosts sets.Set[string]) sets.Set[string] {
	localSvcSuffix := ".svc." + network.GetClusterDomainName()
	retained := sets.New[string]()
	for _, h := range sets.List(hosts) {
		if strings.HasSuffix(h, localSvcSuffix) {
			retained.Insert(h)
		}
	}
	return retained
}

func makeMatch(host, path string, headers map[string]v1alpha1.HeaderMatch, gateways sets.Set[string]) *istiov1beta1.HTTPMatchRequest {
	match := &istiov1beta1.HTTPMatchRequest{
		Gateways: sets.List(gateways),
		Authority: &istiov1beta1.StringMatch{
			// Do not use Regex as Istio 1.4 or later has 100 bytes limitation.
			MatchType: &istiov1beta1.StringMatch_Prefix{Prefix: host},
		},
	}
	// Empty path is considered match all path. We only need to consider path
	// when it's non-empty.
	if path != "" {
		match.Uri = &istiov1beta1.StringMatch{
			MatchType: &istiov1beta1.StringMatch_Prefix{Prefix: path},
		}
	}

	for k, v := range headers {
		match.Headers = map[string]*istiov1beta1.StringMatch{
			k: {
				MatchType: &istiov1beta1.StringMatch_Exact{
					Exact: v.Exact,
				},
			},
		}
	}

	return match
}

// hostPrefix returns an host to match either host or host:<any port>.
// For clusterLocalHost, it trims .svc.<local domain> from the host to match short host.
func hostPrefix(host string) string {
	localDomainSuffix := ".svc." + network.GetClusterDomainName()
	if !strings.HasSuffix(host, localDomainSuffix) {
		return host
	}
	return strings.TrimSuffix(host, localDomainSuffix)
}

func getHosts(ing *v1alpha1.Ingress) sets.Set[string] {
	hosts := sets.New[string]()
	for _, rule := range ing.Spec.Rules {
		hosts.Insert(rule.Hosts...)
	}
	return hosts
}

func getClusterLocalIngressRules(i *v1alpha1.Ingress) []v1alpha1.IngressRule {
	var result []v1alpha1.IngressRule
	for _, rule := range i.Spec.Rules {
		if rule.Visibility == v1alpha1.IngressVisibilityClusterLocal {
			result = append(result, rule)
		}
	}

	return result
}

func getPublicIngressRules(i *v1alpha1.Ingress) []v1alpha1.IngressRule {
	var result []v1alpha1.IngressRule
	for _, rule := range i.Spec.Rules {
		if rule.Visibility == v1alpha1.IngressVisibilityExternalIP || rule.Visibility == "" {
			result = append(result, rule)
		}
	}

	return result
}

// Keep me until ingress.ExpandedHosts uses sets.Set[string]
func expandedHosts(hosts sets.Set[string]) sets.Set[string] {
	tmp := sets.NewString(sets.List(hosts)...)

	ret := ingress.ExpandedHosts(tmp)

	return sets.New(ret.List()...)
}
