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
	"hash/adler32"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	istiov1beta1 "istio.io/api/networking/v1beta1"
	"istio.io/client-go/pkg/apis/networking/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"knative.dev/net-istio/pkg/reconciler/ingress/config"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/tracker"
)

const (
	GatewayHTTPPort              = 80
	ExternalGatewayHTTPSPort     = 443
	ClusterLocalGatewayHTTPSPort = 8444
	dns1123LabelMaxLength        = 63 // Public for testing only.
	dns1123LabelFmt              = "[a-zA-Z0-9](?:[-a-zA-Z0-9]*[a-zA-Z0-9])?"
	localGatewayPostfix          = "-local"
)

var httpServerPortName = "http-server"

var gatewayGvk = v1beta1.SchemeGroupVersion.WithKind("Gateway")

// Istio Gateway requires to have at least one server. This placeholderServer is used when
// all of the real servers are deleted.
var placeholderServer = istiov1beta1.Server{
	Hosts: []string{"place-holder.place-holder"},
	Port: &istiov1beta1.Port{
		Name:     "place-holder",
		Number:   9999,
		Protocol: "HTTP",
	},
}

var dns1123LabelRegexp = regexp.MustCompile("^" + dns1123LabelFmt + "$")

// GetServers gets the `Servers` from `Gateway` that belongs to the given Ingress.
func GetServers(gateway *v1beta1.Gateway, ing *v1alpha1.Ingress) []*istiov1beta1.Server {
	servers := []*istiov1beta1.Server{}
	for i := range gateway.Spec.GetServers() {
		if belongsToIngress(gateway.Spec.GetServers()[i], ing) {
			servers = append(servers, gateway.Spec.GetServers()[i])
		}
	}
	return SortServers(servers)
}

// GetHTTPServer gets the HTTP `Server` from `Gateway`.
func GetHTTPServer(gateway *v1beta1.Gateway) *istiov1beta1.Server {
	for _, server := range gateway.Spec.GetServers() {
		// The server with "http" port is the default HTTP server.
		if server.GetPort().GetName() == httpServerPortName || server.GetPort().GetName() == "http" {
			return server
		}
	}
	return nil
}

func belongsToIngress(server *istiov1beta1.Server, ing *v1alpha1.Ingress) bool {
	// The format of the portName should be "<namespace>/<ingress_name>:<number>".
	// For example, default/routetest:0.
	portNameSplits := strings.Split(server.GetPort().GetName(), ":")
	if len(portNameSplits) != 2 {
		return false
	}
	return portNameSplits[0] == portNamePrefix(ing.GetNamespace(), ing.GetName())
}

// SortServers sorts `Server` according to its port name.
func SortServers(servers []*istiov1beta1.Server) []*istiov1beta1.Server {
	sort.Slice(servers, func(i, j int) bool {
		return strings.Compare(servers[i].GetPort().GetName(), servers[j].GetPort().GetName()) < 0
	})
	return servers
}

// MakeIngressTLSGateways creates Gateways that have only TLS servers for a given Ingress.
func MakeIngressTLSGateways(ctx context.Context, ing *v1alpha1.Ingress, visibility v1alpha1.IngressVisibility,
	ingressTLS []v1alpha1.IngressTLS, originSecrets map[string]*corev1.Secret, svcLister corev1listers.ServiceLister,
) ([]*v1beta1.Gateway, error) {
	// No need to create Gateway if there is no related ingress TLS.
	if len(ingressTLS) == 0 {
		return []*v1beta1.Gateway{}, nil
	}
	gatewayServices, err := getGatewayServices(ctx, ing, svcLister)
	if err != nil {
		return nil, err
	}
	gateways := make([]*v1beta1.Gateway, len(gatewayServices))
	for i, gatewayService := range gatewayServices {
		servers, err := MakeTLSServers(ing, visibility, ingressTLS, gatewayService.Namespace, originSecrets)
		if err != nil {
			return nil, err
		}
		gateways[i] = makeIngressGateway(ing, visibility, gatewayService.Spec.Selector, servers, gatewayService)
	}
	return gateways, nil
}

