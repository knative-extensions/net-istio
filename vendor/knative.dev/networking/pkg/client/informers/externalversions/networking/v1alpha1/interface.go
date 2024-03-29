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

// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	internalinterfaces "knative.dev/networking/pkg/client/informers/externalversions/internalinterfaces"
)

// Interface provides access to all the informers in this group version.
type Interface interface {
	// Certificates returns a CertificateInformer.
	Certificates() CertificateInformer
	// ClusterDomainClaims returns a ClusterDomainClaimInformer.
	ClusterDomainClaims() ClusterDomainClaimInformer
	// Ingresses returns a IngressInformer.
	Ingresses() IngressInformer
	// ServerlessServices returns a ServerlessServiceInformer.
	ServerlessServices() ServerlessServiceInformer
}

type version struct {
	factory          internalinterfaces.SharedInformerFactory
	namespace        string
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// New returns a new Interface.
func New(f internalinterfaces.SharedInformerFactory, namespace string, tweakListOptions internalinterfaces.TweakListOptionsFunc) Interface {
	return &version{factory: f, namespace: namespace, tweakListOptions: tweakListOptions}
}

// Certificates returns a CertificateInformer.
func (v *version) Certificates() CertificateInformer {
	return &certificateInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// ClusterDomainClaims returns a ClusterDomainClaimInformer.
func (v *version) ClusterDomainClaims() ClusterDomainClaimInformer {
	return &clusterDomainClaimInformer{factory: v.factory, tweakListOptions: v.tweakListOptions}
}

// Ingresses returns a IngressInformer.
func (v *version) Ingresses() IngressInformer {
	return &ingressInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// ServerlessServices returns a ServerlessServiceInformer.
func (v *version) ServerlessServices() ServerlessServiceInformer {
	return &serverlessServiceInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}
