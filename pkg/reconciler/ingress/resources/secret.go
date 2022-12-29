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
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"hash/adler32"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/tracker"
)

// GetSecrets gets the all of the secrets referenced by the given Ingress, and
// returns a map whose key is the a secret namespace/name key and value is pointer of the secret.
func GetSecrets(ing *v1alpha1.Ingress, secretLister corev1listers.SecretLister) (map[string]*corev1.Secret, error) {
	secrets := map[string]*corev1.Secret{}
	for _, tls := range ing.Spec.TLS {
		ref := secretKey(tls)
		if _, ok := secrets[ref]; ok {
			continue
		}
		secret, err := secretLister.Secrets(tls.SecretNamespace).Get(tls.SecretName)
		if err != nil {
			return nil, err
		}
		secrets[ref] = secret
	}
	return secrets, nil
}

// MakeSecrets makes copies of the origin Secrets under the namespace of Istio gateway service.
func MakeSecrets(ctx context.Context, originSecrets map[string]*corev1.Secret, accessor kmeta.OwnerRefableAccessor) ([]*corev1.Secret, error) {
	nameNamespaces, err := GetIngressGatewaySvcNameNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	secrets := []*corev1.Secret{}
	for _, originSecret := range originSecrets {
		for _, meta := range nameNamespaces {
			if meta.Namespace == originSecret.Namespace {
				// no need to copy secret when the target namespace is the same
				// as the origin namespace
				continue
			}
			secrets = append(secrets, makeSecret(originSecret, targetSecret(originSecret, accessor), meta.Namespace,
				MakeTargetSecretLabels(originSecret.Name, originSecret.Namespace), MakeTargetSecretAnnotations(originSecret.Name)))
		}
	}
	return secrets, nil
}

// MakeWildcardSecrets copies wildcard certificates from origin namespace to the namespace of gateway servicess so they could
// consumed by Istio ingress.
func MakeWildcardSecrets(ctx context.Context, originWildcardCerts map[string]*corev1.Secret) ([]*corev1.Secret, error) {
	nameNamespaces, err := GetIngressGatewaySvcNameNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	secrets := []*corev1.Secret{}
	for _, secret := range originWildcardCerts {
		for _, meta := range nameNamespaces {
			if meta.Namespace == secret.Namespace {
				// no need to copy secret when the target namespace is the same
				// as the origin namespace
				continue
			}
			secrets = append(secrets, makeSecret(secret, targetWildcardSecretName(secret.Name, secret.Namespace), meta.Namespace, map[string]string{}, nil))
		}
	}
	return secrets, nil
}

func targetWildcardSecretName(originSecretName, originSecretNamespace string) string {
	return originSecretNamespace + "--" + originSecretName + "-wildcard"
}

func makeSecret(originSecret *corev1.Secret, name, namespace string, labels, annotations map[string]string) *corev1.Secret {
	labels[networking.CertificateUIDLabelKey] = originSecret.Labels[networking.CertificateUIDLabelKey] // propagate label for informer use

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Data: originSecret.Data,
		Type: originSecret.Type,
	}
}

// MakeTargetSecretLabels returns the labels used in target secret.
func MakeTargetSecretLabels(originSecretName, originSecretNamespace string) map[string]string {
	labels := map[string]string{
		networking.OriginSecretNamespaceLabelKey: originSecretNamespace,
	}

	// the name of a secret is allowed to be longer than 63 characters while a label value is not
	if len(originSecretName) <= dns1123LabelMaxLength {
		labels[networking.OriginSecretNameLabelKey] = originSecretName
	} else {
		suffix := fmt.Sprint(adler32.Checksum([]byte(originSecretName)))

		maxPrefixLength := dns1123LabelMaxLength - len(suffix) - 1
		prefix := originSecretName[0:maxPrefixLength]

		labels[networking.OriginSecretNameLabelKey] = fmt.Sprintf("%s-%s", prefix, suffix)
	}

	return labels
}