// MakeExternalIngressGateways creates Gateways with given Servers for a given Ingress.
func MakeExternalIngressGateways(ctx context.Context, ing *v1alpha1.Ingress, servers []*istiov1beta1.Server, svcLister corev1listers.ServiceLister) ([]*v1beta1.Gateway, error) {
	gatewayServices, err := getGatewayServices(ctx, ing, svcLister)
	if err != nil {
		return nil, err
	}
	gateways := make([]*v1beta1.Gateway, len(gatewayServices))
	for i, gatewayService := range gatewayServices {
		gateways[i] = makeIngressGateway(ing, v1alpha1.IngressVisibilityExternalIP, gatewayService.Spec.Selector, servers, gatewayService)
	}
	return gateways, nil
}

// MakeWildcardTLSGateways creates gateways that only contain TLS server with wildcard hosts based on the wildcard secret information.
// Gateways generated are based on the related ingress being reconciled.
// For each public ingress service, we will create a list of Gateways. Each Gateway of the list corresponds to a wildcard cert secret.
func MakeWildcardTLSGateways(ctx context.Context, ing *v1alpha1.Ingress, originWildcardSecrets map[string]*corev1.Secret,
	svcLister corev1listers.ServiceLister,
) ([]*v1beta1.Gateway, error) {
	if len(originWildcardSecrets) == 0 {
		return []*v1beta1.Gateway{}, nil
	}
	gatewayServices, err := getGatewayServices(ctx, ing, svcLister)
	if err != nil {
		return nil, err
	}
	gateways := []*v1beta1.Gateway{}
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
	gatewayService *corev1.Service,
) ([]*v1beta1.Gateway, error) {
	gateways := make([]*v1beta1.Gateway, 0, len(originWildcardSecrets))
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
		servers := []*istiov1beta1.Server{{
			Hosts: hosts,
			Port: &istiov1beta1.Port{
				Name:     "https",
				Number:   ExternalGatewayHTTPSPort,
				Protocol: "HTTPS",
			},
			Tls: &istiov1beta1.ServerTLSSettings{
				Mode:              istiov1beta1.ServerTLSSettings_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
				CredentialName:    credentialName,
				// TODO: Drop this when all supported Istio version uses TLS v1.2 by default.
				MinProtocolVersion: istiov1beta1.ServerTLSSettings_TLSV1_2,
			},
		}}
		gvk := schema.GroupVersionKind{Version: "v1", Kind: "Secret"}
		gateways = append(gateways, &v1beta1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:            WildcardGatewayName(secret.Name, gatewayService.Namespace, gatewayService.Name),
				Namespace:       secret.Namespace,
				OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(secret, gvk)},
			},
			Spec: istiov1beta1.Gateway{
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
func GetQualifiedGatewayNames(gateways []*v1beta1.Gateway) []string {
	result := make([]string, 0, len(gateways))
	for _, gw := range gateways {
		result = append(result, gw.Namespace+"/"+gw.Name)
	}
	return result
}

// GatewayRef returns the Reference for a give Gateway.
func GatewayRef(gw *v1beta1.Gateway) tracker.Reference {
	apiVersion, kind := gatewayGvk.ToAPIVersionAndKind()
	return tracker.Reference{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       gw.Name,
		Namespace:  gw.Namespace,
	}
}

func makeIngressGateway(ing *v1alpha1.Ingress, visibility v1alpha1.IngressVisibility, selector map[string]string, servers []*istiov1beta1.Server, gatewayService *corev1.Service) *v1beta1.Gateway {
	return &v1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:            GatewayName(ing, visibility, gatewayService),
			Namespace:       ing.GetNamespace(),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ing)},
			Labels: map[string]string{
				// We need this label to find out all Gateways of a given Ingress.
				networking.IngressLabelKey: ing.GetName(),
			},
		},
		Spec: istiov1beta1.Gateway{
			Selector: selector,
			Servers:  servers,
		},
	}
}

