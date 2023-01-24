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

package resources

import (
	"context"
	"fmt"
	"hash/adler32"
	"regexp"
	"sort"
	"strings"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"knative.dev/net-istio/pkg/reconciler/ingress/config"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/tracker"
)

// GatewayHTTPPort is the HTTP port the gateways listen on.
const (
	GatewayHTTPPort       = 80
	dns1123LabelMaxLength = 63 // Public for testing only.
	dns1123LabelFmt       = "[a-zA-Z0-9](?:[-a-zA-Z0-9]*[a-zA-Z0-9])?"
)

var httpServerPortName = "http-server"

var gatewayGvk = v1alpha3.SchemeGroupVersion.WithKind("Gateway")

// Istio Gateway requires to have at least one server. This placeholderServer is used when
// all of the real servers are deleted.
var placeholderServer = istiov1alpha3.Server{
	Hosts: []string{"place-holder.place-holder"},
	Port: &istiov1alpha3.Port{
		Name:     "place-holder",
		Number:   9999,
		Protocol: "HTTP",
	},
}

var dns1123LabelRegexp = regexp.MustCompile("^" + dns1123LabelFmt + "$")

// GetServers gets the `Servers` from `Gateway` that belongs to the given Ingress.
func GetServers(gateway *v1alpha3.Gateway, ing *v1alpha1.Ingress) []*istiov1alpha3.Server {
	servers := []*istiov1alpha3.Server{}
	for i := range gateway.Spec.Servers {
		if belongsToIngress(gateway.Spec.Servers[i], ing) {
			servers = append(servers, gateway.Spec.Servers[i])
		}
	}
	return SortServers(servers)
}

// GetHTTPServer gets the HTTP `Server` from `Gateway`.
func GetHTTPServer(gateway *v1alpha3.Gateway) *istiov1alpha3.Server {
	for _, server := range gateway.Spec.Servers {
		// The server with "http" port is the default HTTP server.
		if server.Port.Name == httpServerPortName || server.Port.Name == "http" {
			return server
		}
	}
	return nil
}

func belongsToIngress(server *istiov1alpha3.Server, ing *v1alpha1.Ingress) bool {
	// The format of the portName should be "<namespace>/<ingress_name>:<number>".
	// For example, default/routetest:0.
	portNameSplits := strings.Split(server.Port.Name, ":")
	if len(portNameSplits) != 2 {
		return false
	}
	return portNameSplits[0] == portNamePrefix(ing.GetNamespace(), ing.GetName())
}

// SortServers sorts `Server` according to its port name.
func SortServers(servers []*istiov1alpha3.Server) []*istiov1alpha3.Server {
	sort.Slice(servers, func(i, j int) bool {
		return strings.Compare(servers[i].Port.Name, servers[j].Port.Name) < 0
	})
	return servers
}

// MakeIngressTLSGateways creates Gateways that have only TLS servers for a given Ingress.
func MakeIngressTLSGateways(ctx context.Context, ing *v1alpha1.Ingress, ingressTLS []v1alpha1.IngressTLS, originSecrets map[string]*corev1.Secret, svcLister corev1listers.ServiceLister) ([]*v1alpha3.Gateway, error) {
	// No need to create Gateway if there is no related ingress TLS.
	if len(ingressTLS) == 0 {
		return []*v1alpha3.Gateway{}, nil
	}
	gatewayServices, err := getGatewayServices(ctx, svcLister)
	if err != nil {
		return nil, err
	}
	gateways := make([]*v1alpha3.Gateway, len(gatewayServices))
	for i, gatewayService := range gatewayServices {
		servers, err := MakeTLSServers(ing, ing.Spec.TLS, gatewayService.Namespace, originSecrets)
		if err != nil {
			return nil, err
		}
		gateways[i] = makeIngressGateway(ing, gatewayService.Spec.Selector, servers, gatewayService)
	}
	return gateways, nil
}

