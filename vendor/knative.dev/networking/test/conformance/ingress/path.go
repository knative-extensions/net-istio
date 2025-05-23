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
	"errors"
	"math"
	"testing"

	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/test"
)

// TestPath verifies that an Ingress properly dispatches to backends based on the path of the URL.
func TestPath(t *testing.T) {
	t.Parallel()
	ctx, clients := context.Background(), test.Setup(t)

	// For /foo
	fooName, fooPort, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

	// For /bar
	barName, barPort, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

	// For /baz
	bazName, bazPort, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

	name, port, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

	// Use a post-split injected header to establish which split we are sending traffic to.
	const headerName = "Which-Backend"

	_, client, _ := CreateIngressReady(ctx, t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      []string{name + "." + test.NetworkingFlags.ServiceDomain},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Path: "/foo",
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      fooName,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(fooPort),
						},
						// Append different headers to each split, which lets us identify
						// which backend we hit.
						AppendHeaders: map[string]string{
							headerName: fooName,
						},
						Percent: 100,
					}},
				}, {
					Path: "/bar",
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      barName,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(barPort),
						},
						// Append different headers to each split, which lets us identify
						// which backend we hit.
						AppendHeaders: map[string]string{
							headerName: barName,
						},
						Percent: 100,
					}},
				}, {
					Path: "/baz",
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      bazName,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(bazPort),
						},
						// Append different headers to each split, which lets us identify
						// which backend we hit.
						AppendHeaders: map[string]string{
							headerName: bazName,
						},
						Percent: 100,
					}},
				}, {
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      name,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(port),
						},
						// Append different headers to each split, which lets us identify
						// which backend we hit.
						AppendHeaders: map[string]string{
							headerName: name,
						},
						Percent: 100,
					}},
				}},
			},
		}},
	})

	tests := map[string]string{
		"/foo":  fooName,
		"/bar":  barName,
		"/baz":  bazName,
		"":      name,
		"/asdf": name,
	}

	for path, want := range tests {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			ri := RuntimeRequest(ctx, t, client, "http://"+name+"."+test.NetworkingFlags.ServiceDomain+path)
			if ri == nil {
				return
			}

			got := ri.Request.Headers.Get(headerName)
			if got != want {
				t.Errorf("Header[%q] = %q, wanted %q", headerName, got, want)
			}
		})
	}
}

func TestPathAndPercentageSplit(t *testing.T) {
	t.Parallel()
	ctx, clients := context.Background(), test.Setup(t)

	fooName, fooPort, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

	barName, barPort, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

	name, port, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)

	// Use a post-split injected header to establish which split we are sending traffic to.
	const headerName = "Which-Backend"

	_, client, _ := CreateIngressReady(ctx, t, clients, v1alpha1.IngressSpec{
		Rules: []v1alpha1.IngressRule{{
			Hosts:      []string{name + "." + test.NetworkingFlags.ServiceDomain},
			Visibility: v1alpha1.IngressVisibilityExternalIP,
			HTTP: &v1alpha1.HTTPIngressRuleValue{
				Paths: []v1alpha1.HTTPIngressPath{{
					Path: "/foo",
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      fooName,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(fooPort),
						},
						AppendHeaders: map[string]string{
							headerName: fooName,
						},
						Percent: 50,
					}, {
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      barName,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(barPort),
						},
						AppendHeaders: map[string]string{
							headerName: barName,
						},
						Percent: 50,
					}},
				}, {
					Splits: []v1alpha1.IngressBackendSplit{{
						IngressBackend: v1alpha1.IngressBackend{
							ServiceName:      name,
							ServiceNamespace: test.ServingNamespace,
							ServicePort:      intstr.FromInt(port),
						},
						// Append different headers to each split, which lets us identify
						// which backend we hit.
						AppendHeaders: map[string]string{
							headerName: name,
						},
						Percent: 100,
					}},
				}},
			},
		}},
	})

	const (
		total     = 1000
		totalHalf = total / 2
		tolerance = total * 0.15
	)
	wantKeys := sets.New(fooName, barName)
	resultCh := make(chan string, total)

	var g errgroup.Group
	g.SetLimit(8)

	for range total {
		g.Go(func() error {
			ri := RuntimeRequest(ctx, t, client, "http://"+name+"."+test.NetworkingFlags.ServiceDomain+"/foo")
			if ri == nil {
				return errors.New("failed to request")
			}
			resultCh <- ri.Request.Headers.Get(headerName)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		t.Error("Error while sending requests:", err)
	}
	close(resultCh)

	got := make(map[string]float64, len(wantKeys))
	for r := range resultCh {
		got[r]++
	}
	for k, v := range got {
		if !wantKeys.Has(k) {
			t.Errorf("%s is not in the expected header say %v", k, wantKeys)
		}
		if math.Abs(v-totalHalf) > tolerance {
			t.Errorf("Header %s got: %v times, want in [%v, %v] range", k, v, totalHalf-tolerance, totalHalf+tolerance)
		}
	}
}
