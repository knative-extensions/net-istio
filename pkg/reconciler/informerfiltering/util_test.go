/*
Copyright 2026 The Knative Authors

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

package informerfiltering

import (
	"context"
	"os"
	"testing"

	istiofilteredFactory "knative.dev/net-istio/pkg/client/istio/injection/informers/factory/filtered"
	"knative.dev/networking/pkg/apis/networking"
)

func TestShouldFilterVSByLabelUnset(t *testing.T) {
	unsetEnv(t, EnableVSInformerFilteringByLabelEnv)

	if got := ShouldFilterVSByLabel(); got {
		t.Fatalf("ShouldFilterVSByLabel() = %v, want false when env var is unset", got)
	}
}

func TestShouldEnableFromEnv(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{{
		name:  "true",
		value: "true",
		want:  true,
	}, {
		name:  "false",
		value: "false",
		want:  false,
	}, {
		name:  "one",
		value: "1",
		want:  true,
	}, {
		name:  "zero",
		value: "0",
		want:  false,
	}, {
		name:  "invalid",
		value: "notabool",
		want:  false,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const key = "ENABLE_TEST_FLAG"
			t.Setenv(key, tt.value)
			if got := shouldEnableFromEnv(key); got != tt.want {
				t.Fatalf("shouldEnableFromEnv() = %v, want %v for value %q", got, tt.want, tt.value)
			}
		})
	}
}

func TestShouldFilterVSByLabelParse(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{{
		name:  "true",
		value: "true",
		want:  true,
	}, {
		name:  "false",
		value: "false",
		want:  false,
	}, {
		name:  "invalid",
		value: "notabool",
		want:  false,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnableVSInformerFilteringByLabelEnv, tt.value)
			if got := ShouldFilterVSByLabel(); got != tt.want {
				t.Fatalf("ShouldFilterVSByLabel() = %v, want %v for value %q", got, tt.want, tt.value)
			}
		})
	}
}

func TestShouldFilterByCertificateUIDParse(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{{
		name:  "true",
		value: "true",
		want:  true,
	}, {
		name:  "false",
		value: "false",
		want:  false,
	}, {
		name:  "invalid",
		value: "notabool",
		want:  false,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnableSecretInformerFilteringByCertUIDEnv, tt.value)
			if got := ShouldFilterByCertificateUID(); got != tt.want {
				t.Fatalf("ShouldFilterByCertificateUID() = %v, want %v for value %q", got, tt.want, tt.value)
			}
		})
	}
}

func TestGetContextWithVSFilteringLabelSelector(t *testing.T) {
	t.Setenv(EnableVSInformerFilteringByLabelEnv, "true")
	ctx := GetContextWithVSFilteringLabelSelector(context.Background())
	selectors := selectorsFromContext(t, ctx)
	if len(selectors) != 1 || selectors[0] != networking.IngressLabelKey {
		t.Fatalf("selectors = %v, want [%q]", selectors, networking.IngressLabelKey)
	}

	t.Setenv(EnableVSInformerFilteringByLabelEnv, "false")
	ctx = GetContextWithVSFilteringLabelSelector(context.Background())
	selectors = selectorsFromContext(t, ctx)
	if len(selectors) != 1 || selectors[0] != "" {
		t.Fatalf("selectors = %v, want [\"\"]", selectors)
	}
}

func selectorsFromContext(t *testing.T, ctx context.Context) []string {
	t.Helper()
	untyped := ctx.Value(istiofilteredFactory.LabelKey{})
	if untyped == nil {
		t.Fatal("expected label selectors in context, got nil")
	}
	selectors, ok := untyped.([]string)
	if !ok {
		t.Fatalf("expected []string selectors, got %T", untyped)
	}
	return selectors
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	value, ok := os.LookupEnv(key)
	_ = os.Unsetenv(key)
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(key, value)
			return
		}
		_ = os.Unsetenv(key)
	})
}
