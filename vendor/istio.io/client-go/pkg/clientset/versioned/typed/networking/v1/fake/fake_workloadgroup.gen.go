// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"
	json "encoding/json"
	"fmt"

	v1 "istio.io/client-go/pkg/apis/networking/v1"
	networkingv1 "istio.io/client-go/pkg/applyconfiguration/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeWorkloadGroups implements WorkloadGroupInterface
type FakeWorkloadGroups struct {
	Fake *FakeNetworkingV1
	ns   string
}

var workloadgroupsResource = v1.SchemeGroupVersion.WithResource("workloadgroups")

var workloadgroupsKind = v1.SchemeGroupVersion.WithKind("WorkloadGroup")

// Get takes name of the workloadGroup, and returns the corresponding workloadGroup object, and an error if there is any.
func (c *FakeWorkloadGroups) Get(ctx context.Context, name string, options metav1.GetOptions) (result *v1.WorkloadGroup, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(workloadgroupsResource, c.ns, name), &v1.WorkloadGroup{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1.WorkloadGroup), err
}

// List takes label and field selectors, and returns the list of WorkloadGroups that match those selectors.
func (c *FakeWorkloadGroups) List(ctx context.Context, opts metav1.ListOptions) (result *v1.WorkloadGroupList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(workloadgroupsResource, workloadgroupsKind, c.ns, opts), &v1.WorkloadGroupList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1.WorkloadGroupList{ListMeta: obj.(*v1.WorkloadGroupList).ListMeta}
	for _, item := range obj.(*v1.WorkloadGroupList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested workloadGroups.
func (c *FakeWorkloadGroups) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(workloadgroupsResource, c.ns, opts))

}

// Create takes the representation of a workloadGroup and creates it.  Returns the server's representation of the workloadGroup, and an error, if there is any.
func (c *FakeWorkloadGroups) Create(ctx context.Context, workloadGroup *v1.WorkloadGroup, opts metav1.CreateOptions) (result *v1.WorkloadGroup, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(workloadgroupsResource, c.ns, workloadGroup), &v1.WorkloadGroup{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1.WorkloadGroup), err
}

// Update takes the representation of a workloadGroup and updates it. Returns the server's representation of the workloadGroup, and an error, if there is any.
func (c *FakeWorkloadGroups) Update(ctx context.Context, workloadGroup *v1.WorkloadGroup, opts metav1.UpdateOptions) (result *v1.WorkloadGroup, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(workloadgroupsResource, c.ns, workloadGroup), &v1.WorkloadGroup{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1.WorkloadGroup), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeWorkloadGroups) UpdateStatus(ctx context.Context, workloadGroup *v1.WorkloadGroup, opts metav1.UpdateOptions) (*v1.WorkloadGroup, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(workloadgroupsResource, "status", c.ns, workloadGroup), &v1.WorkloadGroup{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1.WorkloadGroup), err
}

// Delete takes name of the workloadGroup and deletes it. Returns an error if one occurs.
func (c *FakeWorkloadGroups) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteActionWithOptions(workloadgroupsResource, c.ns, name, opts), &v1.WorkloadGroup{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeWorkloadGroups) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(workloadgroupsResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1.WorkloadGroupList{})
	return err
}

// Patch applies the patch and returns the patched workloadGroup.
func (c *FakeWorkloadGroups) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.WorkloadGroup, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(workloadgroupsResource, c.ns, name, pt, data, subresources...), &v1.WorkloadGroup{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1.WorkloadGroup), err
}

// Apply takes the given apply declarative configuration, applies it and returns the applied workloadGroup.
func (c *FakeWorkloadGroups) Apply(ctx context.Context, workloadGroup *networkingv1.WorkloadGroupApplyConfiguration, opts metav1.ApplyOptions) (result *v1.WorkloadGroup, err error) {
	if workloadGroup == nil {
		return nil, fmt.Errorf("workloadGroup provided to Apply must not be nil")
	}
	data, err := json.Marshal(workloadGroup)
	if err != nil {
		return nil, err
	}
	name := workloadGroup.Name
	if name == nil {
		return nil, fmt.Errorf("workloadGroup.Name must be provided to Apply")
	}
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(workloadgroupsResource, c.ns, *name, types.ApplyPatchType, data), &v1.WorkloadGroup{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1.WorkloadGroup), err
}

// ApplyStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating ApplyStatus().
func (c *FakeWorkloadGroups) ApplyStatus(ctx context.Context, workloadGroup *networkingv1.WorkloadGroupApplyConfiguration, opts metav1.ApplyOptions) (result *v1.WorkloadGroup, err error) {
	if workloadGroup == nil {
		return nil, fmt.Errorf("workloadGroup provided to Apply must not be nil")
	}
	data, err := json.Marshal(workloadGroup)
	if err != nil {
		return nil, err
	}
	name := workloadGroup.Name
	if name == nil {
		return nil, fmt.Errorf("workloadGroup.Name must be provided to Apply")
	}
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(workloadgroupsResource, c.ns, *name, types.ApplyPatchType, data, "status"), &v1.WorkloadGroup{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1.WorkloadGroup), err
}