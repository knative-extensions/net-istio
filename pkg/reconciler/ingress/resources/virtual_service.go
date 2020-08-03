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
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/gogo/protobuf/types"
	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"knative.dev/net-istio/pkg/reconciler/ingress/config"
	"knative.dev/net-istio/pkg/reconciler/ingress/resources/names"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/pkg/ingress"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/network"
	"knative.dev/pkg/system"
)

var retriableConditions = strings.Join([]string{
	"5xx",
	"connect-failure",
	"refused-stream",
	"cancelled",
	"resource-exhausted",
	"retriable-status-codes"}, ",")

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
func MakeIngressVirtualService(ctx context.Context, ing *v1alpha1.Ingress, gateways map[v1alpha1.IngressVisibility]sets.String) *v1alpha3.VirtualService {
	vs := &v1alpha3.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.IngressVirtualService(ing),
			Namespace:       VirtualServiceNamespace(ing),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing)},
			Annotations:     ing.GetAnnotations(),
		},
		Spec: *makeVirtualServiceSpec(ctx, ing, gateways, ingress.ExpandedHosts(getHosts(ing))),
	}

	// Populate the Ingress labels.
	vs.Labels = kmeta.FilterMap(ing.GetLabels(), func(k string) bool {
		return k != RouteLabelKey && k != RouteNamespaceLabelKey
	})
	vs.Labels[networking.IngressLabelKey] = ing.Name
	return vs
}