// MakeIngressGateways creates Gateways with given Servers for a given Ingress.
func MakeIngressGateways(ctx context.Context, ing *v1alpha1.Ingress, servers []*istiov1alpha3.Server, svcLister corev1listers.ServiceLister) ([]*v1alpha3.Gateway, error) {
	gatewayServices, err := getGatewayServices(ctx, svcLister)
	if err != nil {
		return nil, err
	}
	gateways := make([]*v1alpha3.Gateway, len(gatewayServices))
	for i, gatewayService := range gatewayServices {
		if err != nil {
			return nil, err
		}
		gateways[i] = makeIngressGateway(ing, gatewayService.Spec.Selector, servers, gatewayService)
	}
	return gateways, nil
}

// MakeWildcardTLSGateways creates gateways that only contain TLS server with wildcard hosts based on the wildcard secret information.
// For each public ingress service, we will create a list of Gateways. Each Gateway of the list corresponds to a wildcard cert secret.
func MakeWildcardTLSGateways(ctx context.Context, originWildcardSecrets map[string]*corev1.Secret,
	svcLister corev1listers.ServiceLister) ([]*v1alpha3.Gateway, error) {
	if len(originWildcardSecrets) == 0 {
		return []*v1alpha3.Gateway{}, nil
	}
	gatewayServices, err := getGatewayServices(ctx, svcLister)
	if err != nil {
		return nil, err
	}
	gateways := []*v1alpha3.Gateway{}
	for _, gatewayService := range gatewayServices {
		gws, err := makeWildcardTLSGateways(originWildcardSecrets, gatewayService)
		if err != nil {
			return nil, err
		}
		gateways = append(gateways, gws...)
	}
	return gateways, nil
}

func makeWildcardTLSGateways(originWildcardSecrets map[string]*corev1.Secret,
	gatewayService *corev1.Service) ([]*v1alpha3.Gateway, error) {
	gateways := make([]*v1alpha3.Gateway, 0, len(originWildcardSecrets))
	for _, secret := range originWildcardSecrets {
		hosts, err := GetHostsFromCertSecret(secret)
		if err != nil {
			return nil, err
		}
		// If the origin secret is not in the target namespace, then it should have been
		// copied into the target namespace. So we use the name of the copy.
		credentialName := targetWildcardSecretName(secret.Name, secret.Namespace)
		if secret.Namespace == gatewayService.Namespace {
			credentialName = secret.Name
		}
		servers := []*istiov1alpha3.Server{{
			Hosts: hosts,
			Port: &istiov1alpha3.Port{
				Name:     "https",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1alpha3.ServerTLSSettings{
				Mode:              istiov1alpha3.ServerTLSSettings_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
				CredentialName:    credentialName,
				// TODO: Drop this when all supported Istio version uses TLS v1.2 by default.
				MinProtocolVersion: istiov1alpha3.ServerTLSSettings_TLSV1_2,
			},
		}}
		gvk := schema.GroupVersionKind{Version: "v1", Kind: "Secret"}
		gateways = append(gateways, &v1alpha3.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:            WildcardGatewayName(secret.Name, gatewayService.Namespace, gatewayService.Name),
				Namespace:       secret.Namespace,
				OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(secret, gvk)},
			},
			Spec: istiov1alpha3.Gateway{
				Selector: gatewayService.Spec.Selector,
				Servers:  servers,
			},
		})
	}
	return gateways, nil
}

// IsDNS1123Label tests for a string that conforms to the definition of a label in
// DNS (RFC 1123).
// This function is copied from https://github.com/istio/istio/blob/806fb24bc121bf93ea06f6a38b7ccb3d78d1f326/pkg/config/labels/instance.go#L97
// We directly copy this function instead of importing it into vendor and using it because
// if this function is changed in the upstream (for example, Istio allows the dot in the future), we don't want to
// import the change without awareness because it could break the compatibility of Gateway name generation.
func isDNS1123Label(value string) bool {
	return len(value) <= dns1123LabelMaxLength && dns1123LabelRegexp.MatchString(value)
}

// WildcardGatewayName creates the name of wildcard Gateway.
func WildcardGatewayName(secretName, gatewayServiceNamespace, gatewayServiceName string) string {
	return fmt.Sprintf("wildcard-%x", adler32.Checksum([]byte(secretName+"-"+gatewayServiceNamespace+"-"+gatewayServiceName)))
}

