/*
Copyright 2019 The Knative Authors.

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
	"fmt"
	"net/url"
	"sort"
	"strconv"

	"go.uber.org/zap"
	istiov1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	istiolisters "knative.dev/net-istio/pkg/client/istio/listers/networking/v1beta1"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/pkg/ingress"
	"knative.dev/networking/pkg/k8s"
	"knative.dev/networking/pkg/status"
)

func NewProbeTargetLister(
	logger *zap.SugaredLogger,
	gatewayLister istiolisters.GatewayLister,
	endpointsLister corev1listers.EndpointsLister,
	serviceLister corev1listers.ServiceLister) status.ProbeTargetLister {
	return &gatewayPodTargetLister{
		logger:          logger,
		gatewayLister:   gatewayLister,
		endpointsLister: endpointsLister,
		serviceLister:   serviceLister,
	}
}

type gatewayPodTargetLister struct {
	logger *zap.SugaredLogger

	gatewayLister   istiolisters.GatewayLister
	endpointsLister corev1listers.EndpointsLister
	serviceLister   corev1listers.ServiceLister
}

func (l *gatewayPodTargetLister) ListProbeTargets(ctx context.Context, ing *v1alpha1.Ingress) ([]status.ProbeTarget, error) {
	results := []status.ProbeTarget{}
	hostsByGateway := ingress.HostsPerVisibility(ing, qualifiedGatewayNamesFromContext(ctx))
	gatewayNames := make([]string, 0, len(hostsByGateway))
	for gatewayName := range hostsByGateway {
		gatewayNames = append(gatewayNames, gatewayName)
	}

	// Sort the gateway names for a consistent ordering.
	sort.Strings(gatewayNames)
	for _, gatewayName := range gatewayNames {
		gateway, err := l.getGateway(gatewayName)
		if err != nil {
			return nil, fmt.Errorf("failed to get Gateway %q: %w", gatewayName, err)
		}
		targets, err := l.listGatewayTargets(gateway)
		if err != nil {
			return nil, fmt.Errorf("failed to list the probing URLs of Gateway %q: %w", gatewayName, err)
		}
		if len(targets) == 0 {
			continue
		}
		for _, target := range targets {
			qualifiedTarget := status.ProbeTarget{
				PodIPs:  target.PodIPs,
				PodPort: target.PodPort,
				Port:    target.Port,
				URLs:    make([]*url.URL, 1),
			}
			// Pick a single host since they all end up being used in the same
			// VirtualService and will be applied atomically by Istio.
			host := hostsByGateway[gatewayName].List()[0]
			newURL := *target.URLs[0]
			newURL.Host = host + ":" + target.Port
			qualifiedTarget.URLs[0] = &newURL
			results = append(results, qualifiedTarget)
		}
	}
	return results, nil
}

func (l *gatewayPodTargetLister) getGateway(name string) (*v1beta1.Gateway, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(name)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Gateway name %q: %w", name, err)
	}
	if namespace == "" {
		return nil, fmt.Errorf("unexpected unqualified Gateway name %q", name)
	}
	return l.gatewayLister.Gateways(namespace).Get(name)
}

// listGatewayPodsURLs returns a probe targets for a given Gateway.
func (l *gatewayPodTargetLister) listGatewayTargets(gateway *v1beta1.Gateway) ([]status.ProbeTarget, error) {
	selector := labels.SelectorFromSet(gateway.Spec.Selector)

	services, err := l.serviceLister.List(selector)
	if err != nil {
		return nil, fmt.Errorf("failed to list Services: %w", err)
	}
	if len(services) == 0 {
		l.logger.Infof("Skipping Gateway %s/%s because it has no corresponding Service", gateway.Namespace, gateway.Name)
		return nil, nil
	}
	service := services[0]

	endpoints, err := l.endpointsLister.Endpoints(service.Namespace).Get(service.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get Endpoints: %w", err)
	}

	seen := sets.NewString()
	targets := []status.ProbeTarget{}
	for _, server := range gateway.Spec.Servers {
		tURL := &url.URL{}
		switch server.Port.Protocol {
		case "HTTP", "HTTP2":
			if server.Tls != nil && server.Tls.HttpsRedirect {
				// ignoring HTTPS redirects.
				continue
			}
			tURL.Scheme = "http"
		case "HTTPS":
			if server.GetTls().GetMode() == istiov1beta1.ServerTLSSettings_MUTUAL {
				l.logger.Infof("Skipping Server %q because HTTPS with TLS mode MUTUAL is not supported", server.Port.Name)
				continue
			}
			tURL.Scheme = "https"
		default:
			l.logger.Infof("Skipping Server %q because protocol %q is not supported", server.Port.Name, server.Port.Protocol)
			continue
		}

		portName, err := k8s.NameForPortNumber(service, int32(server.Port.Number))
		if err != nil {
			l.logger.Infof("Skipping Server %q because Service %s/%s doesn't contain a port %d", server.Port.Name, service.Namespace, service.Name, server.Port.Number)
			continue
		}

		key := server.Port.Protocol + "/" + strconv.Itoa(int(server.Port.Number))
		if seen.Has(key) {
			continue
		}
		seen.Insert(key)

		for _, sub := range endpoints.Subsets {
			// The translation from server.Port.Number -> portName -> portNumber is intentional.
			// We can't simply translate from the Service.Spec because Service.Spec.Target.Port
			// could be either a name or a number.  In the Endpoints spec, all ports are provided
			// as numbers.
			portNumber, err := k8s.PortNumberForName(sub, portName)
			if err != nil {
				l.logger.Infof("Skipping Subset %v because it doesn't contain a port name %q", sub.Addresses, portName)
				continue
			}
			target := status.ProbeTarget{
				PodIPs:  sets.NewString(),
				PodPort: strconv.Itoa(int(portNumber)),
				Port:    strconv.Itoa(int(server.Port.Number)),
				URLs:    []*url.URL{tURL},
			}
			for _, addr := range sub.Addresses {
				target.PodIPs.Insert(addr.IP)
			}
			targets = append(targets, target)
		}
	}
	return targets, nil
}