// MakeMeshVirtualService creates a mesh Virtual Service
func MakeMeshVirtualService(ctx context.Context, ing *v1alpha1.Ingress, gateways map[v1alpha1.IngressVisibility]sets.String) *v1alpha3.VirtualService {
	hosts := keepLocalHostnames(getHosts(ing))
	// If cluster local gateway is configured, we need to expand hosts because of
	// https://github.com/knative/serving/issues/6488#issuecomment-573513768.
	if len(gateways[v1alpha1.IngressVisibilityClusterLocal]) != 0 {
		hosts = ingress.ExpandedHosts(hosts)
	}
	if len(hosts) == 0 {
		return nil
	}
	vs := &v1alpha3.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.MeshVirtualService(ing),
			Namespace:       VirtualServiceNamespace(ing),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing)},
			Annotations:     ing.GetAnnotations(),
		},
		Spec: *makeVirtualServiceSpec(ctx, ing, map[v1alpha1.IngressVisibility]sets.String{
			v1alpha1.IngressVisibilityExternalIP:   sets.NewString("mesh"),
			v1alpha1.IngressVisibilityClusterLocal: sets.NewString("mesh"),
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
func MakeVirtualServices(ctx context.Context, ing *v1alpha1.Ingress, gateways map[v1alpha1.IngressVisibility]sets.String) ([]*v1alpha3.VirtualService, error) {
	// Insert probe header
	ing = ing.DeepCopy()
	if _, err := ingress.InsertProbe(ing); err != nil {
		return nil, fmt.Errorf("failed to insert a probe into the Ingress: %w", err)
	}
	vss := []*v1alpha3.VirtualService{}
	if meshVs := MakeMeshVirtualService(ctx, ing, gateways); meshVs != nil {
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
		vss = append(vss, MakeIngressVirtualService(ctx, ing, gateways))
	}

	return vss, nil
}

func makeVirtualServiceSpec(ctx context.Context, ing *v1alpha1.Ingress, gateways map[v1alpha1.IngressVisibility]sets.String, hosts sets.String) *istiov1alpha3.VirtualService {
	spec := istiov1alpha3.VirtualService{
		Hosts: hosts.List(),
	}

	gw := sets.String{}
	for _, rule := range ing.Spec.Rules {
		for _, p := range rule.HTTP.Paths {
			hosts := hosts.Intersection(sets.NewString(rule.Hosts...))
			if hosts.Len() != 0 {
				http := makeVirtualServiceRoute(ctx, hosts, &p, gateways, rule.Visibility)
				// Add all the Gateways that exist inside the http.match section of
				// the VirtualService.
				// This ensures that we are only using the Gateways that actually appear
				// in VirtualService routes.
				for _, m := range http.Match {
					gw = gw.Union(sets.NewString(m.Gateways...))
				}
				spec.Http = append(spec.Http, http)
			}
		}
	}
	spec.Gateways = gw.List()
	return &spec
}

func makeVirtualServiceRoute(ctx context.Context, hosts sets.String, http *v1alpha1.HTTPIngressPath, gateways map[v1alpha1.IngressVisibility]sets.String, visibility v1alpha1.IngressVisibility) *istiov1alpha3.HTTPRoute {
	matches := []*istiov1alpha3.HTTPMatchRequest{}
	clusterDomainName := network.GetClusterDomainName()
	for _, host := range hosts.List() {
		g := gateways[visibility]
		if strings.HasSuffix(host, clusterDomainName) && len(gateways[v1alpha1.IngressVisibilityClusterLocal]) > 0 {
			// For local hostname, always use private gateway
			g = gateways[v1alpha1.IngressVisibilityClusterLocal]
		}
		matches = append(matches, makeMatch(host, http.Path, http.Headers, g))
	}

	weights := []*istiov1alpha3.HTTPRouteDestination{}
	for _, split := range http.Splits {
		var h *istiov1alpha3.Headers
		if len(split.AppendHeaders) > 0 {
			h = &istiov1alpha3.Headers{
				Request: &istiov1alpha3.Headers_HeaderOperations{
					Set: split.AppendHeaders,
				},
			}
		}

		weights = append(weights, &istiov1alpha3.HTTPRouteDestination{
			Destination: &istiov1alpha3.Destination{
				Host: network.GetServiceHostname(
					split.ServiceName, split.ServiceNamespace),
				Port: &istiov1alpha3.PortSelector{
					Number: uint32(split.ServicePort.IntValue()),
				},
			},
			Weight:  int32(split.Percent),
			Headers: h,
		})
	}

	var h *istiov1alpha3.Headers
	if len(http.AppendHeaders) > 0 {
		h = &istiov1alpha3.Headers{
			Request: &istiov1alpha3.Headers_HeaderOperations{
				Set: http.AppendHeaders,
			},
		}
	}

	var rewrite *istiov1alpha3.HTTPRewrite
	if http.RewriteHost != "" {
		rewrite = &istiov1alpha3.HTTPRewrite{
			Authority: http.RewriteHost,
		}

		weights = []*istiov1alpha3.HTTPRouteDestination{{
			Weight: 100,
			Destination: &istiov1alpha3.Destination{
				Host: privateGatewayServiceURLFromContext(ctx),
			},
		}}
	}

	route := &istiov1alpha3.HTTPRoute{
		Match:   matches,
		Route:   weights,
		Rewrite: rewrite,
		Headers: h,
	}
	if http.Timeout != nil {
		route.Timeout = types.DurationProto(http.Timeout.Duration)
	}
	route.Retries = &istiov1alpha3.HTTPRetry{}
	if http.Retries != nil && http.Retries.Attempts > 0 {
		route.Retries = &istiov1alpha3.HTTPRetry{
			RetryOn:  retriableConditions,
			Attempts: int32(http.Retries.Attempts),
		}
		if http.Retries.PerTryTimeout != nil {
			route.Retries.PerTryTimeout = types.DurationProto(http.Retries.PerTryTimeout.Duration)
		}
	}
	return route
}

func keepLocalHostnames(hosts sets.String) sets.String {
	localSvcSuffix := ".svc." + network.GetClusterDomainName()
	retained := sets.NewString()
	for _, h := range hosts.List() {
		if strings.HasSuffix(h, localSvcSuffix) {
			retained.Insert(h)
		}
	}
	return retained
}

func makeMatch(host string, pathRegExp string, headers map[string]v1alpha1.HeaderMatch, gateways sets.String) *istiov1alpha3.HTTPMatchRequest {
	match := &istiov1alpha3.HTTPMatchRequest{
		Gateways: gateways.List(),
		Authority: &istiov1alpha3.StringMatch{
			// Do not use Regex as Istio 1.4 or later has 100 bytes limitation.
			MatchType: &istiov1alpha3.StringMatch_Prefix{Prefix: hostPrefix(host)},
		},
	}
	// Empty pathRegExp is considered match all path. We only need to
	// consider pathRegExp when it's non-empty.
	if pathRegExp != "" {
		match.Uri = &istiov1alpha3.StringMatch{
			MatchType: &istiov1alpha3.StringMatch_Regex{Regex: pathRegExp},
		}
	}

	for k, v := range headers {
		match.Headers = map[string]*istiov1alpha3.StringMatch{
			k: {
				MatchType: &istiov1alpha3.StringMatch_Exact{
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

func getHosts(ing *v1alpha1.Ingress) sets.String {
	hosts := sets.NewString()
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

func privateGatewayServiceURLFromContext(ctx context.Context) string {
	cfg := config.FromContext(ctx).Istio
	if len(cfg.LocalGateways) > 0 {
		return cfg.LocalGateways[0].ServiceURL
	}

	return ""
}
