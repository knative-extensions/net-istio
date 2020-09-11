// +build e2e

/*
Copyright 2020 The Knative Authors

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

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"

	"knative.dev/net-istio/pkg/reconciler/ingress/config"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/test"
	"knative.dev/networking/test/conformance/ingress"
	"knative.dev/pkg/system"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
)

func TestIstioProbing(t *testing.T) {
	clients := Setup(t)
	namespace := system.Namespace()

	// Save the current Gateway to restore it after the test
	oldGateway, err := clients.IstioClient.NetworkingV1alpha3().Gateways(namespace).Get(context.Background(), config.KnativeIngressGateway, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Gateway %s/%s", namespace, config.KnativeIngressGateway)
	}

	// After the test ends, restore the old gateway
	test.EnsureCleanup(t, func() {
		curGateway, err := clients.IstioClient.NetworkingV1alpha3().Gateways(namespace).Get(context.Background(), config.KnativeIngressGateway, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get Gateway %s/%s", namespace, config.KnativeIngressGateway)
		}
		curGateway.Spec.Servers = oldGateway.Spec.Servers
		if _, err := clients.IstioClient.NetworkingV1alpha3().Gateways(namespace).Update(context.Background(), curGateway, metav1.UpdateOptions{}); err != nil {
			t.Fatalf("Failed to restore Gateway %s/%s: %v", namespace, config.KnativeIngressGateway, err)
		}
	})

	tlsOptions := &istiov1alpha3.ServerTLSSettings{
		Mode:              istiov1alpha3.ServerTLSSettings_SIMPLE,
		PrivateKey:        "/etc/istio/ingressgateway-certs/tls.key",
		ServerCertificate: "/etc/istio/ingressgateway-certs/tls.crt",
	}

	cases := []struct {
		name    string
		servers []*istiov1alpha3.Server
		urls    []string
	}{{
		name: "HTTP",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-http",
				Number:   80,
				Protocol: "HTTP",
			},
		}},
		urls: []string{"http://%s/"},
	}, {
		name: "HTTP2",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-http2",
				Number:   80,
				Protocol: "HTTP2",
			},
		}},
		urls: []string{"http://%s/"},
	}, {
		name: "HTTP custom port",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-http",
				Number:   443,
				Protocol: "HTTP",
			},
		}},
		urls: []string{"http://%s:443/"},
	}, {
		name: "HTTP & HTTPS",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-http",
				Number:   80,
				Protocol: "HTTP",
			},
		}, {
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-https",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: tlsOptions,
		}},
		urls: []string{"http://%s/", "https://%s/"},
	}, {
		name: "HTTP redirect & HTTPS",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-http",
				Number:   80,
				Protocol: "HTTP",
			},
		}, {
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-https",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: tlsOptions,
		}},
		urls: []string{"http://%s/", "https://%s/"},
	}, {
		name: "HTTPS",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-https",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: tlsOptions,
		}},
		urls: []string{"https://%s/"},
	}, {
		name: "HTTPS non standard port",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-https",
				Number:   80,
				Protocol: "HTTPS",
			},
			Tls: tlsOptions,
		}},
		urls: []string{"https://%s:80/"},
	}, {
		name: "unsupported protocol (GRPC)",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-grpc",
				Number:   80,
				Protocol: "GRPC",
			},
		}},
		// No URLs to probe, just validates the Knative Service is Ready instead of stuck in NotReady
	}, {
		name: "unsupported protocol (TCP)",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-tcp",
				Number:   80,
				Protocol: "TCP",
			},
		}},
		// No URLs to probe, just validates the Knative Service is Ready instead of stuck in NotReady
	}, {
		name: "unsupported protocol (Mongo)",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-mongo",
				Number:   80,
				Protocol: "Mongo",
			},
		}},
		// No URLs to probe, just validates the Knative Service is Ready instead of stuck in NotReady
	}, {
		name: "port not present in service",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-http",
				Number:   8090,
				Protocol: "HTTP",
			},
		}},
		// No URLs to probe, just validates the Knative Service is Ready instead of stuck in NotReady
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {

			name, port, _ := ingress.CreateRuntimeService(context.Background(), t, clients.NetworkingClient, networking.ServicePortNameHTTP1)
			hosts := []string{name + ".example.com"}

			var transportOptions []interface{}
			if hasHTTPS(c.servers) {
				transportOptions = append(transportOptions, setupHTTPS(t, clients.KubeClient, hosts))
			}

			setupGateway(t, clients, namespace, c.servers)

			_, _, _ = ingress.CreateIngressReady(context.Background(), t, clients.NetworkingClient, v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts:      hosts,
					Visibility: v1alpha1.IngressVisibilityExternalIP,
					HTTP: &v1alpha1.HTTPIngressRuleValue{
						Paths: []v1alpha1.HTTPIngressPath{{
							Splits: []v1alpha1.IngressBackendSplit{{
								IngressBackend: v1alpha1.IngressBackend{
									ServiceName:      name,
									ServiceNamespace: test.ServingNamespace,
									ServicePort:      intstr.FromInt(port),
								},
							}},
						}},
					},
				}},
			})

			// Probe the Service on all endpoints
			var g errgroup.Group
			for _, tmpl := range c.urls {
				tmpl := tmpl
				g.Go(func() error {
					u, err := url.Parse(fmt.Sprintf(tmpl, hosts[0]))
					if err != nil {
						return fmt.Errorf("failed to parse URL: %w", err)
					}
					if _, err := pkgTest.WaitForEndpointStateWithTimeout(
						context.Background(),
						clients.KubeClient,
						t.Logf,
						u,
						pkgTest.MatchesAllOf(pkgTest.IsStatusOK),
						"istio probe",
						test.ServingFlags.ResolvableDomain,
						1*time.Minute,
						transportOptions...); err != nil {
						return fmt.Errorf("failed to probe %s: %w", u, err)
					}
					return nil
				})
			}
			err = g.Wait()
			if err != nil {
				t.Fatal("Failed to probe the Service:", err)
			}
		})
	}
}

func hasHTTPS(servers []*istiov1alpha3.Server) bool {
	for _, server := range servers {
		if server.Port.Protocol == "HTTPS" {
			return true
		}
	}
	return false
}

// setupGateway updates the ingress Gateway to the provided Servers and waits until all Envoy pods have been updated.
func setupGateway(t *testing.T, clients *Clients, namespace string, servers []*istiov1alpha3.Server) {
	// Get the current Gateway
	curGateway, err := clients.IstioClient.NetworkingV1alpha3().Gateways(namespace).Get(context.Background(), config.KnativeIngressGateway, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Gateway %s/%s: %v", namespace, config.KnativeIngressGateway, err)
	}

	// Update its Spec
	newGateway := curGateway.DeepCopy()
	newGateway.Spec.Servers = servers

	// Update the Gateway
	gw, err := clients.IstioClient.NetworkingV1alpha3().Gateways(namespace).Update(context.Background(), newGateway, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Failed to update Gateway %s/%s: %v", namespace, config.KnativeIngressGateway, err)
	}

	selector := labels.SelectorFromSet(gw.Spec.Selector).String()
	// Restart the Gateway pods: this is needed because Istio without SDS won't refresh the cert when the secret is updated
	pods, err := clients.KubeClient.Kube.CoreV1().Pods("istio-system").List(context.Background(), metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		t.Fatal("Failed to list Gateway pods:", err)
	}

	// TODO(bancel): there is a race condition here if a pod listed in the call above is deleted before calling watch below

	var wg sync.WaitGroup
	wg.Add(len(pods.Items))
	wtch, err := clients.KubeClient.Kube.CoreV1().Pods("istio-system").Watch(context.Background(), metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		t.Fatal("Failed to watch Gateway pods:", err)
	}
	defer wtch.Stop()

	done := make(chan struct{})
	go func() {
		for {
			select {
			case event := <-wtch.ResultChan():
				if event.Type == watch.Deleted {
					wg.Done()
				}
			case <-done:
				return
			}
		}
	}()

	err = clients.KubeClient.Kube.CoreV1().Pods("istio-system").DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		t.Fatal("Failed to delete Gateway pods:", err)
	}

	wg.Wait()
	done <- struct{}{}
}

// setupHTTPS creates a self-signed certificate, installs it as a Secret and returns an *http.Transport
// trusting the certificate as a root CA.
func setupHTTPS(t *testing.T, kubeClient *pkgTest.KubeClient, hosts []string) spoof.TransportOption {
	t.Helper()

	cert, key, err := generateCertificate(hosts)
	if err != nil {
		t.Fatal("Failed to generate the certificate:", err)
	}

	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	if ok := rootCAs.AppendCertsFromPEM(cert); !ok {
		t.Fatalf("Failed to add the certificate to the root CA")
	}

	kubeClient.Kube.CoreV1().Secrets("istio-system").Delete(context.Background(), "istio-ingressgateway-certs", metav1.DeleteOptions{})
	_, err = kubeClient.Kube.CoreV1().Secrets("istio-system").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "istio-system",
			Name:      "istio-ingressgateway-certs",
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.key": key,
			"tls.crt": cert,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to set Secret %s/%s: %v", "istio-system", "istio-ingressgateway-certs", err)
	}

	return func(transport *http.Transport) *http.Transport {
		transport.TLSClientConfig = &tls.Config{RootCAs: rootCAs}
		return transport
	}
}

// generateCertificate generates a self-signed certificate for the provided hosts and returns
// the PEM encoded certificate and private key.
func generateCertificate(hosts []string) ([]byte, []byte, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	notBefore := time.Now().Add(-5 * time.Minute)
	notAfter := notBefore.Add(2 * time.Hour)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Knative Serving"},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
		}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create the certificate: %w", err)
	}

	var certBuf bytes.Buffer
	if err := pem.Encode(&certBuf, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return nil, nil, fmt.Errorf("failed to encode the certificate: %w", err)
	}

	var keyBuf bytes.Buffer
	if err := pem.Encode(&keyBuf, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}); err != nil {
		return nil, nil, fmt.Errorf("failed to encode the private key: %w", err)
	}

	return certBuf.Bytes(), keyBuf.Bytes(), nil
}
