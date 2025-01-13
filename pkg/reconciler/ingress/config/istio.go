/*
Copyright 2018 The Knative Authors

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

package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"knative.dev/pkg/network"
	"knative.dev/pkg/system"
	"sigs.k8s.io/yaml"
)

const (
	// IstioConfigName is the name of the configmap containing all
	// customizations for istio related features.
	IstioConfigName = "config-istio"

	// gatewayKeyPrefix is the prefix of all keys to configure Istio gateways for public Ingresses.
	gatewayKeyPrefix = "gateway."

	// localGatewayKeyPrefix is the prefix of all keys to configure Istio gateways for public & private Ingresses.
	localGatewayKeyPrefix = "local-gateway."

	// externalGatewaysKey is the configmap key to configure Istio gateways for public Ingresses.
	externalGatewaysKey = "external-gateways"

	// localGatewaysKey is the configmap key to configure Istio gateways for private Ingresses.
	localGatewaysKey = "local-gateways"

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
	Namespace     string
	Name          string
	ServiceURL    string                `json:"service"`
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// QualifiedName returns gateway name in '{namespace}/{name}' format.
func (g Gateway) QualifiedName() string {
	return g.Namespace + "/" + g.Name
}

func (g Gateway) Validate() error {
	if g.Namespace == "" {
		return errors.New("missing namespace")
	}

	if g.Name == "" {
		return errors.New("missing name")
	}

	if g.ServiceURL == "" {
		return errors.New("missing service")
	}

	if errs := validation.IsDNS1123Subdomain(strings.TrimSuffix(g.ServiceURL, ".")); len(errs) > 0 {
		return fmt.Errorf("invalid gateway service format: %v", errs)
	}

	if _, err := metav1.LabelSelectorAsSelector(g.LabelSelector); err != nil {
		return fmt.Errorf("failed to create selector from label selector: %w", err)
	}

	return nil
}

// Istio contains istio related configuration defined in the
// istio config map.
type Istio struct {
	// IngressGateways specifies the gateway urls for public Ingress.
	IngressGateways []Gateway

	// LocalGateways specifies the gateway urls for public & private Ingress.
	LocalGateways []Gateway
}

func (i Istio) Validate() error {
	for _, gtw := range i.IngressGateways {
		if err := gtw.Validate(); err != nil {
			return fmt.Errorf("invalid gateway %s: %w", gtw.QualifiedName(), err)
		}
	}

	for _, gtw := range i.LocalGateways {
		if err := gtw.Validate(); err != nil {
			return fmt.Errorf("invalid local gateway %s: %w", gtw.QualifiedName(), err)
		}
	}

	return nil
}

// DefaultExternalGateways returns the external gateway without any label selector
func (i Istio) DefaultExternalGateways() []Gateway {
	return defaultGateways(i.IngressGateways)
}

// DefaultLocalGateways returns the local gateway without any label selector
func (i Istio) DefaultLocalGateways() []Gateway {
	return defaultGateways(i.LocalGateways)
}

func defaultGateways(gtws []Gateway) []Gateway {
	ret := make([]Gateway, 0)

	for _, gtw := range gtws {
		gateway := gtw

		if gtw.LabelSelector == nil {
			ret = append(ret, gateway)
		}
	}

	return ret
}

// NewIstioFromConfigMap creates an Istio config from the supplied ConfigMap
func NewIstioFromConfigMap(configMap *corev1.ConfigMap) (*Istio, error) {
	ret := &Istio{}
	var err error

	oldFormatDefined := isOldFormatDefined(configMap)
	newFormatDefined := isNewFormatDefined(configMap)

	switch {
	case newFormatDefined && oldFormatDefined:
		return nil, fmt.Errorf(
			"invalid configmap: %q or %q can not be defined simultaneously with %q or %q",
			localGatewaysKey, externalGatewaysKey, gatewayKeyPrefix, localGatewayKeyPrefix,
		)
	case newFormatDefined:
		ret, err = parseNewFormat(configMap)
		if err != nil {
			return nil, fmt.Errorf("failed to parse configmap: %w", err)
		}
	case oldFormatDefined:
		ret = parseOldFormat(configMap)
	default:
		defaultValues(ret)
	}

	err = ret.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return ret, nil
}

func isNewFormatDefined(configMap *corev1.ConfigMap) bool {
	_, hasGateway := configMap.Data[externalGatewaysKey]
	_, hasLocalGateway := configMap.Data[localGatewaysKey]

	return hasGateway || hasLocalGateway
}

func isOldFormatDefined(configMap *corev1.ConfigMap) bool {
	for key := range configMap.Data {
		if strings.HasPrefix(key, gatewayKeyPrefix) || strings.HasPrefix(key, localGatewayKeyPrefix) {
			return true
		}
	}

	return false
}

func parseNewFormat(configMap *corev1.ConfigMap) (*Istio, error) {
	ret := &Istio{}

	gatewaysStr, hasGateway := configMap.Data[externalGatewaysKey]

	if hasGateway {
		gateways, err := parseNewFormatGateways(gatewaysStr)
		if err != nil {
			return ret, fmt.Errorf("failed to parse %q gateways: %w", externalGatewaysKey, err)
		}

		ret.IngressGateways = gateways
	}

	localGatewaysStr, hasLocalGateway := configMap.Data[localGatewaysKey]

	if hasLocalGateway {
		localGateways, err := parseNewFormatGateways(localGatewaysStr)
		if err != nil {
			return ret, fmt.Errorf("failed to parse %q gateways: %w", localGatewaysKey, err)
		}

		ret.LocalGateways = localGateways
	}

	defaultValues(ret)

	if len(ret.DefaultExternalGateways()) != 1 {
		return ret, fmt.Errorf("exactly one external gateway with no selector can be defined, here: %v", ret.DefaultExternalGateways())
	}

	if len(ret.DefaultLocalGateways()) != 1 {
		return ret, fmt.Errorf("exactly one local gateway with no selector can be defined, here: %v", ret.DefaultLocalGateways())
	}

	return ret, nil
}

func parseNewFormatGateways(data string) ([]Gateway, error) {
	ret := make([]Gateway, 0)

	err := yaml.Unmarshal([]byte(data), &ret)
	if err != nil {
		return ret, fmt.Errorf("failed to unmarshal: %w", err)
	}

	return ret, nil
}

func parseOldFormat(configMap *corev1.ConfigMap) *Istio {
	ret := &Istio{
		IngressGateways: parseOldFormatGateways(configMap, gatewayKeyPrefix),
		LocalGateways:   parseOldFormatGateways(configMap, localGatewayKeyPrefix),
	}

	defaultValues(ret)

	return ret
}

func parseOldFormatGateways(configMap *corev1.ConfigMap, prefix string) []Gateway {
	urls := map[string]string{}
	gatewayNames := []string{}
	for k, v := range configMap.Data {
		if !strings.HasPrefix(k, prefix) || k == prefix {
			continue
		}
		gatewayName, serviceURL := k[len(prefix):], v

		gatewayNames = append(gatewayNames, gatewayName)
		urls[gatewayName] = serviceURL
	}
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
	}
	return gateways
}

func defaultValues(conf *Istio) {
	if len(conf.IngressGateways) == 0 {
		conf.IngressGateways = defaultIngressGateways()
	}

	if len(conf.LocalGateways) == 0 {
		conf.LocalGateways = defaultLocalGateways()
	}
}
