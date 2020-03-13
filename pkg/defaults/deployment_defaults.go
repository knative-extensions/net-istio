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

	appsv1 "k8s.io/api/apps/v1"
	"knative.dev/pkg/apis"

	"knative.dev/serving/pkg/apis/serving"
)

const (
	// GroupName is the group name for istio labels and annotations
	GroupName = "service.istio.io"

	// RevisionLabelKey is the label key attached to k8s resources to indicate
	// which Revision triggered their creation.
	RevisionLabelKey = GroupName + "/canonical-revision"

	// ServiceLabelKey is the label key attached to a Route and Configuration indicating by
	// which Service they are created.
	ServiceLabelKey = GroupName + "/canonical-service"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// IstioDeployment is a wrapper around Deployment for setting Istio specific defaults
type IstioDeployment struct {
	appsv1.Deployment `json:",inline"`
}

// Verify that Deployment adheres to the appropriate interfaces.
var (
	// Check that Deployment can be defaulted.
	_ apis.Defaultable = (*IstioDeployment)(nil)
	_ apis.Validatable = (*IstioDeployment)(nil)
)

// SetDefaults implements apis.Defaultable
func (r *IstioDeployment) SetDefaults(ctx context.Context) {
	if r.Labels == nil {
		r.Labels = make(map[string]string)
	}

	if r.Spec.Template.Labels == nil {
		r.Spec.Template.Labels = make(map[string]string)
	}

	revisionName := r.Labels[serving.RevisionLabelKey]
	if revisionName != "" {
		r.Labels[RevisionLabelKey] = revisionName
		r.Spec.Template.Labels[RevisionLabelKey] = revisionName
	}

	servingName := r.servingName()
	if servingName != "" {
		r.Labels[ServiceLabelKey] = servingName
		r.Spec.Template.Labels[ServiceLabelKey] = servingName
	}
}

func (r *IstioDeployment) servingName() string {
	// start with the service name if available.
	// otherwise fall back to configuration name.
	parentKeys := []string{
		serving.ServiceLabelKey,
		serving.ConfigurationLabelKey,
	}

	for _, parentKey := range parentKeys {
		parent, ok := r.Labels[parentKey]
		if ok {
			return parent
		}
	}
	return ""
}

// Validate returns nil due to no need for validation
func (r *IstioDeployment) Validate(ctx context.Context) *apis.FieldError {
	return nil
}
