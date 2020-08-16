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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/networking/test"
	pkgTest "knative.dev/pkg/test"

	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

// CreateServiceReady creates a new Service in state 'Ready'. This function expects Service and Image name
// passed in through 'names'.
// Returns error if the service does not come up correctly.
func CreateServiceReady(t pkgTest.T, clients *Clients, names *test.ResourceNames) error {
	if names.Image == "" {
		return fmt.Errorf("expected non-empty Image name; got Image=%v", names.Image)
	}

	t.Log("Creating a new Service.", "service", names.Service)

	s := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Service,
		},
		Spec: v1.ServiceSpec{
			ConfigurationSpec: v1.ConfigurationSpec{
				Template: v1.RevisionTemplateSpec{
					Spec: v1.RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: pkgTest.ImagePath(names.Image),
							}},
						},
					},
				},
			},
		},
	}
	test.AddTestAnnotation(t, s.ObjectMeta)

	if _, err := clients.ServingClient.Services.Create(s); err != nil {
		return err
	}

	t.Log("Waiting for Service to transition to Ready.", "service", names.Service)
	return WaitForServiceState(clients.ServingClient, names.Service, IsServiceReady, "ServiceIsReady")
}

// WaitForServiceState polls the status of the Service called name
// from client every `PollInterval` until `inState` returns `true` indicating it
// is done, returns an error or PollTimeout. desc will be used to name the metric
// that is emitted to track how long it took for name to get into the state checked by inState.
func WaitForServiceState(client *servingClients, name string, inState func(s *v1.Service) (bool, error), desc string) error {
	var lastState *v1.Service
	waitErr := wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		var err error
		lastState, err = client.Services.Get(name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return inState(lastState)
	})

	if waitErr != nil {
		return fmt.Errorf("service %q is not in desired state, got: %#v: %w", name, lastState, waitErr)
	}
	return nil
}

// IsServiceReady will check the status conditions of the service and return true if the service is
// ready. This means that its configurations and routes have all reported ready.
func IsServiceReady(s *v1.Service) (bool, error) {
	return s.IsReady(), nil
}
