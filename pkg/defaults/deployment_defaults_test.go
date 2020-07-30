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

package defaults

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/google/go-cmp/cmp"
)

func TestIstioDeploymentDefaulting(t *testing.T) {
	tests := []struct {
		name string
		in   *IstioDeployment
		want *IstioDeployment
	}{{
		name: "empty",
		in:   &IstioDeployment{},
		want: &IstioDeployment{
			appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
				},
				Spec: appsv1.DeploymentSpec{
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{},
						},
					},
				},
			},
		},
	}, {
		name: "serving label",
		in: &IstioDeployment{
			appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						ServingServiceLabelKey: "foo-service",
					},
				},
			},
		},
		want: &IstioDeployment{
			appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						ServingServiceLabelKey:            "foo-service",
						"service.istio.io/canonical-name": "foo-service",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"service.istio.io/canonical-name": "foo-service",
							},
						},
					},
				},
			},
		},
	}, {
		name: "configuration label",
		in: &IstioDeployment{
			appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						ServingConfigurationLabelKey: "foo-config",
					},
				},
			},
		},
		want: &IstioDeployment{
			appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						ServingConfigurationLabelKey:      "foo-config",
						"service.istio.io/canonical-name": "foo-config",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"service.istio.io/canonical-name": "foo-config",
							},
						},
					},
				},
			},
		},
	}, {
		name: "revision label",
		in: &IstioDeployment{
			appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						ServingRevisionLabelKey: "foo-revision",
					},
				},
			},
		},
		want: &IstioDeployment{
			appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						ServingRevisionLabelKey:               "foo-revision",
						"service.istio.io/canonical-revision": "foo-revision",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"service.istio.io/canonical-revision": "foo-revision",
							},
						},
					},
				},
			},
		},
	}, {
		name: "service, config, and revision",
		in: &IstioDeployment{
			appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						ServingConfigurationLabelKey: "foo-config",
						ServingRevisionLabelKey:      "foo-revision",
						ServingServiceLabelKey:       "foo-service",
					},
				},
			},
		},
		want: &IstioDeployment{
			appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						ServingConfigurationLabelKey:          "foo-config",
						ServingRevisionLabelKey:               "foo-revision",
						ServingServiceLabelKey:                "foo-service",
						"service.istio.io/canonical-revision": "foo-revision",
						"service.istio.io/canonical-name":     "foo-service",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"service.istio.io/canonical-revision": "foo-revision",
								"service.istio.io/canonical-name":     "foo-service",
							},
						},
					},
				},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.in
			got.SetDefaults(context.Background())
			if !cmp.Equal(got, test.want) {
				t.Errorf("SetDefaults (-want, +got) = %v",
					cmp.Diff(test.want, got))
			}
		})
	}
}

func TestValidate(t *testing.T) {
	in := &IstioDeployment{}

	if in.Validate(context.Background()) != nil {
		t.Error("Validate should have returned nil")
	}
}

func TestDeepCopyObject(t *testing.T) {

	tests := []struct {
		name string
		in   *IstioDeployment
	}{{
		name: "with name",
		in: &IstioDeployment{
			appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo-deployment",
				},
			},
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			in := test.in

			got := in.DeepCopyObject()

			if got == in {
				t.Error("DeepCopyInto returned same object")
			}

			if !cmp.Equal(in, got) {
				t.Errorf("DeepCopyInto (-in, +got) = %v",
					cmp.Diff(in, got))
			}
		})
	}
}