// GetQualifiedGatewayNames return the qualified Gateway names for the given Gateways.
func GetQualifiedGatewayNames(gateways []*v1alpha3.Gateway) []string {
	result := make([]string, 0, len(gateways))
	for _, gw := range gateways {
		result = append(result, gw.Namespace+"/"+gw.Name)
	}
	return result
}

// GatewayRef returns the Reference for a give Gateway.
func GatewayRef(gw *v1alpha3.Gateway) tracker.Reference {
	apiVersion, kind := gatewayGvk.ToAPIVersionAndKind()
	return tracker.Reference{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       gw.Name,
		Namespace:  gw.Namespace,
	}
}

func makeIngressGateway(ing *v1alpha1.Ingress, selector map[string]string, servers []*istiov1alpha3.Server, gatewayService *corev1.Service) *v1alpha3.Gateway {
	return &v1alpha3.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:            GatewayName(ing, gatewayService),
			Namespace:       ing.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing)},
			Labels: map[string]string{
				// We need this label to find out all of Gateways of a given Ingress.
				networking.IngressLabelKey: ing.GetName(),
			},
		},
		Spec: istiov1alpha3.Gateway{
			Selector: selector,
			Servers:  servers,
		},
	}
}

func getGatewayServices(ctx context.Context, svcLister corev1listers.ServiceLister) ([]*corev1.Service, error) {
	ingressSvcMetas, err := GetIngressGatewaySvcNameNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	services := make([]*corev1.Service, len(ingressSvcMetas))
	for i, ingressSvcMeta := range ingressSvcMetas {
		svc, err := svcLister.Services(ingressSvcMeta.Namespace).Get(ingressSvcMeta.Name)
		if err != nil {
			return nil, err
		}
		services[i] = svc
	}
	return services, nil
}

// GatewayName create a name for the Gateway that is built based on the given Ingress and bonds to the
// given ingress gateway service.
func GatewayName(accessor kmeta.Accessor, gatewaySvc *corev1.Service) string {
	prefix := accessor.GetName()
	if !isDNS1123Label(prefix) {
		prefix = fmt.Sprint(adler32.Checksum([]byte(prefix)))
	}

	gatewayServiceKey := fmt.Sprintf("%s/%s", gatewaySvc.Namespace, gatewaySvc.Name)
	gatewayServiceKeyChecksum := fmt.Sprint(adler32.Checksum([]byte(gatewayServiceKey)))

	// Ensure that the overall gateway name still is a DNS1123 label
	maxPrefixLength := dns1123LabelMaxLength - len(gatewayServiceKeyChecksum) - 1
	if len(prefix) > maxPrefixLength {
		prefix = prefix[0:maxPrefixLength]
	}

	return fmt.Sprint(prefix+"-", adler32.Checksum([]byte(gatewayServiceKey)))
}