// MakeTargetSecretAnnotations returns the annotations used in target secret.
func MakeTargetSecretAnnotations(originSecretName string) map[string]string {
	if len(originSecretName) > dns1123LabelMaxLength {
		return map[string]string{
			networking.OriginSecretNameLabelKey: originSecretName,
		}
	}

	return nil
}

// ExtractOriginSecretRef extracts the origin secret from a certificate
func ExtractOriginSecretRef(secret *corev1.Secret) tracker.Reference {
	originSecretNamespace := secret.Labels[networking.OriginSecretNamespaceLabelKey]

	// the name of a secret is allowed to be longer than 63 characters while a label value is not
	// the label value therefore might contain a hash and we find the name in an annotation
	originSecretName := secret.Labels[networking.OriginSecretNameLabelKey]
	if secret.Annotations != nil && secret.Annotations[networking.OriginSecretNameLabelKey] != "" {
		originSecretName = secret.Annotations[networking.OriginSecretNameLabelKey]
	}

	return SecretRef(originSecretNamespace, originSecretName)
}

// targetSecret returns the name of the Secret that is copied from the origin Secret.
func targetSecret(originSecret *corev1.Secret, accessor kmeta.OwnerRefable) string {
	return fmt.Sprintf("%s-%s", accessor.GetObjectMeta().GetName(), originSecret.UID)
}

// SecretRef returns the Reference of a secret given the namespace and name of the secret.
func SecretRef(namespace, name string) tracker.Reference {
	gvk := corev1.SchemeGroupVersion.WithKind("Secret")
	apiVersion, kind := gvk.ToAPIVersionAndKind()
	return tracker.Reference{
		APIVersion: apiVersion,
		Kind:       kind,
		Namespace:  namespace,
		Name:       name,
	}
}

// Generates the k8s secret key with the given TLS.
func secretKey(tls v1alpha1.IngressTLS) string {
	return fmt.Sprintf("%s/%s", tls.SecretNamespace, tls.SecretName)
}

// CategorizeSecrets categorizes secrets into two sets: wildcard cert secrets and non-wildcard cert secrets.
func CategorizeSecrets(secrets map[string]*corev1.Secret) (map[string]*corev1.Secret, map[string]*corev1.Secret, error) {
	nonWildcardSecrets := map[string]*corev1.Secret{}
	wildcardSecrets := map[string]*corev1.Secret{}
	for k, secret := range secrets {
		isWildcard, err := isWildcardSecret(secret)
		if err != nil {
			return nil, nil, err
		}
		if isWildcard {
			wildcardSecrets[k] = secret
		} else {
			nonWildcardSecrets[k] = secret
		}
	}
	return nonWildcardSecrets, wildcardSecrets, nil
}

func isWildcardSecret(secret *corev1.Secret) (bool, error) {
	hosts, err := GetHostsFromCertSecret(secret)
	if err != nil {
		return false, err
	}
	return isWildcardHost(hosts[0])
}

func isWildcardHost(domain string) (bool, error) {
	splits := strings.Split(domain, ".")
	if len(splits) <= 1 {
		return false, fmt.Errorf("incorrect domain format, got domain %s", domain)
	}
	return splits[0] == "*", nil
}

// GetHostsFromCertSecret gets cert hosts from cert secret.
func GetHostsFromCertSecret(secret *corev1.Secret) ([]string, error) {
	block, _ := pem.Decode(secret.Data[corev1.TLSCertKey])
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM data for secret %s/%s", secret.Namespace, secret.Name)
	}

	certData, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate for secret %s/%s: %w", secret.Namespace, secret.Name, err)
	}
	if len(certData.DNSNames) == 0 {
		return nil, fmt.Errorf("certificate should have DNS names, but it has %d", len(certData.DNSNames))
	}
	return certData.DNSNames, nil
}
