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

package status

import (
	"context"

	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/pkg/status"
)

// FakeStatusManager implements status.Manager for use in unit tests.
type FakeStatusManager struct {
	FakeIsReady func(ctx context.Context, ing *v1alpha1.Ingress) (bool, error)

	isReadyCallCount map[string]int
}

var _ status.Manager = (*FakeStatusManager)(nil)

// IsReady implements IsReady
func (m *FakeStatusManager) IsReady(ctx context.Context, ing *v1alpha1.Ingress) (bool, error) {
	if m.isReadyCallCount == nil {
		m.isReadyCallCount = make(map[string]int, 1)
	}

	m.isReadyCallCount[status.IngressKey(ing)]++

	return m.FakeIsReady(ctx, ing)
}

// IsReadyCallCount returns how many times IsReady has been called for a given ingress
func (m *FakeStatusManager) IsReadyCallCount(ing *v1alpha1.Ingress) int {
	return m.isReadyCallCount[status.IngressKey(ing)]
}
