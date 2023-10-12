/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
	"knative.dev/pkg/network"
	"knative.dev/pkg/system"
)

const (
	// IstioConfigName is the name of the configmap containing all
	// customizations for istio related features.
	IstioConfigName = "config-istio"

	// gatewayKeyPrefix is the prefix of all keys to configure Istio gateways for public Ingresses.
	gatewayKeyPrefix = "gateway."

	// localGatewayKeyPrefix is the prefix of all keys to configure Istio gateways for public & private Ingresses.
	localGatewayKeyPrefix = "local-gateway."

	// expositionKeyPrefix is the prefix of all keys to filter on Istio gateways.
	expositionKeyPrefix = "exposition."

	// KnativeIngressGateway is the name of the ingress gateway
	KnativeIngressGateway = "knative-ingress-gateway"

	// KnativeLocalGateway is the name of the local gateway
	KnativeLocalGateway = "knative-local-gateway"

	// IstioIngressGateway is the name of the Istio ingress gateway
	IstioIngressGateway = "istio-ingressgateway"

	// IstioNamespace is the namespace containing Istio
	IstioNamespace = "istio-system"
)

func defaultIngressGateways() []Gateway {
	return []Gateway{{
		Namespace:  system.Namespace(),
		Name:       KnativeIngressGateway,
		ServiceURL: network.GetServiceHostname(IstioIngressGateway, IstioNamespace),
	}}
}

func defaultLocalGateways() []Gateway {
	return []Gateway{{
		Namespace:  system.Namespace(),
		Name:       KnativeLocalGateway,
		ServiceURL: network.GetServiceHostname(KnativeLocalGateway, IstioNamespace),
	}}
}

// Gateway specifies the name of the Gateway and the K8s Service backing it.
type Gateway struct {
	Namespace   string
	Name        string
	ServiceURL  string
	Expositions sets.Set[string]
}

// QualifiedName returns gateway name in '{namespace}/{name}' format.
func (g Gateway) QualifiedName() string {
	return g.Namespace + "/" + g.Name
}

// Istio contains istio related configuration defined in the
// istio config map.
type Istio struct {
	// IngressGateway specifies the gateway urls for public Ingress.
	IngressGateways []Gateway

	// LocalGateway specifies the gateway urls for public & private Ingress.
	LocalGateways []Gateway
}

func parseGateways(configMap *corev1.ConfigMap, prefix string) ([]Gateway, error) {
	urls := map[string]string{}
	gatewayNames := []string{}
	for k, v := range configMap.Data {
		if !strings.HasPrefix(k, prefix) || k == prefix {
			continue
		}
		gatewayName, serviceURL := k[len(prefix):], v
		if errs := validation.IsDNS1123Subdomain(strings.TrimSuffix(serviceURL, ".")); len(errs) > 0 {
			return nil, fmt.Errorf("invalid gateway format: %v", errs)
		}
		gatewayNames = append(gatewayNames, gatewayName)
		urls[gatewayName] = serviceURL
	}

	expositions := parseExposition(configMap)

	sort.Strings(gatewayNames)
	gateways := make([]Gateway, len(gatewayNames))
	for i, gatewayName := range gatewayNames {
		var namespace, name string
		parts := strings.SplitN(gatewayName, ".", 2)
		if len(parts) == 1 {
			namespace = system.Namespace()
			name = parts[0]
		} else {
			namespace = parts[0]
			name = parts[1]
		}
		gateways[i] = Gateway{
			Namespace:  namespace,
			Name:       name,
			ServiceURL: urls[gatewayName],
		}

		if expositionKeys, ok := expositions[fmt.Sprintf("%s.%s", namespace, name)]; ok {
			gateways[i].Expositions = expositionKeys
		}
	}
	return gateways, nil
}

func parseExposition(configMap *corev1.ConfigMap) map[string]sets.Set[string] {
	ret := make(map[string]sets.Set[string])

	for k, v := range configMap.Data {
		if !strings.HasPrefix(k, expositionKeyPrefix) || k == expositionKeyPrefix {
			continue
		}

		gatewayName, expositionKeys := k[len(expositionKeyPrefix):], v

		if !strings.Contains(gatewayName, ".") {
			gatewayName = fmt.Sprintf("%s.%s", system.Namespace(), gatewayName)
		}

		expositions := strings.Split(expositionKeys, ",")
		for _, expo := range expositions {
			toAdd := strings.TrimSpace(expo)
			if len(toAdd) == 0 {
				continue
			}
			if _, ok := ret[gatewayName]; !ok {
				ret[gatewayName] = sets.New[string]()
			}

			ret[gatewayName] = ret[gatewayName].Insert(toAdd)
		}
	}

	return ret
}

// NewIstioFromConfigMap creates an Istio config from the supplied ConfigMap
func NewIstioFromConfigMap(configMap *corev1.ConfigMap) (*Istio, error) {
	gateways, err := parseGateways(configMap, gatewayKeyPrefix)
	if err != nil {
		return nil, err
	}
	if len(gateways) == 0 {
		gateways = defaultIngressGateways()
	}
	localGateways, err := parseGateways(configMap, localGatewayKeyPrefix)
	if err != nil {
		return nil, err
	}
	if len(localGateways) == 0 {
		localGateways = defaultLocalGateways()
	}

	return &Istio{
		IngressGateways: gateways,
		LocalGateways:   localGateways,
	}, nil
}