// MakeTLSServers creates the expected Gateway TLS `Servers` based on the given IngressTLS.
func MakeTLSServers(ing *v1alpha1.Ingress, ingressTLS []v1alpha1.IngressTLS, gatewayServiceNamespace string, originSecrets map[string]*corev1.Secret) ([]*istiov1alpha3.Server, error) {
	servers := make([]*istiov1alpha3.Server, len(ingressTLS))
	// TODO(zhiminx): for the hosts that does not included in the IngressTLS but listed in the IngressRule,
	// do we consider them as hosts for HTTP?
	for i, tls := range ingressTLS {
		credentialName := tls.SecretName
		// If the origin secret is not in the target namespace, then it should have been
		// copied into the target namespace. So we use the name of the copy.
		if tls.SecretNamespace != gatewayServiceNamespace {
			originSecret, ok := originSecrets[secretKey(tls)]
			if !ok {
				return nil, fmt.Errorf("unable to get the original secret %s/%s", tls.SecretNamespace, tls.SecretName)
			}
			credentialName = targetSecret(originSecret, ing)
		}

		servers[i] = &istiov1alpha3.Server{
			Hosts: tls.Hosts,
			Port: &istiov1alpha3.Port{
				Name:     fmt.Sprintf(portNamePrefix(ing.GetNamespace(), ing.GetName())+":%d", i),
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: &istiov1alpha3.ServerTLSSettings{
				Mode:              istiov1alpha3.ServerTLSSettings_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
				CredentialName:    credentialName,
				// TODO: Drop this when all supported Istio version uses TLS v1.2 by default.
				MinProtocolVersion: istiov1alpha3.ServerTLSSettings_TLSV1_2,
			},
		}
	}
	return SortServers(servers), nil
}

func portNamePrefix(prefix, suffix string) string {
	if !isDNS1123Label(suffix) {
		suffix = fmt.Sprint(adler32.Checksum([]byte(suffix)))
	}
	return prefix + "/" + suffix
}

// MakeHTTPServer creates a HTTP Gateway `Server` based on the HTTP option
// configuration.
func MakeHTTPServer(httpOption v1alpha1.HTTPOption, hosts []string) *istiov1alpha3.Server {
	// Currently we consider when httpOption is empty, it means HTTP server is disabled.
	// This logic will be deprecated when deprecating "Disabled" HTTPProtocol.
	// See https://github.com/knative/networking/issues/417
	if httpOption == "" {
		return nil
	}
	server := &istiov1alpha3.Server{
		Hosts: hosts,
		Port: &istiov1alpha3.Port{
			Name:     httpServerPortName,
			Number:   GatewayHTTPPort,
			Protocol: "HTTP",
		},
	}
	if httpOption == v1alpha1.HTTPOptionRedirected {
		server.Tls = &istiov1alpha3.ServerTLSSettings{
			HttpsRedirect: true,
		}
	}
	return server
}

// GetNonWildcardIngressTLS gets Ingress TLS that do not reference wildcard certificates.
func GetNonWildcardIngressTLS(ingressTLS []v1alpha1.IngressTLS, nonWildcardSecrest map[string]*corev1.Secret) []v1alpha1.IngressTLS {
	result := []v1alpha1.IngressTLS{}
	for _, tls := range ingressTLS {
		if _, ok := nonWildcardSecrest[secretKey(tls)]; ok {
			result = append(result, tls)
		}
	}
	return result
}

// GetIngressGatewaySvcNameNamespaces gets the Istio ingress namespaces from ConfigMap.
// TODO(nghia):  Remove this by parsing at config parsing time.
func GetIngressGatewaySvcNameNamespaces(ctx context.Context) ([]metav1.ObjectMeta, error) {
	cfg := config.FromContext(ctx).Istio
	nameNamespaces := make([]metav1.ObjectMeta, len(cfg.IngressGateways))
	for i, ingressgateway := range cfg.IngressGateways {
		parts := strings.SplitN(ingressgateway.ServiceURL, ".", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("unexpected service URL form: %s", ingressgateway.ServiceURL)
		}
		nameNamespaces[i] = metav1.ObjectMeta{
			Name:      parts[0],
			Namespace: parts[1],
		}
	}
	return nameNamespaces, nil
}

// UpdateGateway replaces the existing servers with the wanted servers.
func UpdateGateway(gateway *v1alpha3.Gateway, want []*istiov1alpha3.Server, existing []*istiov1alpha3.Server) *v1alpha3.Gateway {
	existingServers := sets.String{}
	for i := range existing {
		existingServers.Insert(existing[i].Port.Name)
	}

	servers := []*istiov1alpha3.Server{}
	for _, server := range gateway.Spec.Servers {
		// We remove
		//  1) the existing servers
		//  2) the placeholder servers.
		if existingServers.Has(server.Port.Name) || isPlaceHolderServer(server) {
			continue
		}
		servers = append(servers, server)
	}
	servers = append(servers, want...)

	// Istio Gateway requires to have at least one server. So if the final gateway does not have any server,
	// we add "placeholder" server back.
	if len(servers) == 0 {
		servers = append(servers, &placeholderServer)
	}

	gateway.Spec.Servers = SortServers(servers)
	return gateway
}

func isPlaceHolderServer(server *istiov1alpha3.Server) bool {
	return cmp.Equal(server, &placeholderServer, protocmp.Transform())
}