func getGatewayServices(ctx context.Context, obj kmeta.Accessor, svcLister corev1listers.ServiceLister) ([]*corev1.Service, error) {
	ingressSvcMetas, err := GetIngressGatewaySvcNameNamespaces(ctx, obj)
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
func GatewayName(accessor kmeta.Accessor, visibility v1alpha1.IngressVisibility, gatewaySvc *corev1.Service) string {
	prefix := accessor.GetName()
	if !isDNS1123Label(prefix) {
		prefix = strconv.FormatUint(uint64(adler32.Checksum([]byte(prefix))), 10)
	}

	gatewayServiceKey := fmt.Sprintf("%s/%s", gatewaySvc.Namespace, gatewaySvc.Name)
	if visibility == v1alpha1.IngressVisibilityClusterLocal {
		gatewayServiceKey += localGatewayPostfix
	}
	gatewayServiceKeyChecksum := strconv.FormatUint(uint64(adler32.Checksum([]byte(gatewayServiceKey))), 10)

	// Ensure that the overall gateway name still is a DNS1123 label
	maxPrefixLength := dns1123LabelMaxLength - len(gatewayServiceKeyChecksum) - 1
	if len(prefix) > maxPrefixLength {
		prefix = prefix[0:maxPrefixLength]
	}

	return fmt.Sprint(prefix+"-", adler32.Checksum([]byte(gatewayServiceKey)))
}

// MakeTLSServers creates the expected Gateway TLS `Servers` based on the given IngressTLS.
func MakeTLSServers(ing *v1alpha1.Ingress, visibility v1alpha1.IngressVisibility, ingressTLS []v1alpha1.IngressTLS, gatewayServiceNamespace string, originSecrets map[string]*corev1.Secret) ([]*istiov1beta1.Server, error) {
	servers := make([]*istiov1beta1.Server, len(ingressTLS))

	var port uint32
	switch visibility {
	case v1alpha1.IngressVisibilityExternalIP:
		port = ExternalGatewayHTTPSPort
	case v1alpha1.IngressVisibilityClusterLocal:
		port = ClusterLocalGatewayHTTPSPort
	default:
		return nil, fmt.Errorf("invalid ingress visibility: %v", visibility)
	}

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

		servers[i] = &istiov1beta1.Server{
			Hosts: tls.Hosts,
			Port: &istiov1beta1.Port{
				Name:     fmt.Sprintf(portNamePrefix(ing.GetNamespace(), ing.GetName())+":%d", i),
				Number:   port,
				Protocol: "HTTPS",
			},
			Tls: &istiov1beta1.ServerTLSSettings{
				Mode:              istiov1beta1.ServerTLSSettings_SIMPLE,
				ServerCertificate: corev1.TLSCertKey,
				PrivateKey:        corev1.TLSPrivateKeyKey,
				CredentialName:    credentialName,
				// TODO: Drop this when all supported Istio version uses TLS v1.2 by default.
				MinProtocolVersion: istiov1beta1.ServerTLSSettings_TLSV1_2,
			},
		}
	}
	return SortServers(servers), nil
}

func portNamePrefix(prefix, suffix string) string {
	if !isDNS1123Label(suffix) {
		suffix = strconv.FormatUint(uint64(adler32.Checksum([]byte(suffix))), 10)
	}
	return prefix + "/" + suffix
}

// MakeHTTPServer creates a HTTP Gateway `Server` based on the HTTP option
// configuration.
func MakeHTTPServer(httpOption v1alpha1.HTTPOption, hosts []string) *istiov1beta1.Server {
	// Currently we consider when httpOption is empty, it means HTTP server is disabled.
	// This logic will be deprecated when deprecating "Disabled" HTTPProtocol.
	// See https://github.com/knative/networking/issues/417
	if httpOption == "" {
		return nil
	}
	server := &istiov1beta1.Server{
		Hosts: hosts,
		Port: &istiov1beta1.Port{
			Name:     httpServerPortName,
			Number:   GatewayHTTPPort,
			Protocol: "HTTP",
		},
	}
	if httpOption == v1alpha1.HTTPOptionRedirected {
		server.Tls = &istiov1beta1.ServerTLSSettings{
			HttpsRedirect: true,
		}
	}
	return server
}

// GetNonWildcardIngressTLS gets Ingress TLS that do not reference wildcard certificates.
func GetNonWildcardIngressTLS(ingressTLS []v1alpha1.IngressTLS, nonWildcardSecrets map[string]*corev1.Secret) []v1alpha1.IngressTLS {
	result := []v1alpha1.IngressTLS{}
	for _, tls := range ingressTLS {
		if _, ok := nonWildcardSecrets[secretKey(tls)]; ok {
			result = append(result, tls)
		}
	}
	return result
}

// GetIngressGatewaySvcNameNamespaces gets the Istio ingress namespaces from ConfigMap for gateways that should expose the service.
func GetIngressGatewaySvcNameNamespaces(ctx context.Context, obj kmeta.Accessor) ([]metav1.ObjectMeta, error) {
	nameNamespaces := make([]metav1.ObjectMeta, 0)

	serviceGateways, err := GatewaysFromContext(ctx, obj)
	if err != nil {
		return nil, fmt.Errorf("failed to get gateway from configuration: %w", err)
	}

	servicePublicGateways, ok := serviceGateways[v1alpha1.IngressVisibilityExternalIP]
	if !ok {
		return nameNamespaces, nil
	}

	for _, ingressgateway := range servicePublicGateways {
		meta, err := parseIngressGatewayConfig(ingressgateway)
		if err != nil {
			return nil, err
		}

		nameNamespaces = append(nameNamespaces, meta)
	}

	return nameNamespaces, nil
}

