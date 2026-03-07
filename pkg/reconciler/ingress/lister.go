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
	"fmt"
	"net/url"
	"sort"
	"strconv"

	"go.uber.org/zap"
	istiov1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	istiolisters "istio.io/client-go/pkg/listers/networking/v1beta1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"knative.dev/net-istio/pkg/reconciler/ingress/config"
	"knative.dev/net-istio/pkg/reconciler/ingress/resources"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/pkg/ingress"
	"knative.dev/networking/pkg/k8s"
	"knative.dev/networking/pkg/status"
)

func NewProbeTargetLister(
	logger *zap.SugaredLogger,
	gatewayLister istiolisters.GatewayLister,
	endpointsLister corev1listers.EndpointsLister,
	serviceLister corev1listers.ServiceLister,
) status.ProbeTargetLister {
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

	// When gateways are explicitly disabled, go directly to mesh-only probing.
	cfg := config.FromContext(ctx)
	if !cfg.Istio.EnableGateways {
		l.logger.Info("Gateways disabled via config, using mesh-only probing")
		meshTargets, err := l.listMeshProbeTargets(ing)
		if err != nil {
			return nil, fmt.Errorf("failed to list mesh probe targets: %w", err)
		}
		return meshTargets, nil
	}

	gatewayQualifiedNames, err := resources.QualifiedGatewayNamesFromContext(ctx, ing)
	if err != nil {
		return nil, fmt.Errorf("failed to get gateways for ingress: %w", err)
	}
	hostsByGateway := ingress.HostsPerVisibility(ing, gatewayQualifiedNames)
	gatewayNames := make([]string, 0, len(hostsByGateway))
	for gatewayName := range hostsByGateway {
		gatewayNames = append(gatewayNames, gatewayName)
	}

	// Sort the gateway names for a consistent ordering.
	sort.Strings(gatewayNames)
	gatewaysFound := false
	for _, gatewayName := range gatewayNames {
		gateway, err := l.getGateway(gatewayName)
		if err != nil {
			if apierrs.IsNotFound(err) {
				l.logger.Infof("Skipping Gateway %q because it doesn't exist", gatewayName)
				continue
			}
			return nil, fmt.Errorf("failed to get Gateway %q: %w", gatewayName, err)
		}
		gatewaysFound = true
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
			host := sets.List(hostsByGateway[gatewayName])[0]
			newURL := *target.URLs[0]
			newURL.Host = host + ":" + target.Port
			qualifiedTarget.URLs[0] = &newURL
			results = append(results, qualifiedTarget)
		}
	}

	// If no gateways were found, fall back to mesh-only mode:
	// probe the destination service pods directly to verify reachability.
	if !gatewaysFound {
		l.logger.Info("No gateways found, falling back to mesh-only probing of destination services")
		meshTargets, err := l.listMeshProbeTargets(ing)
		if err != nil {
			return nil, fmt.Errorf("failed to list mesh probe targets: %w", err)
		}
		results = append(results, meshTargets...)
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
	selector := labels.SelectorFromSet(gateway.Spec.GetSelector())

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

	seen := sets.New[string]()
	targets := []status.ProbeTarget{}
	for _, server := range gateway.Spec.GetServers() {
		tURL := &url.URL{}
		switch server.GetPort().GetProtocol() {
		case "HTTP", "HTTP2":
			if server.GetTls() != nil && server.GetTls().GetHttpsRedirect() {
				// ignoring HTTPS redirects.
				continue
			}
			tURL.Scheme = "http"
		case "HTTPS":
			if server.GetTls().GetMode() == istiov1beta1.ServerTLSSettings_MUTUAL {
				l.logger.Infof("Skipping Server %q because HTTPS with TLS mode MUTUAL is not supported", server.GetPort().GetName())
				continue
			}
			tURL.Scheme = "https"
		default:
			l.logger.Infof("Skipping Server %q because protocol %q is not supported", server.GetPort().GetName(), server.GetPort().GetProtocol())
			continue
		}

		//nolint:gosec // ignore integer overflow
		portName, err := k8s.NameForPortNumber(service, int32(server.GetPort().GetNumber()))
		if err != nil {
			l.logger.Infof("Skipping Server %q because Service %s/%s doesn't contain a port %d", server.GetPort().GetName(), service.Namespace, service.Name, server.GetPort().GetNumber())
			continue
		}

		key := server.GetPort().GetProtocol() + "/" + strconv.Itoa(int(server.GetPort().GetNumber()))
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
				PodIPs:  sets.New[string](),
				PodPort: strconv.Itoa(int(portNumber)),
				Port:    strconv.Itoa(int(server.GetPort().GetNumber())),
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

// listMeshProbeTargets builds probe targets from the Ingress spec's destination
// services when no gateways are available (mesh-only mode). It resolves each
// backend service to its Endpoints and probes the pods directly.
func (l *gatewayPodTargetLister) listMeshProbeTargets(ing *v1alpha1.Ingress) ([]status.ProbeTarget, error) {
	results := []status.ProbeTarget{}
	seen := sets.New[string]()

	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil || len(rule.Hosts) == 0 {
			continue
		}
		// Use the first host for the probe URL Host header.
		host := rule.Hosts[0]

		for _, path := range rule.HTTP.Paths {
			for _, split := range path.Splits {
				key := split.ServiceNamespace + "/" + split.ServiceName + ":" + split.ServicePort.String()
				if seen.Has(key) {
					continue
				}
				seen.Insert(key)

				svc, err := l.serviceLister.Services(split.ServiceNamespace).Get(split.ServiceName)
				if err != nil {
					l.logger.Infof("Skipping service %s/%s for mesh probing: failed to get Service: %v",
						split.ServiceNamespace, split.ServiceName, err)
					continue
				}

				// Resolve the service port to a port name and number.
				var portName string
				var svcPortNumber int32
				if split.ServicePort.Type == intstr.Int {
					svcPortNumber = int32(split.ServicePort.IntValue())
					portName, err = k8s.NameForPortNumber(svc, svcPortNumber)
					if err != nil {
						l.logger.Infof("Skipping service %s/%s port %d for mesh probing: %v",
							split.ServiceNamespace, split.ServiceName, svcPortNumber, err)
						continue
					}
				} else {
					portName = split.ServicePort.String()
					for _, p := range svc.Spec.Ports {
						if p.Name == portName {
							svcPortNumber = p.Port
							break
						}
					}
					if svcPortNumber == 0 {
						l.logger.Infof("Skipping service %s/%s port %q for mesh probing: port name not found in Service",
							split.ServiceNamespace, split.ServiceName, portName)
						continue
					}
				}

				endpoints, err := l.endpointsLister.Endpoints(split.ServiceNamespace).Get(split.ServiceName)
				if err != nil {
					l.logger.Infof("Skipping service %s/%s for mesh probing: failed to get Endpoints: %v",
						split.ServiceNamespace, split.ServiceName, err)
					continue
				}

				svcPort := strconv.Itoa(int(svcPortNumber))
				for _, sub := range endpoints.Subsets {
					podPort, err := k8s.PortNumberForName(sub, portName)
					if err != nil {
						l.logger.Infof("Skipping Subset for service %s/%s: port name %q not found in Endpoints",
							split.ServiceNamespace, split.ServiceName, portName)
						continue
					}
					target := status.ProbeTarget{
						PodIPs:  sets.New[string](),
						PodPort: strconv.Itoa(int(podPort)),
						Port:    svcPort,
						URLs: []*url.URL{{
							Scheme: "http",
							Host:   host + ":" + svcPort,
						}},
					}
					for _, addr := range sub.Addresses {
						target.PodIPs.Insert(addr.IP)
					}
					results = append(results, target)
				}
			}
		}
	}
	return results, nil
}