// TODO(nghia):  Remove this by parsing at config parsing time.
func parseIngressGatewayConfig(ingressgateway config.Gateway) (metav1.ObjectMeta, error) {
	ret := metav1.ObjectMeta{}

	parts := strings.SplitN(ingressgateway.ServiceURL, ".", 3)
	if len(parts) != 3 {
		return ret, fmt.Errorf("unexpected service URL form: %s", ingressgateway.ServiceURL)
	}

	ret.Name = parts[0]
	ret.Namespace = parts[1]

	return ret, nil
}

// UpdateGateway replaces the existing servers with the wanted servers.
func UpdateGateway(gateway *v1beta1.Gateway, want []*istiov1beta1.Server, existing []*istiov1beta1.Server) *v1beta1.Gateway {
	existingServers := sets.New[string]()
	for i := range existing {
		existingServers.Insert(existing[i].GetPort().GetName())
	}

	servers := []*istiov1beta1.Server{}
	for _, server := range gateway.Spec.GetServers() {
		// We remove
		//  1) the existing servers
		//  2) the placeholder servers.
		if existingServers.Has(server.GetPort().GetName()) || isPlaceHolderServer(server) {
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

func isPlaceHolderServer(server *istiov1beta1.Server) bool {
	return cmp.Equal(server, &placeholderServer, protocmp.Transform())
}

// QualifiedGatewayNamesFromContext get gateway names from context.
func QualifiedGatewayNamesFromContext(ctx context.Context, obj kmeta.Accessor) (map[v1alpha1.IngressVisibility]sets.Set[string], error) {
	ret := make(map[v1alpha1.IngressVisibility]sets.Set[string])

	gateways, err := GatewaysFromContext(ctx, obj)
	if err != nil {
		return ret, fmt.Errorf("failed to get gateways from configuration: %w", err)
	}

	for _, visibility := range []v1alpha1.IngressVisibility{v1alpha1.IngressVisibilityClusterLocal, v1alpha1.IngressVisibilityExternalIP} {
		ret[visibility] = sets.New[string]()

		for _, gtw := range gateways[visibility] {
			ret[visibility] = ret[visibility].Insert(gtw.QualifiedName())
		}
	}

	return ret, nil
}

// GatewaysFromContext get gateways relevant to this ingress from context.
func GatewaysFromContext(ctx context.Context, obj kmeta.Accessor) (map[v1alpha1.IngressVisibility][]config.Gateway, error) {
	ret := make(map[v1alpha1.IngressVisibility][]config.Gateway)

	istioConfig := config.FromContext(ctx).Istio

	// External gateways selection
	externalGateways, err := filterGateway(istioConfig.IngressGateways, obj.GetLabels())
	if err != nil {
		return ret, fmt.Errorf("failed to filter external gateways: %w", err)
	}

	if len(externalGateways) == 0 {
		externalGateways = istioConfig.DefaultExternalGateways()
	}

	ret[v1alpha1.IngressVisibilityExternalIP] = externalGateways

	// Local gateways selection
	localGateways, err := filterGateway(istioConfig.LocalGateways, obj.GetLabels())
	if err != nil {
		return ret, fmt.Errorf("failed to filter local gateways: %w", err)
	}

	if len(localGateways) == 0 {
		localGateways = istioConfig.DefaultLocalGateways()
	}

	ret[v1alpha1.IngressVisibilityClusterLocal] = localGateways

	return ret, nil
}

func filterGateway(gtws []config.Gateway, ingressLabels map[string]string) ([]config.Gateway, error) {
	ret := make([]config.Gateway, 0, 1)

	for _, gtw := range gtws {
		if gtw.LabelSelector == nil { // default value
			continue
		}

		selector, err := metav1.LabelSelectorAsSelector(gtw.LabelSelector)
		if err != nil {
			return ret, fmt.Errorf("failed to create selector from gateway (%s) label selector: %w", gtw.QualifiedName(), err)
		}

		if !selector.Matches(fields.Set(ingressLabels)) {
			continue
		}

		ret = append(ret, gtw)
	}

	return ret, nil
}
